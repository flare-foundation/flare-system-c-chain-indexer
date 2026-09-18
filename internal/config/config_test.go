package config

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %s", err)
	}
	return path
}

// Renamed config keys must fail loudly at startup rather than being silently
// ignored (which would let the old value drop to a default).
func TestParseConfigFileRejectsRenamedKeys(t *testing.T) {
	tests := []struct {
		name    string
		content string
		oldKey  string
		newKey  string
	}{
		{
			name:    "num_parallel_req",
			content: "[indexer]\nnum_parallel_req = 10\n",
			oldKey:  "indexer.num_parallel_req",
			newKey:  "indexer.rpc_concurrency",
		},
		{
			name:    "timeout_millis",
			content: "[timeout]\ntimeout_millis = 2000\n",
			oldKey:  "timeout.timeout_millis",
			newKey:  "timeout.rpc_timeout_millis",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := parseConfigFile(&Config{}, writeTempConfig(t, tc.content))
			if err == nil {
				t.Fatalf("expected error for renamed key %q, got nil", tc.oldKey)
			}
			if !strings.Contains(err.Error(), tc.oldKey) || !strings.Contains(err.Error(), tc.newKey) {
				t.Fatalf("error should name both old and new keys, got: %s", err)
			}
		})
	}
}

func TestNormalizeIndexerConfigRejectsExcessiveHistoryEpochs(t *testing.T) {
	cfg := IndexerConfig{Mode: IndexerModeFsp, HistoryEpochs: maxHistoryEpochs + 1}
	if err := normalizeIndexerConfig(&cfg); err == nil {
		t.Fatal("expected error for history_epochs above the max, got nil")
	}
}

func TestGetHistoryDropRejectsExcessiveValue(t *testing.T) {
	tooLarge := maxHistoryDropSeconds + 1
	cfg := DBConfig{HistoryDrop: &tooLarge}
	if _, err := cfg.GetHistoryDrop(context.Background(), big.NewInt(int64(chain.ChainIDFlare))); err == nil {
		t.Fatal("expected error for history_drop above the max, got nil")
	}
}

func TestParseConfigFileAcceptsCurrentTimeoutKey(t *testing.T) {
	cfg := &Config{}
	path := writeTempConfig(t, "[timeout]\nrpc_timeout_millis = 5000\n")
	if err := parseConfigFile(cfg, path); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if cfg.Timeout.RPCTimeoutMillis != 5000 {
		t.Fatalf("rpc_timeout_millis not decoded: got %d", cfg.Timeout.RPCTimeoutMillis)
	}
}

// max_lag_seconds defaults to 60 when absent and an explicit 0 disables the
// check, so normalizeIndexerConfig must not treat 0 as "unset" for this key.
func TestMaxLagSecondsDefaultAndDisable(t *testing.T) {
	cfg, err := buildConfig(writeTempConfig(t, "[indexer]\nconfirmations = 1\n"))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if cfg.Indexer.MaxLagSeconds != 60 {
		t.Errorf("absent max_lag_seconds = %v, want the default 60", cfg.Indexer.MaxLagSeconds)
	}
	if cfg.Indexer.NoNewBlocksDelayWarning != 60 {
		t.Errorf("absent no_new_blocks_delay_warning = %v, want the default 60", cfg.Indexer.NoNewBlocksDelayWarning)
	}

	cfg, err = buildConfig(writeTempConfig(t, "[indexer]\nmax_lag_seconds = 0\n"))
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if cfg.Indexer.MaxLagSeconds != 0 {
		t.Errorf("explicit max_lag_seconds = 0 must stay 0 (disabled), got %v", cfg.Indexer.MaxLagSeconds)
	}
}
