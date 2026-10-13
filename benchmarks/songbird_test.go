package benchmarks

import (
	"context"
	"testing"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/contracts"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/core"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/database"

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
	_, err := toml.DecodeFile("config_benchmark.toml", &tCfg)
	if err != nil {
		logger.Fatalw("Reading the config failed", "error", err)
	}
	cfg := tCfg.Config
	config.GlobalConfigCallback.Call(cfg)

	for i := 0; i < b.N; i++ {
		logger.Infow("Benchmark starting", "node_url", cfg.Chain.NodeURL, "database", cfg.DB.Database)

		// connect to the database
		db, err := database.ConnectAndInitialize(ctx, &cfg.DB)
		if err != nil {
			logger.Fatalw("Database connect and initialize failed", "error", err)
		}

		nodeURL, err := cfg.Chain.FullNodeURL()
		if err != nil {
			logger.Fatalw("Invalid node URL in config", "error", err)
		}

		ethClient, err := chain.DialRPCNode(nodeURL, cfg.Indexer.RpcConcurrency)
		if err != nil {
			logger.Fatalw("Connecting to the node failed", "error", err)
		}

		resolver, err := contracts.NewContractResolver(ethClient)
		if err != nil {
			logger.Fatalw("Contract registry resolver failed", "error", err)
		}

		cIndexer, err := core.NewEngine(&cfg, db, ethClient, resolver)
		if err != nil {
			logger.Fatalw("Creating the indexer failed", "error", err)
		}

		_, err = cIndexer.IndexHistory(ctx, cfg.Indexer.StartIndex)
		if err != nil {
			logger.Fatalw("History run failed", "error", err)
		}
	}
}
