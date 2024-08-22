package main

import (
	"context"
	"flag"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer"
	"flare-ftso-indexer/logger"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

func main() {
	if err := run(context.Background()); err != nil {
		logger.Fatal("Fatal error: %s", err)
	}
}

func run(ctx context.Context) error {
	flag.Parse()
	cfg, err := config.BuildConfig()
	if err != nil {
		return errors.Wrap(err, "config error")
	}

	config.GlobalConfigCallback.Call(cfg)

	ethClient, err := chain.DialRPCNode(cfg)
	if err != nil {
		return errors.Wrap(err, "Could not connect to the RPC nodes")
	}

	db, err := database.ConnectAndInitialize(ctx, &cfg.DB)
	if err != nil {
		return errors.Wrap(err, "Database connect and initialize errors")
	}

	if cfg.DB.HistoryDrop > 0 {
		// Run an initial iteration of the history drop. This could take some
		// time if it has not been run in a while after an outage - running
		// separately avoids database clashes with the indexer.
		logger.Info("running initial DropHistory iteration")
		startTime := time.Now()

		var firstBlockNumber uint64

		err = backoff.RetryNotify(
			func() (err error) {
				firstBlockNumber, err = database.DropHistoryIteration(ctx, db, cfg.DB.HistoryDrop, ethClient)
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}

				return err
			},
			backoff.NewExponentialBackOff(),
			func(err error, d time.Duration) {
				logger.Error("DropHistory error: %s. Will retry after %s", err, d)
			},
		)
		if err != nil {
			return errors.Wrap(err, "startup DropHistory error")
		}

		logger.Info("initial DropHistory iteration finished in %s", time.Since(startTime))

		if firstBlockNumber > cfg.Indexer.StartIndex {
			logger.Info("Setting new startIndex due to history drop: %d", firstBlockNumber)
			cfg.Indexer.StartIndex = firstBlockNumber
		}
	}

	return runIndexer(ctx, cfg, db, ethClient)
}

func runIndexer(ctx context.Context, cfg *config.Config, db *gorm.DB, ethClient *chain.Client) error {
	cIndexer, err := indexer.CreateBlockIndexer(cfg, db, ethClient)
	if err != nil {
		return err
	}

	bOff := backoff.NewExponentialBackOff()

	err = backoff.RetryNotify(
		func() error {
			return cIndexer.IndexHistory(ctx)
		},
		bOff,
		func(err error, d time.Duration) {
			logger.Error("Index history error: %s. Will retry after %s", err, d)
		},
	)
	if err != nil {
		return errors.Wrap(err, "Index history fatal error")
	}

	if cfg.DB.HistoryDrop > 0 {
		go database.DropHistory(
			ctx, db, cfg.DB.HistoryDrop, database.HistoryDropIntervalCheck, ethClient,
		)
	}

	err = backoff.RetryNotify(
		func() error {
			return cIndexer.IndexContinuous(ctx)
		},
		bOff,
		func(err error, d time.Duration) {
			logger.Error("Index continuous error: %s. Will retry after %s", err, d)
		},
	)
	if err != nil {
		return errors.Wrap(err, "Index continuous fatal error")
	}

	logger.Info("Finished indexing")

	return nil
}
