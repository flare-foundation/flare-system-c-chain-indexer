package config

import (
	"context"
	"flag"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/logger"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/pkg/errors"
)

const (
	day                  time.Duration   = 24 * time.Hour
	defaultConfirmations                 = 1
	defaultChainType     chain.ChainType = chain.ChainTypeAvax
)

var (
	GlobalConfigCallback         ConfigCallback[GlobalConfig]
	CfgFlag                                    = flag.String("config", "config.toml", "Configuration file (toml format)")
	BackoffMaxElapsedTime        time.Duration = 5 * time.Minute
	Timeout                      time.Duration = time.Second
	mainnetMinHistoryDropSeconds               = uint64((10 * day).Seconds())
	testnetMinHistoryDropSeconds               = uint64((2 * day).Seconds())
)

var minHistoryDropSecondsByChain = map[chain.ChainID]uint64{
	chain.ChainIDFlare:    mainnetMinHistoryDropSeconds,
	chain.ChainIDSongbird: mainnetMinHistoryDropSeconds,
	chain.ChainIDCoston:   testnetMinHistoryDropSeconds,
	chain.ChainIDCoston2:  testnetMinHistoryDropSeconds,
}

func init() {
	GlobalConfigCallback.AddCallback(func(config GlobalConfig) {
		tCfg := config.TimeoutConfig()

		if tCfg.BackoffMaxElapsedTimeSeconds != nil {
			BackoffMaxElapsedTime = time.Duration(*tCfg.BackoffMaxElapsedTimeSeconds) * time.Second
		}

		if tCfg.TimeoutMillis > 0 {
			Timeout = time.Duration(tCfg.TimeoutMillis) * time.Millisecond
		}

		loggerCfg := config.LoggerConfig()
		logger.InitializeLogger(
			loggerCfg.Console,
			loggerCfg.File,
			loggerCfg.Level,
			loggerCfg.MaxFileSize,
		)
	})
}

type GlobalConfig interface {
	LoggerConfig() LoggerConfig
	TimeoutConfig() TimeoutConfig
}

type Config struct {
	DB      DBConfig      `toml:"db"`
	Logger  LoggerConfig  `toml:"logger"`
	Chain   ChainConfig   `toml:"chain"`
	Indexer IndexerConfig `toml:"indexer"`
	Timeout TimeoutConfig `toml:"timeout"`
}

type LoggerConfig struct {
	Level       string `toml:"level"` // valid values are: DEBUG, INFO, WARN, ERROR, DPANIC, PANIC, FATAL (zap)
	File        string `toml:"file"`
	MaxFileSize int    `toml:"max_file_size"` // In megabytes
	Console     bool   `toml:"console"`
}

type DBConfig struct {
	Host       string `toml:"host"`
	Port       int    `toml:"port"`
	Database   string `toml:"database"`
	Username   string `toml:"username"`
	Password   string `toml:"password"`
	LogQueries bool   `toml:"log_queries"`

	// Using a pointer to distinguish between unset and zero value - the latter
	// disables history drop.
	HistoryDrop      *uint64 `toml:"history_drop"`
	DropTableAtStart bool    `toml:"drop_table_at_start"`
}

func (db *DBConfig) GetHistoryDrop(ctx context.Context, chainIDBig *big.Int) (uint64, error) {
	chainID := chain.ChainIDFromBigInt(chainIDBig)

	minHistoryDropSeconds, ok := minHistoryDropSecondsByChain[chainID]
	if !ok {
		minHistoryDropSeconds = mainnetMinHistoryDropSeconds // Default to mainnet if chain not recognized
	}

	if db.HistoryDrop == nil {
		return minHistoryDropSeconds, nil // Use default if not set
	}

	if *db.HistoryDrop < minHistoryDropSeconds {
		return 0, errors.Errorf(
			"history drop must be at least %d seconds, got %d seconds",
			minHistoryDropSeconds,
			db.HistoryDrop,
		)
	}

	return *db.HistoryDrop, nil
}

type ChainConfig struct {
	NodeURL   string          `toml:"node_url"`
	APIKey    string          `toml:"api_key"`
	ChainType chain.ChainType `toml:"chain_type"`
}

type IndexerConfig struct {
	BatchSize               uint64            `toml:"batch_size"`
	StartIndex              uint64            `toml:"start_index"`
	StopIndex               uint64            `toml:"stop_index"`
	NumParallelReq          int               `toml:"num_parallel_req"`
	LogRange                uint64            `toml:"log_range"`
	NewBlockCheckMillis     int               `toml:"new_block_check_millis"`
	CollectTransactions     []TransactionInfo `toml:"collect_transactions"`
	CollectLogs             []LogInfo         `toml:"collect_logs"`
	Confirmations           uint64            `toml:"confirmations"`
	NoNewBlocksDelayWarning float64           `toml:"no_new_blocks_delay_warning"`
}

type TimeoutConfig struct {
	BackoffMaxElapsedTimeSeconds *int `toml:"backoff_max_elapsed_time_seconds"`
	TimeoutMillis                int  `toml:"timeout_millis"`
}

type TransactionInfo struct {
	ContractAddress string `toml:"contract_address"`
	FuncSig         string `toml:"func_sig"`
	Status          bool   `toml:"status"`
	CollectEvents   bool   `toml:"collect_events"`
	Signature       bool   `toml:"signature"` // if true, the transaction signature will be collected
}

type LogInfo struct {
	ContractAddress string `toml:"contract_address"`
	Topic           string `toml:"topic"`
}

func BuildConfig() (*Config, error) {
	cfgFileName := *CfgFlag

	// Set default values for the config
	cfg := &Config{
		Indexer: IndexerConfig{Confirmations: defaultConfirmations},
		Chain:   ChainConfig{ChainType: defaultChainType},
	}

	err := parseConfigFile(cfg, cfgFileName)
	if err != nil {
		return nil, err
	}

	applyEnvOverrides(cfg)

	return cfg, nil
}

func parseConfigFile(cfg *Config, fileName string) error {
	content, err := os.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("error opening config file: %w", err)
	}

	_, err = toml.Decode(string(content), cfg)
	if err != nil {
		return fmt.Errorf("error parsing config file: %w", err)
	}
	return nil
}

func (c Config) LoggerConfig() LoggerConfig {
	return c.Logger
}

func (c Config) TimeoutConfig() TimeoutConfig {
	return c.Timeout
}

func (cc ChainConfig) FullNodeURL() (*url.URL, error) {
	u, err := url.Parse(cc.NodeURL)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing node url")
	}

	if cc.APIKey != "" {
		q := u.Query()
		q.Set("x-apikey", cc.APIKey)
		u.RawQuery = q.Encode()
	}

	return u, nil
}

var envOverrides = map[string]func(*Config, string){
	"DB_USERNAME":  func(c *Config, v string) { c.DB.Username = v },
	"DB_PASSWORD":  func(c *Config, v string) { c.DB.Password = v },
	"NODE_URL":     func(c *Config, v string) { c.Chain.NodeURL = v },
	"NODE_API_KEY": func(c *Config, v string) { c.Chain.APIKey = v },
}

func applyEnvOverrides(cfg *Config) {
	for env, override := range envOverrides {
		if val, ok := os.LookupEnv(env); ok {
			override(cfg, val)
		}
	}
}
