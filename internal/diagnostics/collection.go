// Package diagnostics contains human-readable logging helpers only.
package diagnostics

import (
	"strings"

	"flare-ftso-indexer/internal/config"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
)

const undefined = "undefined"

// LogIndexerPolicy prints what the indexer will collect, one entry per line,
// with hex-encoded function selectors and topic hashes.
func LogIndexerPolicy(cfg config.IndexerConfig) {
	logger.Infof(
		"Indexer collection policy: %d transaction filters, %d log filters",
		len(cfg.CollectTransactions),
		len(cfg.CollectLogs),
	)
	for i := range cfg.CollectTransactions {
		tx := &cfg.CollectTransactions[i]
		logger.Infof(
			"  tx_filter: contract=%s, func_sig=%s, status=%t, collect_events=%t",
			contractRef(tx.ContractName, tx.ContractAddress),
			formatHexOrAny(tx.FuncSig),
			tx.Status,
			tx.CollectEvents,
		)
	}
	for i := range cfg.CollectLogs {
		lg := &cfg.CollectLogs[i]
		logger.Infof(
			"  log_filter: contract=%s, topic=%s",
			contractRef(lg.ContractName, lg.ContractAddress),
			formatHexOrAny(lg.Topic),
		)
	}
}

// LogFspEventFilter prints the contract+topic pairs used for FSP event-range
// backfilling.
func LogFspEventFilter(logs []config.LogInfo) {
	logger.Infof("FSP event range filter: %d entries", len(logs))
	for i := range logs {
		lg := &logs[i]
		logger.Infof(
			"  fsp_event_filter: contract=%s, topic=%s",
			contractRef(lg.ContractName, lg.ContractAddress),
			formatHexOrAny(lg.Topic),
		)
	}
}

func contractRef(name, address string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	if a := strings.TrimSpace(address); a != "" {
		return a
	}
	return "<any>"
}

func formatHexOrAny(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" || v == undefined {
		return "<any>"
	}
	if !strings.HasPrefix(v, "0x") {
		v = "0x" + v
	}
	return v
}
