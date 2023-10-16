package benchmarks

import (
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer"
	"flare-ftso-indexer/indexer/abi"
	"flare-ftso-indexer/logger"
	"fmt"
	"testing"
)

func BenchmarkBlockRequests(b *testing.B) {
	*config.CfgFlag = "../config.songbird.toml"
	cfg, err := config.BuildConfig()
	if err != nil {
		logger.Fatal("Config error: ", err)
		return
	}
	config.GlobalConfigCallback.Call(cfg)

	// this can be used to benchmark the indexer on the FTSO
	// protocol currently running on Songbird or Flare
	abi.FtsoPrefixToFuncCall["60848b44"] = "revealPrices"
	abi.FtsoPrefixToFuncCall["c5adc539"] = "submitPriceHashes"

	for i := 0; i < b.N; i++ {
		logger.Info("Running with configuration: chain: %s, database: %s", cfg.Chain.NodeURL, cfg.DB.Database)

		abi.InitVotingAbi("../indexer/abi/contracts/Voting.json", "../indexer/abi/contracts/VotingRewardManager.json")
		// connect to the database
		db, err := database.ConnectAndInitializeTestDB(&cfg.DB, true)
		if err != nil {
			fmt.Println("Database connect and initialize error: ", err)
			return
		}
		cIndexer, err := indexer.CreateBlockIndexer(cfg, db)
		if err != nil {
			logger.Error("Indexer init error: ", err)
			return
		}
		err = cIndexer.IndexHistory()
		if err != nil {
			logger.Error("History run error: ", err)
		}

	}
}
