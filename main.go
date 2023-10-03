package main

import (
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer"
	"flare-ftso-indexer/indexer/abi"
	"flare-ftso-indexer/logger"
	"fmt"
)

func main() {
	cfg, err := config.BuildConfig()
	if err != nil {
		fmt.Println("Config error: ", err)
		return
	}
	config.GlobalConfigCallback.Call(cfg)
	logger.Info("Running with configuration: chain: %s, database: %s", cfg.Chain.NodeURL, cfg.DB.Database)

	abi.InitVotingAbi("indexer/abi/contracts/Voting.json", "indexer/abi/contracts/VotingRewardManager.json")
	db, err := database.ConnectAndInitialize(&cfg.DB)
	if err != nil {
		fmt.Println("Database connect and initialize error: ", err)
		return
	}

	cIndexer, err := indexer.CreateBlockIndexer(cfg, db)
	if err != nil {
		fmt.Println("Indexer init error: ", err)
		return
	}
	err = cIndexer.IndexHistory()
	if err != nil {
		fmt.Println("History run error: ", err)
		return
	}

	err = cIndexer.IndexContinuous()
	if err != nil {
		fmt.Println("Run error: ", err)
		return
	}
}
