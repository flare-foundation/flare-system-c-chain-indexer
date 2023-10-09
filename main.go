package main

import (
	"flag"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer"
	"flare-ftso-indexer/indexer/abi"
	"flare-ftso-indexer/logger"
)

func main() {
	flag.Parse()
	cfg, err := config.BuildConfig()
	if err != nil {
		logger.Fatal("Config error: ", err)
		return
	}
	config.GlobalConfigCallback.Call(cfg)
	logger.Info("Running with configuration: chain: %s, database: %s", cfg.Chain.NodeURL, cfg.DB.Database)

	abi.InitVotingAbi("indexer/abi/contracts/Voting.json", "indexer/abi/contracts/VotingRewardManager.json")
	db, err := database.ConnectAndInitialize(&cfg.DB)
	if err != nil {
		logger.Fatal("Database connect and initialize error: ", err)
		return
	}

	cIndexer, err := indexer.CreateBlockIndexer(cfg, db)
	if err != nil {
		logger.Error("Indexer init error: ", err)
		return
	}
	for {
		err = cIndexer.IndexHistory()
		if err != nil {
			logger.Error("History run error: ", err)
			logger.Info("Restarting indexing history from the current state")
		} else {
			break
		}
	}

	for {
		err = cIndexer.IndexContinuous()
		if err != nil {
			logger.Error("Run error: ", err)
			logger.Info("Restarting from the current state")
		}
	}
}
