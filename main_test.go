package main_test

import (
	"context"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer"
	"log"
	"strings"
	"testing"

	"github.com/ava-labs/coreth/ethclient"
	"github.com/bradleyjkemp/cupaloy/v2"
	"github.com/caarlos0/env/v10"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	contractAddress    = "0x694905ca5f9F6c49f4748E8193B3e8053FA9E7E4"
	startBlock         = 6446256
	endBlockHistory    = 6447813
	endBlockContinuous = 6446306
)

type testConfig struct {
	DBHost     string `env:"DB_HOST" envDefault:"localhost"`
	DBPort     int    `env:"DB_PORT" envDefault:"3306"`
	DBName     string `env:"DB_NAME" envDefault:"flare_ftso_indexer_test"`
	DBUsername string `env:"DB_USERNAME" envDefault:"root"`
	DBPassword string `env:"DB_PASSWORD" envDefault:"root"`

	// This should be a Coston2 node.
	NodeURL    string `env:"NODE_URL" envDefault:"http://localhost:8545"`
	NodeAPIKey string `env:"NODE_API_KEY"`
}

func TestIntegration(t *testing.T) {
	ctx := context.Background()

	var tCfg testConfig
	err := env.Parse(&tCfg)
	require.NoError(t, err, "Could not parse test config")

	t.Run("IndexHistory", func(t *testing.T) {
		t.Log("Indexing history")

		cfg := initConfig(tCfg, true)

		db, err := database.ConnectAndInitialize(ctx, &cfg.DB)
		require.NoError(t, err, "Could not connect to the database")

		indexer, err := createIndexer(&cfg, db)
		require.NoError(t, err, "Could not create the indexer")

		err = indexer.IndexHistory(ctx)
		require.NoError(t, err, "Could not run the indexer")

		checkDB(ctx, t, db, &cfg)
	})

	t.Run("IndexContinuous", func(t *testing.T) {
		t.Log("Indexing continuous")

		cfg := initConfig(tCfg, false)

		db, err := database.ConnectAndInitialize(ctx, &cfg.DB)
		require.NoError(t, err, "Could not connect to the database")

		indexer, err := createIndexer(&cfg, db)
		require.NoError(t, err, "Could not create the indexer")

		err = indexer.IndexContinuous(ctx)
		require.NoError(t, err, "Could not run the indexer")

		checkDB(ctx, t, db, &cfg)
	})
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
	}

	logInfo := config.LogInfo{
		ContractAddress: contractAddress,
		Topic:           "undefined",
	}

	cfg := config.Config{
		Indexer: config.IndexerConfig{
			BatchSize:           500,
			StartIndex:          startBlock,
			StopIndex:           endBlock,
			NumParallelReq:      16,
			LogRange:            10,
			NewBlockCheckMillis: 1000,
			CollectTransactions: []config.TransactionInfo{txInfo},
			CollectLogs:         []config.LogInfo{logInfo},
		},
		Chain: config.ChainConfig{
			NodeURL: tCfg.NodeURL,
			APIKey:  tCfg.NodeAPIKey,
		},
		Logger: config.LoggerConfig{
			Level:       "DEBUG",
			File:        "flare-indexer-inttest.log",
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
			HistoryDrop:      0,
			DropTableAtStart: true,
		},
	}

	config.GlobalConfigCallback.Call(cfg)

	return cfg
}

func createIndexer(cfg *config.Config, db *gorm.DB) (*indexer.BlockIndexer, error) {
	ethClient, err := dialRPCNode(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "Could not connect to the RPC nodes")
	}

	return indexer.CreateBlockIndexer(cfg, db, ethClient)
}

func dialRPCNode(cfg *config.Config) (ethclient.Client, error) {
	nodeURL, err := cfg.Chain.FullNodeURL()
	if err != nil {
		return nil, err
	}

	return ethclient.Dial(nodeURL.String())
}

func checkDB(ctx context.Context, t *testing.T, db *gorm.DB, cfg *config.Config) {
	t.Run("check transactions", func(t *testing.T) {
		var transactions []database.Transaction
		result := db.WithContext(ctx).Order("hash ASC").Find(&transactions)
		require.NoError(t, result.Error, "Could not find transactions")

		log.Printf("Found %d transactions", len(transactions))

		checkTransactions(t, transactions, cfg)

		zeroTransactionIDs(transactions)
		cupaloy.SnapshotT(t, transactions)
	})

	t.Run("check logs", func(t *testing.T) {
		var logs []database.Log
		result := db.WithContext(ctx).
			Preload("Transaction").
			Order("transaction_hash ASC, log_index ASC").
			Find(&logs)
		require.NoError(t, result.Error, "Could not find logs")

		log.Printf("Found %d logs", len(logs))

		checkLogs(t, logs, cfg)

		zeroLogIDs(logs)
		cupaloy.SnapshotT(t, logs)
	})
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

// For both Transactions and Logs, the ID is not deterministic and
// irrelevant for the test. These functions zero out the IDs so that
// the snapshots are deterministic.
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
