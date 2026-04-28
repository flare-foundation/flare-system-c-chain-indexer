package benchmarks

import (
	"context"
	"flare-ftso-indexer/internal/chain"
	"flare-ftso-indexer/internal/config"
	"flare-ftso-indexer/internal/contracts"
	"flare-ftso-indexer/internal/core"
	"flare-ftso-indexer/internal/database"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
)

type benchmarksConfig struct {
	config.Config
}

func BenchmarkBlockRequests(b *testing.B) {
	ctx := context.Background()

	tCfg := benchmarksConfig{}
	tCfg.Indexer.Confirmations = 1
	tCfg.Chain.ChainType = 1
	_, err := toml.DecodeFile("config_banchmark.toml", &tCfg)
	if err != nil {
		logger.Fatalf("Config error: %s", err)
	}
	cfg := tCfg.Config
	config.GlobalConfigCallback.Call(cfg)

	for i := 0; i < b.N; i++ {
		logger.Infof("Running with configuration: chain: %s, database: %s", cfg.Chain.NodeURL, cfg.DB.Database)

		// connect to the database
		db, err := database.ConnectAndInitialize(ctx, &cfg.DB)
		if err != nil {
			logger.Fatalf("Database connect and initialize error: %s", err)
		}

		nodeURL, err := cfg.Chain.FullNodeURL()
		if err != nil {
			logger.Fatalf("Invalid node URL in config: %s", err)
		}

		ethClient, err := chain.DialRPCNode(nodeURL, cfg.Chain.ChainType)
		if err != nil {
			logger.Fatalf("Eth client error: %s", err)
		}

		resolver, err := contracts.NewContractResolver(ethClient)
		if err != nil {
			logger.Fatalf("Registry resolver error: %s", err)
		}

		cIndexer, err := core.NewEngine(&cfg, db, ethClient, resolver)
		if err != nil {
			logger.Fatalf("Indexer create error: %s", err)
		}

		_, err = cIndexer.IndexHistory(ctx, cfg.Indexer.StartIndex)
		if err != nil {
			logger.Fatalf("History run error: %s", err)
		}
	}
}
