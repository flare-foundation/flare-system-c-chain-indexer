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
		logger.Info("Using configured history drop value of %.1f days", historyDropDays)
	}

	if historyDrop > 0 {
		firstBlockNumber, err := boff.Retry(
			ctx,
			func() (uint64, error) {
				return database.GetStartBlock(
					ctx, db, historyDrop, ethClient, cfg.Indexer.StartIndex,
				)
			},
			"GetStartBlock",
		)
		if err != nil {
			return errors.Wrap(err, "GetStartBlock error")
		}

		logger.Info("firstBlockNumber = %d", firstBlockNumber)

		if firstBlockNumber > cfg.Indexer.StartIndex {
			logger.Info("Setting new startIndex due to history drop: %d", firstBlockNumber)
			cfg.Indexer.StartIndex = firstBlockNumber
		}
	}

	return runIndexer(ctx, cfg, db, ethClient, historyDrop)
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

	err = boff.RetryNoReturn(
		ctx,
		func() error {
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
			return cIndexer.IndexContinuous(ctx)
		},
		"IndexContinuous",
	)
	if err != nil {
		return errors.Wrap(err, "Index continuous fatal error")
	}

	logger.Info("Finished indexing")

	return nil
}
