package indexer

import (
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/logger"
	indexer_testing "flare-ftso-indexer/testing"
	"fmt"
	"testing"
	"time"

	"github.com/caarlos0/env/v10"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/assert"
)

type testConfig struct {
	DBHost        string `env:"DB_HOST" envDefault:"localhost"`
	DBPort        int    `env:"DB_PORT" envDefault:"3306"`
	DBName        string `env:"DB_NAME" envDefault:"flare_ftso_indexer_test"`
	DBUsername    string `env:"DB_USERNAME" envDefault:"root"`
	DBPassword    string `env:"DB_PASSWORD" envDefault:"root"`
	MockChainPort int    `env:"MOCK_CHAIN_PORT" envDefault:"5500"`
}

func TestIndexer(t *testing.T) {
	var tCfg testConfig
	if err := env.Parse(&tCfg); err != nil {
		t.Fatal("Config parse error:", err)
	}

	// mock blockchain
	go func() {
		err := indexer_testing.MockChain(
			tCfg.MockChainPort,
			"../testing/chain_copy/blocks.json",
			"../testing/chain_copy/transactions.json",
		)
		if err != nil {
			logger.Fatal("Mock chain error: %s", err)
		}
	}()

	time.Sleep(3 * time.Second)
	indexer_testing.ChainLastBlock = 2000

	// set configuration parameters
	mockChainAddress := fmt.Sprintf("http://localhost:%d", tCfg.MockChainPort)
	cfgChain := config.ChainConfig{NodeURL: mockChainAddress}

	// for the test we do not use finalizations
	collectTransactions := []config.TransactionInfo{
		{
			ContractAddress: "22474d350ec2da53d717e30b96e9a2b7628ede5b",
			FuncSig:         "f14fcbc8",
			Status:          true,
			CollectEvents:   true,
		},
		{
			ContractAddress: "22474d350ec2da53d717e30b96e9a2b7628ede5b",
			FuncSig:         "4369af80",
			Status:          true,
			CollectEvents:   true,
		},
		{
			ContractAddress: "22474d350ec2da53d717e30b96e9a2b7628ede5b",
			FuncSig:         "46f073cf",
			Status:          true,
			CollectEvents:   true,
		},
		{
			ContractAddress: "22474d350ec2da53d717e30b96e9a2b7628ede5b",
			FuncSig:         "901d0e19",
			Status:          true,
			CollectEvents:   true,
		},
		{
			ContractAddress: "b682deef4f8e298d86bfc3e21f50c675151fb974",
			FuncSig:         "2636434d",
			Status:          true,
			CollectEvents:   false,
		},
	}

	cfgIndexer := config.IndexerConfig{
		StartIndex: 50, StopIndex: 2400, BatchSize: 500, NumParallelReq: 4,
		NewBlockCheckMillis: 200, TimeoutMillis: 100, CollectTransactions: collectTransactions,
	}
	cfgLog := config.LoggerConfig{Level: "DEBUG", Console: true, File: "../logger/logs/flare-ftso-indexer_test.log"}
	cfgDB := config.DBConfig{
		Host: tCfg.DBHost, Port: tCfg.DBPort, Database: tCfg.DBName,
		Username: tCfg.DBUsername, Password: tCfg.DBPassword, DropTableAtStart: true,
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

	ethClient, err := ethclient.Dial(cfg.Chain.NodeURL)
	if err != nil {
		logger.Fatal("Could not connect to the Ethereum node: %s", err)
	}

	cfg.Indexer.StartIndex, err = database.GetMinBlockWithHistoryDrop(cfg.Indexer.StartIndex, historyDropIntervalSeconds, ethClient)
	if err != nil {
		logger.Fatal("Could not set the starting index: %s", err)
	}

	// create the indexer
	cIndexer := CreateBlockIndexer(&cfg, db, ethClient)
	// index history with parallel processing
	err = cIndexer.IndexHistory()
	if err != nil {
		logger.Fatal("History run error: %s", err)
	}

	// at the mock server add new blocks after some time
	go increaseLastBlockAndStop()

	// turn on the function to delete in the database everything that
	// is older than the historyDrop interval
	go database.DropHistory(db, historyDropIntervalSeconds, database.HistoryDropIntervalCheck, ethClient)

	// run indexer
	err = cIndexer.IndexContinuous()
	if err != nil {
		logger.Fatal("Continuous run error: %s", err)
	}

	// correctness check
	states, err := database.GetDBStates(db)
	assert.NoError(t, err)
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
