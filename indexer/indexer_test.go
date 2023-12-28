package indexer

import (
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/logger"
	indexer_testing "flare-ftso-indexer/testing"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIndexer(t *testing.T) {
	// mock blockchain
	go indexer_testing.MockChain(5500, "../testing/chain_copy/blocks.json", "../testing/chain_copy/transactions.json")
	time.Sleep(3 * time.Second)
	indexer_testing.ChainLastBlock = 2000

	// set configuration parameters
	mockChainAddress := "http://localhost:5500"
	cfgChain := config.ChainConfig{NodeURL: mockChainAddress}
	collectTransactions := [][4]interface{}{
		{"22474d350ec2da53d717e30b96e9a2b7628ede5b", "f14fcbc8", true, true},
		{"22474d350ec2da53d717e30b96e9a2b7628ede5b", "4369af80", true, true},
		{"22474d350ec2da53d717e30b96e9a2b7628ede5b", "46f073cf", true, true},
		{"22474d350ec2da53d717e30b96e9a2b7628ede5b", "901d0e19", true, true},
		{"b682deef4f8e298d86bfc3e21f50c675151fb974", "2636434d", true, false},
	} // for the test we do not use finalizations
	cfgIndexer := config.IndexerConfig{
		StartIndex: 50, StopIndex: 2400, BatchSize: 500, NumParallelReq: 4,
		NewBlockCheckMillis: 200, TimeoutMillis: 100, CollectTransactions: collectTransactions,
	}
	cfgLog := config.LoggerConfig{Level: "DEBUG", Console: true, File: "../logger/logs/flare-ftso-indexer_test.log"}
	cfgDB := config.DBConfig{
		Host: "localhost", Port: 3306, Database: "flare_ftso_indexer_test",
		Username: "root", Password: "root", DropTableAtStart: true,
	}
	cfg := config.Config{Indexer: cfgIndexer, Chain: cfgChain, Logger: cfgLog, DB: cfgDB}
	config.GlobalConfigCallback.Call(cfg)

	// connect to the database
	db, err := database.ConnectAndInitialize(&cfgDB)
	if err != nil {
		logger.Fatal("Database connect and initialize error: %s", err)
	}

	// set a new starting index based on the history drop interval
	historyDropIntervalSeconds := 10000
	cfg.Indexer.StartIndex, err = database.GetMinBlockWithHistoryDrop(cfg.Indexer.StartIndex, historyDropIntervalSeconds, cfg.Chain.NodeURL)
	if err != nil {
		logger.Fatal("Could not set the starting index: %s", err)
	}

	// create the indexer
	cIndexer, err := CreateBlockIndexer(&cfg, db)
	if err != nil {
		logger.Fatal("Indexer init error: %s", err)
	}
	// index history with parallel processing
	err = cIndexer.IndexHistory()
	if err != nil {
		logger.Fatal("History run error: %s", err)
	}

	// at the mock server add new blocks after some time
	go increaseLastBlockAndStop()

	// turn on the function to delete in the database everything that
	// is older than the historyDrop interval
	go database.DropHistory(db, historyDropIntervalSeconds, database.HistoryDropIntervalCheck, cfgChain.NodeURL)

	// run indexer
	err = cIndexer.IndexContinuous()
	if err != nil {
		logger.Fatal("Continuous run error: %s", err)
	}

	// correctness check
	states, err := database.GetDBStates(db)
	assert.Equal(t, 1213, int(states.States[database.FirstDatabaseIndexState].Index))
	assert.Equal(t, 2400, int(states.States[database.LastDatabaseIndexState].Index))
	assert.Equal(t, 2499, int(states.States[database.LastChainIndexState].Index))
}

func increaseLastBlockAndStop() {
	indexer_testing.ChainLastBlock = 2100
	time.Sleep(time.Second)
	indexer_testing.ChainLastBlock = 2200
	time.Sleep(time.Second)
	indexer_testing.ChainLastBlock = 2499
	time.Sleep(10 * time.Second)
}
