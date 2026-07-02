package config

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"

	"github.com/BurntSushi/toml"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
	"github.com/pkg/errors"
)

const (
	day                            time.Duration   = 24 * time.Hour
	defaultConfirmations                           = 1
	defaultChainType               chain.ChainType = chain.ChainTypeAvax
	defaultIndexerMode                             = IndexerModeFull
	defaultFspIndexLookbackSeconds                 = uint64(2 * time.Hour / time.Second)
	defaultLogRange                                = uint64(1000)
	defaultRpcConcurrency                          = 100
	defaultBatchSize                               = uint64(1000)
	// maxHistoryEpochs guards against a config typo (e.g. an extra digit).
	maxHistoryEpochs = 1000
)

var (
	GlobalConfigCallback  ConfigCallback[GlobalConfig]
	CfgFlag                             = flag.String("config", "config.toml", "Configuration file (toml format)")
	BackoffMaxElapsedTime time.Duration = 5 * time.Minute
	// RPCTimeout bounds a single RPC attempt (block, receipt, eth_getLogs,
	// contract call). It must be generous enough for the heaviest call —
	// eth_getLogs over a full log_range on a busy or throttled endpoint —
	// since one timeout exhausting its backoff tears the indexer back down to
	// startup. Cheap calls return well under it, so a high value costs nothing
	// on the happy path.
	RPCTimeout                   time.Duration = 5 * time.Second
	mainnetMinHistoryDropSeconds               = uint64((14 * day).Seconds())
	testnetMinHistoryDropSeconds               = uint64((2 * day).Seconds())
	// maxHistoryDropSeconds guards against a config typo (e.g. wrong units).
	maxHistoryDropSeconds = uint64((3650 * day).Seconds())
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

		if tCfg.RPCTimeoutMillis > 0 {
			RPCTimeout = time.Duration(tCfg.RPCTimeoutMillis) * time.Millisecond
		}

		loggerCfg := config.LoggerConfig()
		if loggerCfg.File != "" {
			_ = os.MkdirAll(filepath.Dir(loggerCfg.File), 0o755)
		}
		logger.Set(logger.Config{
			Level:       loggerCfg.Level,
			File:        loggerCfg.File,
			MaxFileSize: loggerCfg.MaxFileSize,
			Console:     loggerCfg.Console,
		})
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

	historyDrop := *db.HistoryDrop
	if historyDrop != 0 && historyDrop < minHistoryDropSeconds {
		return 0, errors.Errorf(
			"history drop must be at least %d seconds, got %d seconds",
			minHistoryDropSeconds,
			historyDrop,
		)
	}
	if historyDrop > maxHistoryDropSeconds {
		return 0, errors.Errorf(
			"history drop must be at most %d seconds, got %d seconds",
			maxHistoryDropSeconds,
			historyDrop,
		)
	}

	return historyDrop, nil
}

type ChainConfig struct {
	NodeURL   string          `toml:"node_url"`
	APIKey    string          `toml:"api_key"`
	ChainType chain.ChainType `toml:"chain_type"`
}

type IndexerConfig struct {
	// BatchSize is the number of blocks processed per batch and committed in a
	// single database transaction (i.e. the DB commit size and in-memory working
	// set). It does not affect RPC request sizes, and is independent of
	// RpcConcurrency and LogRange. Within a batch, rows are inserted in fixed
	// chunks of database.DBTransactionBatchesSize, not BatchSize.
	BatchSize  uint64 `toml:"batch_size"`
	StartIndex uint64 `toml:"start_index"`
	StopIndex  uint64 `toml:"stop_index"`
	Mode       string `toml:"mode"`
	// HistoryEpochs is FSP-mode only: number of past reward epochs whose
	// metadata events are backfilled at startup and retained by history drop.
	// 0 falls back to a short lookback window (see defaultFspIndexLookbackSeconds).
	HistoryEpochs uint64 `toml:"history_epochs"`
	// RpcConcurrency is the max number of simultaneous RPC calls of any kind —
	// block, receipt and log (eth_getLogs) fetches, plus contract calls and
	// history-drop lookups — enforced process-wide in chain.Client.
	RpcConcurrency int `toml:"rpc_concurrency"`
	// LogRange is the max blocks per eth_getLogs (FilterLogs) request,
	// bounded by the RPC node's getLogs cap (typically 100-10000).
	LogRange                uint64            `toml:"log_range"`
	NewBlockCheckMillis     int               `toml:"new_block_check_millis"`
	CollectTransactions     []TransactionInfo `toml:"collect_transactions"`
	CollectLogs             []LogInfo         `toml:"collect_logs"`
	Confirmations           uint64            `toml:"confirmations"`
	NoNewBlocksDelayWarning float64           `toml:"no_new_blocks_delay_warning"`
	FspTxLookbackSeconds    uint64            `toml:"fsp_tx_lookback_seconds"`
}

const (
	IndexerModeFull = "full"
	IndexerModeFsp  = "fsp"
)

func (c IndexerConfig) IsFspMode() bool {
	return c.Mode == IndexerModeFsp
}

type TimeoutConfig struct {
	BackoffMaxElapsedTimeSeconds *int `toml:"backoff_max_elapsed_time_seconds"`
	RPCTimeoutMillis             int  `toml:"rpc_timeout_millis"`
}

type TransactionInfo struct {
	ContractAddress string `toml:"contract_address"`
	ContractName    string `toml:"contract_name"`
	FuncSig         string `toml:"func_sig"`
	Status          bool   `toml:"status"`
	CollectEvents   bool   `toml:"collect_events"`
}

type LogInfo struct {
	ContractAddress string `toml:"contract_address"`
	ContractName    string `toml:"contract_name"`
	Topic           string `toml:"topic"`
}

func BuildConfig() (*Config, error) {
	cfgFileName := *CfgFlag

	// Set default values for the config
	cfg := &Config{
		Indexer: IndexerConfig{
			Confirmations:        defaultConfirmations,
			Mode:                 defaultIndexerMode,
			LogRange:             defaultLogRange,
			RpcConcurrency:       defaultRpcConcurrency,
			BatchSize:            defaultBatchSize,
			FspTxLookbackSeconds: defaultFspIndexLookbackSeconds,
		},
		Chain: ChainConfig{ChainType: defaultChainType},
	}

	err := parseConfigFile(cfg, cfgFileName)
	if err != nil {
		return nil, err
	}

	applyEnvOverrides(cfg)
	if err := normalizeIndexerConfig(&cfg.Indexer); err != nil {
		return nil, err
	}

	return cfg, nil
}

func normalizeIndexerConfig(cfg *IndexerConfig) error {
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	if cfg.Mode == "" {
		cfg.Mode = defaultIndexerMode
	}
	if cfg.Mode != IndexerModeFull && cfg.Mode != IndexerModeFsp {
		return errors.Errorf(
			"invalid indexer mode %q: must be %q or %q",
			cfg.Mode, IndexerModeFull, IndexerModeFsp,
		)
	}

	if cfg.Mode == IndexerModeFsp {
		cfg.CollectTransactions, cfg.CollectLogs = mergeFspCollectors(
			cfg.CollectTransactions,
			cfg.CollectLogs,
		)
	}

	if cfg.HistoryEpochs > maxHistoryEpochs {
		return errors.Errorf("indexer.history_epochs must be at most %d, got %d", maxHistoryEpochs, cfg.HistoryEpochs)
	}

	if cfg.LogRange == 0 {
		cfg.LogRange = defaultLogRange
	}
	if cfg.RpcConcurrency <= 0 {
		cfg.RpcConcurrency = defaultRpcConcurrency
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = defaultBatchSize
	}
	if cfg.FspTxLookbackSeconds == 0 {
		cfg.FspTxLookbackSeconds = defaultFspIndexLookbackSeconds
	}

	return nil
}

func parseConfigFile(cfg *Config, fileName string) error {
	content, err := os.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("error opening config file: %w", err)
	}

	md, err := toml.Decode(string(content), cfg)
	if err != nil {
		return fmt.Errorf("error parsing config file: %w", err)
	}
	renamedKeys := map[string]string{
		"indexer.num_parallel_req": "indexer.rpc_concurrency",
		"timeout.timeout_millis":   "timeout.rpc_timeout_millis",
	}
	for _, key := range md.Undecoded() {
		if newName, ok := renamedKeys[key.String()]; ok {
			return fmt.Errorf("config key %q has been renamed to %q", key.String(), newName)
		}
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
	"DB_HOST": func(c *Config, v string) { c.DB.Host = v },
	"DB_PORT": func(c *Config, v string) {
		port, err := strconv.Atoi(v)
		if err == nil {
			c.DB.Port = port
		} else {
			// The logger is not yet initialized here, so we use fmt.Printf
			fmt.Printf("ERROR: Invalid DB_PORT value: %s\n", v)
		}
	},
	"DB_DATABASE":  func(c *Config, v string) { c.DB.Database = v },
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
