package main

import (
	"flag"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer"
	"flare-ftso-indexer/logger"
)

func main() {
	flag.Parse()
	cfg, err := config.BuildConfig()
	if err != nil {
		logger.Fatal("Config error: %s", err)
		return
	}
	config.GlobalConfigCallback.Call(cfg)
	logger.Info("Running with configuration: chain: %s, database: %s", cfg.Chain.NodeURL, cfg.DB.Database)

	db, err := database.ConnectAndInitialize(&cfg.DB)
	if err != nil {
		logger.Fatal("Database connect and initialize error: %s", err)
		return
	}

	var startIndex int
	if cfg.DB.HistoryDrop > 0 {
		startIndex, err = database.GetMinBlockWithHistoryDrop(cfg.Indexer.StartIndex, cfg.DB.HistoryDrop, cfg.Chain.NodeURL)
		if err != nil {
			logger.Fatal("Could not set the starting index: %s", err)
			return
		}
		if startIndex != cfg.Indexer.StartIndex {
			logger.Info("Setting new startIndex due to history drop: %d", startIndex)
			cfg.Indexer.StartIndex = startIndex
		}
	}

	cIndexer, err := indexer.CreateBlockIndexer(cfg, db)
	if err != nil {
		logger.Error("Indexer init error: %s", err)
		return
	}
	for {
		err = cIndexer.IndexHistory()
		if err != nil {
			logger.Error("History run error: %s", err)
			logger.Info("Restarting indexing history from the current state")
		} else {
			break
		}
	}

	if cfg.DB.HistoryDrop > 0 {
		go database.DropHistory(db, cfg.DB.HistoryDrop, database.HistoryDropIntervalCheck, cfg.Chain.NodeURL)
	}

	for {
		err = cIndexer.IndexContinuous()
		if err != nil {
			logger.Error("Run error: %s", err)
			logger.Info("Restarting from the current state")
		}
	}
}
