package config

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/pkg/errors"
)

var (
	BackoffMaxElapsedTime time.Duration                = 5 * time.Minute
	Timeout               time.Duration                = 1000 * time.Millisecond
	GlobalConfigCallback  ConfigCallback[GlobalConfig] = ConfigCallback[GlobalConfig]{}
	CfgFlag                                            = flag.String("config", "config.toml", "Configuration file (toml format)")
)

func init() {
	GlobalConfigCallback.AddCallback(func(config GlobalConfig) {
		tCfg := config.TimeoutConfig()

		if tCfg.BackoffMaxElapsedTimeSeconds != nil {
			BackoffMaxElapsedTime = time.Duration(*tCfg.BackoffMaxElapsedTimeSeconds) * time.Second
		}

		if tCfg.TimeoutMillis > 0 {
			Timeout = time.Duration(tCfg.TimeoutMillis) * time.Millisecond
		}
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
	Host             string `toml:"host"`
	Port             int    `toml:"port"`
	Database         string `toml:"database"`
	Username         string `toml:"username"`
	Password         string `toml:"password"`
	LogQueries       bool   `toml:"log_queries"`
	HistoryDrop      uint64 `toml:"history_drop"`
	DropTableAtStart bool   `toml:"drop_table_at_start"`
}

type ChainConfig struct {
	NodeURL string `toml:"node_url"`
	APIKey  string `toml:"api_key"`
}

type IndexerConfig struct {
	BatchSize           uint64            `toml:"batch_size"`
	StartIndex          uint64            `toml:"start_index"`
	StopIndex           uint64            `toml:"stop_index"`
	NumParallelReq      int               `toml:"num_parallel_req"`
	LogRange            uint64            `toml:"log_range"`
	NewBlockCheckMillis int               `toml:"new_block_check_millis"`
	CollectTransactions []TransactionInfo `toml:"collect_transactions"`
	CollectLogs         []LogInfo         `toml:"collect_logs"`
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
}

type LogInfo struct {
	ContractAddress string `toml:"contract_address"`
	Topic           string `toml:"topic"`
}

func BuildConfig() (*Config, error) {
	cfgFileName := *CfgFlag

	cfg := new(Config)
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
