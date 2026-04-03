package main

import (
	"context"
	"flag"
	"flare-ftso-indexer/boff"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer"
	"flare-ftso-indexer/logger"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"gorm.io/gorm"
)

func main() {
	defer logger.SyncFileLogger()

	if err := run(context.Background()); err != nil {
		logger.Fatal("Fatal error: %s", err)
	}
}

func run(ctx context.Context) error {
	flag.Parse()
	cfg, err := config.BuildConfig()
	if err != nil {
		// The logger is not initialized yet so fallback to directly
		// printing to stdout.
		fmt.Println("Error building config: ", err)
		return err
	}

	config.GlobalConfigCallback.Call(cfg)

	// Sync logger when docker container stops or Ctrl+C is pressed
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-signalChan
		logger.Info("Received signal: %v", sig)
		logger.SyncFileLogger()
		os.Exit(0)
	}()

	nodeURL, err := cfg.Chain.FullNodeURL()
	if err != nil {
		return errors.Wrap(err, "Invalid node URL in config")
	}

	ethClient, err := chain.DialRPCNode(nodeURL, cfg.Chain.ChainType)
	if err != nil {
		return errors.Wrap(err, "Could not connect to the RPC nodes")
	}

	db, err := database.ConnectAndInitialize(ctx, &cfg.DB)
	if err != nil {
		return errors.Wrap(err, "Database connect and initialize errors")
	}

	chainID, err := ethClient.ChainID(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get chain ID")
	}

	logger.Info("Connected to chain ID %s", chainID)

	historyDrop, err := cfg.DB.GetHistoryDrop(ctx, chainID)
	if err != nil {
		return errors.Wrap(err, "Failed to get history drop configuration")
	}

	historyDropDays := (float64(historyDrop) * float64(time.Second)) / float64(24*time.Hour)
	if cfg.DB.HistoryDrop == nil {
		logger.Info("Using default history drop value of %.1f days", historyDropDays)
	} else {
		if *cfg.DB.HistoryDrop == 0 {
			logger.Info("History drop disabled")
		} else {
			logger.Info("Using configured history drop value of %.1f days", historyDropDays)
		}
	}

	startIndex, err := getStartIndex(ctx, db, ethClient, cfg, historyDrop)
	if err != nil {
		return errors.Wrap(err, "getStartIndex error")
	}

	cfg.Indexer.StartIndex = startIndex

	return runIndexer(ctx, cfg, db, ethClient, historyDrop)
}

func getStartIndex(
	ctx context.Context,
	db *gorm.DB,
	ethClient *chain.Client,
	cfg *config.Config,
	historyDrop uint64,
) (uint64, error) {
	var minBlockNumber, maxBlockNumber *uint64
	var blockCount int64

	db.Model(&database.Block{}).Select("MIN(number)").Scan(&minBlockNumber)
	db.Model(&database.Block{}).Select("MAX(number)").Scan(&maxBlockNumber)
	db.Model(&database.Block{}).Count(&blockCount)

	// If blocks exist in DB, check for gaps
	if maxBlockNumber != nil && minBlockNumber != nil && blockCount > 0 {
		expectedCount := int64(*maxBlockNumber - *minBlockNumber + 1)

		if blockCount < expectedCount {
			// Gap detected - find the first missing block
			var firstGap *uint64
			err := db.Raw(`
				SELECT t.number + 1 AS gap_start
				FROM blocks t
				WHERE NOT EXISTS (
					SELECT 1 FROM blocks t2 WHERE t2.number = t.number + 1
				)
				AND t.number < ?
				ORDER BY t.number
				LIMIT 1
			`, *maxBlockNumber).Scan(&firstGap).Error

			if err == nil && firstGap != nil {
				logger.Warn("Gap detected in block history: expected %d blocks, found %d. Resuming from block %d to fill gaps",
					expectedCount, blockCount, *firstGap)
				return *firstGap, nil
			}
		}

		logger.Info("Starting after latest indexed block from DB: %d", *maxBlockNumber)
		return *maxBlockNumber + 1, nil
	}

	// No blocks are indexed yet
	// If history drop is disabled, return the configured start index
	if historyDrop == 0 {
		logger.Info("No indexed blocks found in DB, starting from configured start index: %d", cfg.Indexer.StartIndex)
		return cfg.Indexer.StartIndex, nil
	}

	// History drop is enabled so calculate start index based on it.
	firstBlockNumber, err := boff.Retry(
		ctx,
		func() (uint64, error) {
			return database.GetStartBlock(
				ctx, historyDrop, ethClient, cfg.Indexer.StartIndex,
			)
		},
		"GetStartBlock",
	)
	if err != nil {
		return 0, errors.Wrap(err, "GetStartBlock error")
	}

	logger.Info("No indexed blocks found in DB, starting from calculated start index based on history drop: %d", firstBlockNumber)
	return firstBlockNumber, nil
}

func runIndexer(
	ctx context.Context,
	cfg *config.Config,
	db *gorm.DB,
	ethClient *chain.Client,
	historyDrop uint64,
) error {
	cIndexer, err := indexer.CreateBlockIndexer(cfg, db, ethClient)
	if err != nil {
		return err
	}

	historyLastIndex, err := boff.Retry(
		ctx,
		func() (uint64, error) {
			return cIndexer.IndexHistory(ctx)
		},
		"IndexHistory",
	)
	if err != nil {
		return errors.Wrap(err, "Index history fatal error")
	}

	if historyDrop > 0 {
		go database.DropHistory(
			ctx,
			db,
			historyDrop,
			database.HistoryDropIntervalCheck,
			ethClient,
			cfg.Indexer.StartIndex,
		)
	}

	err = boff.RetryNoReturn(
		ctx,
		func() error {
			return cIndexer.IndexContinuous(ctx, historyLastIndex+1)
		},
		"IndexContinuous",
	)
	if err != nil {
		return errors.Wrap(err, "Index continuous fatal error")
	}

	logger.Info("Finished indexing")

	return nil
}
