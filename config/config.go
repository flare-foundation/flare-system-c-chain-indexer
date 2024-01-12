package config

import (
	"flag"
	"fmt"
	"net/url"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/pkg/errors"
)

var (
	ReqRepeats           int                          = 20
	TimeoutMillisDefault int                          = 1000
	GlobalConfigCallback ConfigCallback[GlobalConfig] = ConfigCallback[GlobalConfig]{}
	CfgFlag                                           = flag.String("config", "config.toml", "Configuration file (toml format)")
)

type GlobalConfig interface {
	LoggerConfig() LoggerConfig
}

type Config struct {
	DB      DBConfig      `toml:"db"`
	Logger  LoggerConfig  `toml:"logger"`
	Chain   ChainConfig   `toml:"chain"`
	Indexer IndexerConfig `toml:"indexer"`
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
	HistoryDrop      int    `toml:"history_drop"`
	DropTableAtStart bool   `toml:"drop_table_at_start"`
}

type ChainConfig struct {
	NodeURL string `toml:"node_url"`
	APIKey  string `toml:"api_key"`
}

type IndexerConfig struct {
	BatchSize           int               `toml:"batch_size"`
	StartIndex          int               `toml:"start_index"`
	StopIndex           int               `toml:"stop_index"`
	NumParallelReq      int               `toml:"num_parallel_req"`
	LogRange            int               `toml:"log_range"`
	NewBlockCheckMillis int               `toml:"new_block_check_millis"`
	TimeoutMillis       int               `toml:"timeout_millis"`
	CollectTransactions []TransactionInfo `toml:"collect_transactions"`
	CollectLogs         []LogInfo         `toml:"collect_logs"`
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

func newConfig() *Config {
	return &Config{}
}

func BuildConfig() (*Config, error) {
	cfgFileName := *CfgFlag

	cfg := newConfig()
	err := ParseConfigFile(cfg, cfgFileName)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func ParseConfigFile(cfg *Config, fileName string) error {
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

func (cc ChainConfig) String() string {
	return fmt.Sprintf("NodeURL: %s, APIKey: %s", cc.NodeURL, cc.APIKey)
}
