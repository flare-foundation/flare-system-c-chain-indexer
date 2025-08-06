package main_test

import (
	"context"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"gorm.io/gorm"
)

const (
	contractAddress    = "0x694905ca5f9F6c49f4748E8193B3e8053FA9E7E4"
	startBlock         = 6446256
	endBlockHistory    = 6447813
	endBlockContinuous = 6446306
)

type testConfig struct {
	DBHost     string `toml:"test_host"`
	DBPort     int    `toml:"test_port"`
	DBName     string `toml:"test_database_main"`
	DBUsername string `toml:"test_username"`
	DBPassword string `toml:"test_password"`

	// This should be a Coston2 node.
	NodeURL    string `toml:"test_node_url"`
	NodeAPIKey string `toml:"test_api_key"`
}

type IntegrationIndex struct {
	suite.Suite
	ctx     context.Context
	cfg     config.Config
	indexer *indexer.BlockIndexer
	db      *gorm.DB
}

type IntegrationIndexContinuousSuite struct {
	IntegrationIndex
}

type IntegrationIndexHistorySuite struct {
	IntegrationIndex
}

func TestIntegrationIndexContinuous(t *testing.T) {
	testSuite := new(IntegrationIndexContinuousSuite)
	err := testSuite.prepareSuite(false)
	if err != nil {
		t.Fatal("Could not prepare the test suite:", err)
	}
	suite.Run(t, testSuite)
}

func TestIntegrationIndexHistory(t *testing.T) {
	testSuite := new(IntegrationIndexHistorySuite)
	err := testSuite.prepareSuite(true)
	if err != nil {
		t.Fatal("Could not prepare the test suite")
	}
	suite.Run(t, testSuite)
}

func (suite *IntegrationIndexContinuousSuite) SetupSuite() {
	err := suite.indexer.IndexContinuous(suite.ctx)
	require.NoError(suite.T(), err, "Could not run the indexer")
}

func (suite *IntegrationIndexHistorySuite) SetupSuite() {
	err := suite.indexer.IndexHistory(suite.ctx)
	require.NoError(suite.T(), err, "Could not run the indexer")
}

func (suite *IntegrationIndex) TestCheckBlocks() {
	var blocks []database.Block
	result := suite.db.WithContext(suite.ctx).Order("hash ASC").Find(&blocks)
	require.NoError(suite.T(), result.Error, "Could not find blocks")

	suite.T().Logf("Found %d blocks", len(blocks))

	checkBlocks(suite.T(), blocks, &suite.cfg)

	zeroBlockIDs(blocks)
	cupaloy.SnapshotT(suite.T(), blocks)
}

func (suite *IntegrationIndex) TestCheckTransactions() {
	var transactions []database.Transaction
	result := suite.db.WithContext(suite.ctx).Order("hash ASC").Find(&transactions)
	require.NoError(suite.T(), result.Error, "Could not find transactions")

	suite.T().Logf("Found %d transactions", len(transactions))

	checkTransactions(suite.T(), transactions, &suite.cfg)

	zeroTransactionIDs(transactions)
	cupaloy.SnapshotT(suite.T(), transactions)
}

func (suite *IntegrationIndex) TestCheckLogs() {
	var logs []database.Log
	result := suite.db.WithContext(suite.ctx).
		Preload("Transaction").
		Order("transaction_hash ASC, log_index ASC").
		Find(&logs)
	require.NoError(suite.T(), result.Error, "Could not find logs")

	suite.T().Logf("Found %d logs", len(logs))

	checkLogs(suite.T(), logs, &suite.cfg)

	zeroLogIDs(logs)
	cupaloy.SnapshotT(suite.T(), logs)
}

func (suite *IntegrationIndex) prepareSuite(isHistory bool) error {
	suite.ctx = context.Background()
	tCfg := testConfig{}

	_, err := toml.DecodeFile("testing/config_test.toml", &tCfg)
	if err != nil {
		return errors.Wrap(err, "Could not parse config file")
	}

	applyEnvOverrides(&tCfg)

	suite.cfg = initConfig(tCfg, isHistory)

	suite.db, err = database.ConnectAndInitialize(suite.ctx, &suite.cfg.DB)
	if err != nil {
		return errors.Wrap(err, "Could not connect to the database")
	}

	suite.indexer, err = createIndexer(&suite.cfg, suite.db)
	if err != nil {
		return errors.Wrap(err, "Could not create the indexer")
	}

	return nil
}

var envOverrides = map[string]func(*testConfig, string){
	"TEST_DB_HOST":      func(c *testConfig, v string) { c.DBHost = v },
	"TEST_DB_PORT":      func(c *testConfig, v string) { c.DBPort = mustParseInt(v) },
	"TEST_DB_NAME_MAIN": func(c *testConfig, v string) { c.DBName = v },
	"TEST_DB_USERNAME":  func(c *testConfig, v string) { c.DBUsername = v },
	"TEST_DB_PASSWORD":  func(c *testConfig, v string) { c.DBPassword = v },
	"NODE_URL":          func(c *testConfig, v string) { c.NodeURL = v },
	"NODE_API_KEY":      func(c *testConfig, v string) { c.NodeAPIKey = v },
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

func initConfig(tCfg testConfig, history bool) config.Config {
	var endBlock uint64
	if history {
		endBlock = endBlockHistory
	} else {
		endBlock = endBlockContinuous
	}

	txInfo := config.TransactionInfo{
		ContractAddress: contractAddress,
		FuncSig:         "undefined",
		Status:          true,
		CollectEvents:   true,
		Signature:       true,
	}

	logInfo := config.LogInfo{
		ContractAddress: contractAddress,
		Topic:           "undefined",
	}

	historyDrop := uint64(0)
	cfg := config.Config{
		Indexer: config.IndexerConfig{
			BatchSize:               500,
			StartIndex:              startBlock,
			StopIndex:               endBlock,
			NumParallelReq:          16,
			LogRange:                10,
			NewBlockCheckMillis:     1000,
			CollectTransactions:     []config.TransactionInfo{txInfo},
			CollectLogs:             []config.LogInfo{logInfo},
			NoNewBlocksDelayWarning: 60,
		},
		Chain: config.ChainConfig{
			NodeURL:   tCfg.NodeURL,
			APIKey:    tCfg.NodeAPIKey,
			ChainType: chain.ChainTypeAvax,
		},
		Logger: config.LoggerConfig{
			Level:       "DEBUG",
			File:        "./logger/logs/flare-indexer-inttest.log",
			MaxFileSize: 10,
			Console:     true,
		},
		DB: config.DBConfig{
			Host:             tCfg.DBHost,
			Port:             tCfg.DBPort,
			Database:         tCfg.DBName,
			Username:         tCfg.DBUsername,
			Password:         tCfg.DBPassword,
			LogQueries:       false,
			HistoryDrop:      &historyDrop,
			DropTableAtStart: true,
		},
	}

	config.GlobalConfigCallback.Call(cfg)

	return cfg
}

func createIndexer(cfg *config.Config, db *gorm.DB) (*indexer.BlockIndexer, error) {
	nodeURL, err := cfg.Chain.FullNodeURL()
	if err != nil {
		return nil, errors.Wrap(err, "Invalid node URL in config")
	}

	ethClient, err := chain.DialRPCNode(nodeURL, cfg.Chain.ChainType)
	if err != nil {
		return nil, errors.Wrap(err, "Could not connect to the RPC nodes")
	}

	return indexer.CreateBlockIndexer(cfg, db, ethClient)
}

func checkBlocks(t *testing.T, blocks []database.Block, cfg *config.Config) {
	for i := range blocks {
		block := &blocks[i]
		checkBlock(t, block, cfg)
	}
}

func checkBlock(t *testing.T, block *database.Block, cfg *config.Config) {
	require.NotEmpty(t, block.Hash, "Block hash should not be empty")
	require.GreaterOrEqual(t, block.Number, uint64(cfg.Indexer.StartIndex))
	require.LessOrEqual(t, block.Number, uint64(cfg.Indexer.StopIndex))
	require.NotZero(t, block.Timestamp, "Timestamp should not be zero")
}

func checkTransactions(t *testing.T, transactions []database.Transaction, cfg *config.Config) {
	for i := range transactions {
		tx := &transactions[i]
		checkTransaction(t, tx, cfg)
	}
}

func checkTransaction(t *testing.T, tx *database.Transaction, cfg *config.Config) {
	require.NotEmpty(t, tx.Hash, "Transaction hash should not be empty")
	require.NotEmpty(t, tx.FunctionSig, "Function signature should not be empty")
	require.NotEmpty(t, tx.Input, "Input should not be empty")
	require.GreaterOrEqual(t, tx.BlockNumber, uint64(cfg.Indexer.StartIndex))
	require.LessOrEqual(t, tx.BlockNumber, uint64(cfg.Indexer.StopIndex))
	require.NotEmpty(t, tx.BlockHash, "Block hash should not be empty")
	require.NotEmpty(t, tx.FromAddress, "From address should not be empty")
	require.True(t, compareAddrStrs(tx.ToAddress, contractAddress), "To address should be the contract address")
	require.NotEmpty(t, tx.Value, "Value should not be empty")
	require.NotEmpty(t, tx.GasPrice, "Gas price should not be empty")
	require.NotZero(t, tx.Gas, "Gas used should not be zero")
	require.NotZero(t, tx.Timestamp, "Timestamp should not be zero")
}

func checkLogs(t *testing.T, logs []database.Log, cfg *config.Config) {
	for i := range logs {
		log := &logs[i]
		checkLog(t, log, cfg)
	}
}

func checkLog(t *testing.T, log *database.Log, cfg *config.Config) {
	if tx := log.Transaction; tx != nil {
		checkTransaction(t, log.Transaction, cfg)
	}

	require.True(t, compareAddrStrs(log.Address, contractAddress), "Log address should be the contract address")
	require.NotEmpty(t, log.Data)
	require.NotEmpty(t, log.Topic0)
	require.NotEmpty(t, log.TransactionHash)
	require.NotZero(t, log.Timestamp)
}

// For Blocks, Transactions and Logs, the ID is not deterministic and
// irrelevant for the test. These functions zero out the IDs so that
// the snapshots are deterministic.
func zeroBlockIDs(blocks []database.Block) {
	for i := range blocks {
		blocks[i].ID = 0
	}
}

func zeroTransactionIDs(transactions []database.Transaction) {
	for i := range transactions {
		transactions[i].ID = 0
	}
}

func zeroLogIDs(logs []database.Log) {
	for i := range logs {
		logs[i].ID = 0
	}
}

func compareAddrStrs(expected, actual string) bool {
	return parseAddrStr(expected) == parseAddrStr(actual)
}

func parseAddrStr(addrStr string) common.Address {
	if !strings.HasPrefix(addrStr, "0x") {
		addrStr = "0x" + addrStr
	}

	return common.HexToAddress(addrStr)
}
