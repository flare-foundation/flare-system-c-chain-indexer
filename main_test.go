package main_test

import (
	"context"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer"
	"log"
	"testing"

	"github.com/bradleyjkemp/cupaloy"
	"github.com/caarlos0/env/v10"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

	cfg := initConfig(tCfg)

	db, err := database.ConnectAndInitialize(ctx, &cfg.DB)
	require.NoError(t, err, "Could not connect to the database")

	err = runIndexer(ctx, &cfg, db)
	require.NoError(t, err, "Could not run the indexer")

	checkDB(ctx, t, db)
}

func initConfig(tCfg testConfig) config.Config {
	txInfo := config.TransactionInfo{
		ContractAddress: "0x694905ca5f9F6c49f4748E8193B3e8053FA9E7E4",
		FuncSig:         "undefined",
		Status:          true,
		CollectEvents:   true,
	}

	logInfo := config.LogInfo{
		ContractAddress: "0x694905ca5f9F6c49f4748E8193B3e8053FA9E7E4",
		Topic:           "undefined",
	}

	cfg := config.Config{
		Indexer: config.IndexerConfig{
			BatchSize:           500,
			StartIndex:          6446256,
			StopIndex:           6447813,
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

func runIndexer(ctx context.Context, cfg *config.Config, db *gorm.DB) error {
	ethClient, err := dialRPCNode(cfg)
	if err != nil {
		return errors.Wrap(err, "Could not connect to the RPC nodes")
	}

	cIndexer := indexer.CreateBlockIndexer(cfg, db, ethClient)

	return cIndexer.IndexHistory(ctx)
}

func dialRPCNode(cfg *config.Config) (*ethclient.Client, error) {
	nodeURL, err := cfg.Chain.FullNodeURL()
	if err != nil {
		return nil, err
	}

	return ethclient.Dial(nodeURL.String())
}

func checkDB(ctx context.Context, t *testing.T, db *gorm.DB) {
	t.Run("check transactions", func(t *testing.T) {
		var transactions []database.Transaction
		result := db.WithContext(ctx).Order("hash ASC").Find(&transactions)
		require.NoError(t, result.Error, "Could not find transactions")

		log.Printf("Found %d transactions", len(transactions))

		zeroTransactionIDs(transactions)
		cupaloy.SnapshotT(t, transactions)
	})

	t.Run("check logs", func(t *testing.T) {
		var logs []database.Log
		result := db.WithContext(ctx).Order("transaction_hash ASC, log_index ASC").Find(&logs)
		require.NoError(t, result.Error, "Could not find logs")

		log.Printf("Found %d logs", len(logs))

		zeroLogIDs(logs)
		cupaloy.SnapshotT(t, logs)
	})
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
