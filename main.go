package main

import (
	"context"
	"flag"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer"
	"flare-ftso-indexer/logger"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/ethereum/go-ethereum/ethclient"
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

	ethClient, err := dialRPCNode(cfg)
	if err != nil {
		return errors.Wrap(err, "Could not connect to the RPC nodes")
	}

	db, err := database.ConnectAndInitialize(ctx, &cfg.DB)
	if err != nil {
		return errors.Wrap(err, "Database connect and initialize errors")
	}

	if cfg.DB.HistoryDrop > 0 {
		startIndex, err := database.GetMinBlockWithHistoryDrop(ctx, cfg.Indexer.StartIndex, cfg.DB.HistoryDrop, ethClient)
		if err != nil {
			return errors.Wrap(err, "Could not set the starting indexs")
		}

		if startIndex != cfg.Indexer.StartIndex {
			logger.Info("Setting new startIndex due to history drop: %d", startIndex)
			cfg.Indexer.StartIndex = startIndex
		}
	}

	return runIndexer(ctx, cfg, db, ethClient)
}

func dialRPCNode(cfg *config.Config) (*ethclient.Client, error) {
	nodeURL, err := cfg.Chain.FullNodeURL()
	if err != nil {
		return nil, err
	}

	return ethclient.Dial(nodeURL.String())
}

func runIndexer(ctx context.Context, cfg *config.Config, db *gorm.DB, ethClient *ethclient.Client) error {
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
			logger.Error("Index history error: %s after %s", err, d)
		},
	)
	if err != nil {
		return errors.Wrap(err, "Index history fatal error")
	}

	if cfg.DB.HistoryDrop > 0 {
		go database.DropHistory(ctx, db, cfg.DB.HistoryDrop, database.HistoryDropIntervalCheck, ethClient)
	}

	err = backoff.RetryNotify(
		func() error {
			return cIndexer.IndexContinuous(ctx)
		},
		bOff,
		func(err error, d time.Duration) {
			logger.Error("Index continuous error: %s after %s", err, d)
		},
	)
	if err != nil {
		return errors.Wrap(err, "Index continuous fatal error")
	}

	logger.Info("Finished indexing")

	return nil
}
