package indexer

import (
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer/abi"
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
	cfgIndexer := config.IndexerConfig{StartIndex: 50, StopIndex: 2400, BatchSize: 500,
		NumParallelReq: 4, NewBlockCheckMillis: 200, TimeoutMillis: 100, Receipts: "all"}
	cfgLog := config.LoggerConfig{Level: "DEBUG", Console: true, File: "../logger/logs/flare-ftso-indexer_test.log"}
	cfgDB := config.DBConfig{Host: "localhost", Port: 3306, Database: "flare_ftso_indexer_test",
		Username: "root", Password: "root",
		OptTables: "commit,revealBitvote,signResult,offerRewards"} // for the test we do not use finalizations
	epochConfig := config.EpochConfig{FirstEpochStartSec: 1636070400, EpochDurationSec: 90}
	cfg := config.Config{Indexer: cfgIndexer, Chain: cfgChain, Logger: cfgLog, DB: cfgDB, Epochs: epochConfig}
	config.GlobalConfigCallback.Call(cfg)

	// init info about contract that will be indexed
	abi.InitVotingAbi("abi/contracts/Voting.json", "abi/contracts/VotingRewardManager.json")

	// connect to the database
	db, err := database.ConnectAndInitializeTestDB(&cfgDB, true)
	if err != nil {
		logger.Fatal("Database connect and initialize error: ", err)
	}
	// create the indexer
	cIndexer, err := CreateBlockIndexer(&cfg, db)
	if err != nil {
		logger.Fatal("Indexer init error: ", err)
	}
	// index history with parallel processing
	err = cIndexer.IndexHistory()
	if err != nil {
		logger.Fatal("History run error: ", err)
	}

	// turn on the function to delete in the database everything that
	// is older than the following interval
	intervalSeconds := int(time.Now().Unix() - 1694605681)
	go database.DropHistory(db, intervalSeconds, database.HistoryDropIntervalCheck)

	// at the mock server add new blocks after some time
	go increaseLastBlockAndStop()

	// run indexer
	err = cIndexer.IndexContinuous()
	if err != nil {
		logger.Fatal("Continuous run error: ", err)
	}

	// correctness check
	states, err := database.GetDBStates(db)
	assert.Equal(t, uint64(1518), states.States[database.FirstDatabaseIndexStateName].Index)
	assert.Equal(t, uint64(2401), states.States[database.NextDatabaseIndexStateName].Index)
	assert.Equal(t, uint64(2499), states.States[database.LastChainIndexStateName].Index)
}

func increaseLastBlockAndStop() {
	indexer_testing.ChainLastBlock = 2100
	time.Sleep(time.Second)
	indexer_testing.ChainLastBlock = 2200
	time.Sleep(time.Second)
	indexer_testing.ChainLastBlock = 2499
	time.Sleep(10 * time.Second)
}
