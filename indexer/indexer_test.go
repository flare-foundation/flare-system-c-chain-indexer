package indexer

import (
	"context"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/logger"
	indexer_testing "flare-ftso-indexer/testing"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type testConfig struct {
	DBHost          string `toml:"test_host"`
	DBPort          int    `toml:"test_port"`
	DBName          string `toml:"test_database_indexer"`
	DBUsername      string `toml:"test_username"`
	DBPassword      string `toml:"test_password"`
	MockChainPort   int    `toml:"test_mock_chain_port"`
	RecorderNodeURL string
	ResponsesFile   string
}

func TestIndexer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	tCfg := testConfig{}
	tCfg.ResponsesFile = "../testing/chain_copy/responses.json"

	_, err := toml.DecodeFile("../testing/config_test.toml", &tCfg)
	require.NoError(t, err, "Could not parse config file")

	applyEnvOverrides(&tCfg)

	// set configuration parameters
	mockChainAddress := fmt.Sprintf("http://localhost:%d", tCfg.MockChainPort)
	cfgChain := config.ChainConfig{NodeURL: mockChainAddress, ChainType: chain.ChainTypeAvax}

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
		StartIndex: 1112, StopIndex: 2400, BatchSize: 500, NumParallelReq: 4,
		NewBlockCheckMillis: 200, CollectTransactions: collectTransactions,
	}
	cfgLog := config.LoggerConfig{Level: "DEBUG", Console: true, File: "../logger/logs/flare-ftso-indexer_test.log"}
	cfgDB := config.DBConfig{
		Host: tCfg.DBHost, Port: tCfg.DBPort, Database: tCfg.DBName,
		Username: tCfg.DBUsername, Password: tCfg.DBPassword, DropTableAtStart: true,
	}
	cfg := config.Config{Indexer: cfgIndexer, Chain: cfgChain, Logger: cfgLog, DB: cfgDB}
	config.GlobalConfigCallback.Call(cfg)

	// mock blockchain
	mockChain, err := indexer_testing.NewMockChain(
		tCfg.MockChainPort,
		tCfg.ResponsesFile,
		tCfg.RecorderNodeURL,
	)
	require.NoError(t, err)

	// connect to the database
	db, err := database.ConnectAndInitialize(ctx, &cfg.DB)
	if err != nil {
		logger.Fatal("Database connect and initialize error: %s", err)
	}

	err = runIndexer(ctx, mockChain, db, &cfg)
	require.NoError(t, err)

	// correctness check
	states, err := database.UpdateDBStates(ctx, db)
	require.NoError(t, err)

	// Set the update timestamps to zero for the snapshot as these will
	// vary with current system  time. Also set the IDs to zero as these
	// depend on a race condition between the different states being
	// inserted concurrently.
	for _, state := range states.States {
		state.Updated = time.Time{}
		state.ID = 0
	}

	cupaloy.SnapshotT(t, states)
}

var envOverrides = map[string]func(*testConfig, string){
	"TEST_DB_HOST":      func(c *testConfig, v string) { c.DBHost = v },
	"TEST_DB_PORT":      func(c *testConfig, v string) { c.DBPort = mustParseInt(v) },
	"TEST_DB_NAME_MAIN": func(c *testConfig, v string) { c.DBName = v },
	"TEST_DB_USERNAME":  func(c *testConfig, v string) { c.DBUsername = v },
	"TEST_DB_PASSWORD":  func(c *testConfig, v string) { c.DBPassword = v },
}

func mustParseInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		panic(errors.Wrapf(err, "Could not parse integer from value: %s", value))
	}
	return parsed
}

func applyEnvOverrides(cfg *testConfig) {
	for env, override := range envOverrides {
		if val, ok := os.LookupEnv(env); ok {
			override(cfg, val)
		}
	}
}

func runIndexer(ctx context.Context, mockChain *indexer_testing.MockChain, db *gorm.DB, cfg *config.Config) error {
	go func() {
		if err := mockChain.Run(ctx); err != nil {
			logger.Fatal("Mock chain error: %s", err)
		}
	}()

	defer func() {
		if err := mockChain.Stop(); err != nil {
			logger.Error("Mock chain stop error: %s", err)
		}
	}()

	time.Sleep(3 * time.Second)

	nodeURL, err := cfg.Chain.FullNodeURL()
	if err != nil {
		logger.Fatal("Invalid node URL in config: %s", err)
	}

	ethClient, err := chain.DialRPCNode(nodeURL, cfg.Chain.ChainType)
	if err != nil {
		logger.Fatal("Could not connect to the Ethereum node: %s", err)
	}

	// create the indexer
	cIndexer, err := CreateBlockIndexer(cfg, db, ethClient)
	if err != nil {
		logger.Fatal("Create indexer error: %s", err)
	}

	// index history with parallel processing
	historyLastIndex, err := cIndexer.IndexHistory(ctx)
	if err != nil {
		logger.Fatal("History run error: %s", err)
	}

	if historyLastIndex != cfg.Indexer.StopIndex {
		logger.Fatal(
			"History indexing did not reach the stop index: last %d, stop %d",
			historyLastIndex, cfg.Indexer.StopIndex,
		)
	}

	return nil
}
