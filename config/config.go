package config

import (
	"flag"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/kelseyhightower/envconfig"
)

var (
	// ConfigFile           string                       = "config.toml"
	ReqRepeats           int                          = 10
	TimeoutMillisDefault int                          = 1000
	GlobalConfigCallback ConfigCallback[GlobalConfig] = ConfigCallback[GlobalConfig]{}
	CfgFlag                                           = flag.String("config", "config.toml", "Configuration file (toml format)")
)

type GlobalConfig interface {
	LoggerConfig() LoggerConfig
	ChainConfig() ChainConfig
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
	Host       string `toml:"host" envconfig:"DB_HOST"`
	Port       int    `toml:"port" envconfig:"DB_PORT"`
	Database   string `toml:"database" envconfig:"DB_DATABASE"`
	Username   string `toml:"username" envconfig:"DB_USERNAME"`
	Password   string `toml:"password" envconfig:"DB_PASSWORD"`
	LogQueries bool   `toml:"log_queries"`
	OptTables  string `toml:"opt_tables"`
}

type ChainConfig struct {
	NodeURL string `toml:"node_url" envconfig:"CHAIN_NODE_URL"`
}

type IndexerConfig struct {
	BatchSize           int    `toml:"batch_size"`
	StartIndex          int    `toml:"start_index"`
	StopIndex           int    `toml:"stop_index"`
	NumParallelReq      int    `toml:"num_parallel_req"`
	NewBlockCheckMillis int    `toml:"new_block_check_millis"`
	TimeoutMillis       int    `toml:"timeout_millis"`
	Receipts            string `toml:"receipts"`
}

// todo
type EpochConfig struct {
	FirstEpochStartSec int `toml:"first_epoch_start_sec" envconfig:"FIRST_EPOCH_START_SEC"`
	EpochDurationSec   int `toml:"epoch_duration_sec" envconfig:"EPOCH_DURATION_SEC"`
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
	err = ReadEnv(cfg)
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

func ReadEnv(cfg interface{}) error {
	err := envconfig.Process("", cfg)
	if err != nil {
		return fmt.Errorf("error reading env config: %w", err)
	}
	return nil
}

func (c Config) LoggerConfig() LoggerConfig {
	return c.Logger
}

func (c Config) ChainConfig() ChainConfig {
	return c.Chain
}
