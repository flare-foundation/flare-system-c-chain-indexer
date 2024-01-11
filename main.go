package main

import (
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
	if err := run(); err != nil {
		logger.Fatal("Fatal error: %s", err)
	}
}

func run() error {
	flag.Parse()
	cfg, err := config.BuildConfig()
	if err != nil {
		return errors.Wrap(err, "config error")
	}

	config.GlobalConfigCallback.Call(cfg)

	logger.Info("Running with configuration: chain: %s, database: %s", cfg.Chain, cfg.DB.Database)

	ethClient, err := dialRPCNode(cfg)
	if err != nil {
		return errors.Wrap(err, "Could not connect to the RPC nodes")
	}

	db, err := database.ConnectAndInitialize(&cfg.DB)
	if err != nil {
		return errors.Wrap(err, "Database connect and initialize errors")
	}

	if cfg.DB.HistoryDrop > 0 {
		startIndex, err := database.GetMinBlockWithHistoryDrop(cfg.Indexer.StartIndex, cfg.DB.HistoryDrop, ethClient)
		if err != nil {
			return errors.Wrap(err, "Could not set the starting indexs")
		}

		if startIndex != cfg.Indexer.StartIndex {
			logger.Info("Setting new startIndex due to history drop: %d", startIndex)
			cfg.Indexer.StartIndex = startIndex
		}
	}

	return runIndexer(cfg, db, ethClient)
}

func dialRPCNode(cfg *config.Config) (*ethclient.Client, error) {
	nodeURL, err := cfg.Chain.FullNodeURL()
	if err != nil {
		return nil, err
	}

	return ethclient.Dial(nodeURL.String())
}

func runIndexer(cfg *config.Config, db *gorm.DB, ethClient *ethclient.Client) error {
	cIndexer := indexer.CreateBlockIndexer(cfg, db, ethClient)
	bOff := backoff.NewExponentialBackOff()

	err := backoff.RetryNotify(cIndexer.IndexHistory, bOff, func(err error, d time.Duration) {
		logger.Error("Index history error: %s", err)
	})
	if err != nil {
		return errors.Wrap(err, "Index history fatal error")
	}

	if cfg.DB.HistoryDrop > 0 {
		go database.DropHistory(db, cfg.DB.HistoryDrop, database.HistoryDropIntervalCheck, ethClient)
	}

	err = backoff.RetryNotify(cIndexer.IndexContinuous, bOff, func(err error, d time.Duration) {
		logger.Error("Index continuous error: %s", err)
	})
	if err != nil {
		return errors.Wrap(err, "Index continuous fatal error")
	}

	logger.Info("Finished indexing")

	return nil
}
