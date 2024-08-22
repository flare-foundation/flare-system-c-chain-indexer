package benchmarks

import (
	"context"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer"
	"flare-ftso-indexer/logger"
	"testing"
)

func BenchmarkBlockRequests(b *testing.B) {
	ctx := context.Background()

	*config.CfgFlag = "../config.songbird.toml"
	cfg, err := config.BuildConfig()
	if err != nil {
		logger.Fatal("Config error: %s", err)
		return
	}
	config.GlobalConfigCallback.Call(cfg)

	for i := 0; i < b.N; i++ {
		logger.Info("Running with configuration: chain: %s, database: %s", cfg.Chain.NodeURL, cfg.DB.Database)

		// connect to the database
		db, err := database.ConnectAndInitialize(ctx, &cfg.DB)
		if err != nil {
			logger.Fatal("Database connect and initialize error: %s", err)
		}

		ethClient, err := chain.DialRPCNode(cfg)
		if err != nil {
			logger.Fatal("Eth client error: %s", err)
		}

		cIndexer, err := indexer.CreateBlockIndexer(cfg, db, ethClient)
		if err != nil {
			logger.Fatal("Indexer create error: %s", err)
		}

		err = cIndexer.IndexHistory(ctx)
		if err != nil {
			logger.Fatal("History run error: %s", err)
		}
	}
}
