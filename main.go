package main

import (
	"context"
	"flag"
	"flare-ftso-indexer/boff"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/contracts"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer/core"
	"flare-ftso-indexer/indexer/fsp"
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

	resolver, err := contracts.NewContractResolver(ethClient)
	if err != nil {
		return errors.Wrap(err, "Failed to initialize contract registry resolver")
	}

	if err := config.ResolveContractAddresses(ctx, cfg, resolver); err != nil {
		return errors.Wrap(err, "Failed to resolve configured contract addresses")
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

	if cfg.Indexer.IsFspMode() {
		return runFspIndexer(ctx, cfg, db, ethClient, resolver)
	}

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

	return runIndexer(ctx, cfg, db, ethClient, resolver, historyDrop)
}

func getStartIndex(
	ctx context.Context,
	db *gorm.DB,
	ethClient *chain.Client,
	cfg *config.Config,
	historyDrop uint64,
) (uint64, error) {
	var latestIndexedBlock database.Block
	err := db.Last(&database.Block{}).Select("number").Scan(&latestIndexedBlock).Error

	// If a latest indexed block is found, return the next block number
	if err == nil {
		logger.Info("Starting after latest indexed block from DB: %d", latestIndexedBlock.Number)
		return latestIndexedBlock.Number + 1, nil
	}

	// In case of an unexpected error, return it
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, errors.Wrap(err, "DB query error")
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
	resolver *contracts.ContractResolver,
	historyDrop uint64,
) error {
	cIndexer, err := core.NewEngine(cfg, db, ethClient, resolver)
	if err != nil {
		return err
	}

	historyLastIndex, err := boff.Retry(
		ctx,
		func() (uint64, error) {
			return cIndexer.IndexHistory(ctx, cfg.Indexer.StartIndex)
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

func runFspIndexer(
	ctx context.Context,
	cfg *config.Config,
	db *gorm.DB,
	ethClient *chain.Client,
	resolver *contracts.ContractResolver,
) error {
	cIndexer, err := core.NewEngine(cfg, db, ethClient, resolver)
	if err != nil {
		return err
	}

	historyLastIndex, err := boff.Retry(
		ctx,
		func() (uint64, error) {
			return fsp.IndexStartup(ctx, cIndexer)
		},
		"IndexFspStartup",
	)
	if err != nil {
		return errors.Wrap(err, "FSP startup backfill fatal error")
	}

	historyDropSeconds := fspHistoryDropHeuristicSeconds(cfg.Indexer.HistoryEpochs)
	logger.Info(
		"Using FSP history drop: history_epochs=%d, derived retention days=%.2f",
		cfg.Indexer.HistoryEpochs,
		float64(historyDropSeconds)/(24*60*60),
	)
	go database.DropHistory(
		ctx,
		db,
		historyDropSeconds,
		database.HistoryDropIntervalCheck,
		ethClient,
		0,
	)

	err = boff.RetryNoReturn(
		ctx,
		func() error {
			return cIndexer.IndexContinuous(ctx, historyLastIndex+1)
		},
		"IndexContinuous",
	)
	if err != nil {
		return errors.Wrap(err, "FSP Index continuous fatal error")
	}

	logger.Info("Finished FSP indexing")
	return nil
}

func fspHistoryDropHeuristicSeconds(historyEpochs uint64) uint64 {
	// Over-estimation heuristic: history_epochs * 3.5 days + 14 days.
	const (
		baseRetentionSeconds     = uint64((14 * 24 * time.Hour) / time.Second)
		retentionPerEpochSeconds = uint64((84 * time.Hour) / time.Second)
	)

	return baseRetentionSeconds + historyEpochs*retentionPerEpochSeconds
}
