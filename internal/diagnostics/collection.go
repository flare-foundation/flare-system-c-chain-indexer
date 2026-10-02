// Package diagnostics contains human-readable logging helpers only.
package diagnostics

import (
	"strings"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
)

const undefined = "undefined"

// LogIndexerPolicy logs what the indexer will collect, one line per filter,
// with hex-encoded function selectors and topic hashes.
func LogIndexerPolicy(cfg config.IndexerConfig) {
	logger.Infow(
		"Collection policy",
		"transaction_filters", len(cfg.CollectTransactions),
		"log_filters", len(cfg.CollectLogs),
	)
	for i := range cfg.CollectTransactions {
		tx := &cfg.CollectTransactions[i]
		logger.Infow(
			"Transaction filter",
			"contract_name", valueOrAny(tx.ContractName),
			"contract_address", valueOrAny(tx.ContractAddress),
			"func_sig", formatHexOrAny(tx.FuncSig),
			"status", tx.Status,
			"collect_events", tx.CollectEvents,
		)
	}
	for i := range cfg.CollectLogs {
		lg := &cfg.CollectLogs[i]
		logger.Infow(
			"Log filter",
			"contract_name", valueOrAny(lg.ContractName),
			"contract_address", valueOrAny(lg.ContractAddress),
			"topic", formatHexOrAny(lg.Topic),
		)
	}
}

// LogFspEventFilter logs the contract+topic pairs used for FSP event-range
// backfilling.
func LogFspEventFilter(logs []config.LogInfo) {
	logger.Infow("FSP event backfill filter", "entries", len(logs))
	for i := range logs {
		lg := &logs[i]
		logger.Infow(
			"FSP event filter",
			"contract_name", valueOrAny(lg.ContractName),
			"contract_address", valueOrAny(lg.ContractAddress),
			"topic", formatHexOrAny(lg.Topic),
		)
	}
}

// valueOrAny renders a filter value, with <any> for one that constrains
// nothing. Name and address are separate fields so each can be filtered on.
func valueOrAny(value string) string {
	v := strings.TrimSpace(value)
	if v == "" || strings.EqualFold(v, undefined) {
		return "<any>"
	}

	return v
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
