package config

import (
	"flag"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
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
	Epochs  EpochConfig   `toml:"epochs"`
}

type LoggerConfig struct {
	Level       string `toml:"level"` // valid values are: DEBUG, INFO, WARN, ERROR, DPANIC, PANIC, FATAL (zap)
	File        string `toml:"file"`
	MaxFileSize int    `toml:"max_file_size"` // In megabytes
	Console     bool   `toml:"console"`
}

type DBConfig struct {
	Host        string `toml:"host"`
	Port        int    `toml:"port"`
	Database    string `toml:"database"`
	Username    string `toml:"username"`
	Password    string `toml:"password"`
	LogQueries  bool   `toml:"log_queries"`
	OptTables   string `toml:"opt_tables"`
	HistoryDrop int    `toml:"history_drop"`
}

type ChainConfig struct {
	NodeURL string `toml:"node_url"`
}

type IndexerConfig struct {
	BatchSize           int              `toml:"batch_size"`
	StartIndex          int              `toml:"start_index"`
	StopIndex           int              `toml:"stop_index"`
	NumParallelReq      int              `toml:"num_parallel_req"`
	NewBlockCheckMillis int              `toml:"new_block_check_millis"`
	TimeoutMillis       int              `toml:"timeout_millis"`
	Collect             [][4]interface{} `toml:"collect"`
}

// todo: should this be fixed?
type EpochConfig struct {
	FirstEpochStartSec int `toml:"first_epoch_start_sec"`
	EpochDurationSec   int `toml:"epoch_duration_sec"`
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
