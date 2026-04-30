package config

import (
	"testing"

	"github.com/flare-foundation/go-flare-common/pkg/contracts/fumanager"
	"github.com/flare-foundation/go-flare-common/pkg/contracts/system"
)

func TestNormalizeIndexerConfig_FspMergesDefaultAndUserCollectors(t *testing.T) {
	cfg := IndexerConfig{
		Mode: IndexerModeFsp,
		CollectTransactions: []TransactionInfo{
			{
				ContractName:  "Submission",
				FuncSig:       "6c532fae",
				CollectEvents: true,
			},
			{
				ContractName: "CustomTxContract",
				FuncSig:      "abcd1234",
				Status:       true,
			},
		},
		CollectLogs: []LogInfo{
			{
				ContractName: "Relay",
				Topic:        "",
			},
			{
				ContractName: "CustomLogContract",
				Topic:        "",
			},
		},
	}

	normalizeIndexerConfig(&cfg)

	if got, want := len(cfg.CollectTransactions), 5; got != want {
		t.Fatalf("unexpected number of transaction collectors: got=%d want=%d", got, want)
	}

	tx, ok := findTransaction(cfg.CollectTransactions, "Submission", "6c532fae")
	if !ok {
		t.Fatalf("submission collector missing after merge")
	}
	if tx.Status || !tx.CollectEvents {
		t.Fatalf("submission collector flags not merged correctly: %+v", tx)
	}

	if _, ok := findTransaction(cfg.CollectTransactions, "CustomTxContract", "abcd1234"); !ok {
		t.Fatalf("custom transaction collector missing after merge")
	}

	if !containsLog(cfg.CollectLogs, "CustomLogContract", "") {
		t.Fatalf("custom log collector missing after merge")
	}
	if !containsLog(cfg.CollectLogs, "FastUpdater", "") {
		t.Fatalf("round log collector missing after merge")
	}
	if !containsLog(cfg.CollectLogs, "FastUpdateIncentiveManager", getTopic(fumanager.FUManagerMetaData, "InflationRewardsOffered")) {
		t.Fatalf("fast update incentive manager reward epoch log collector missing after merge")
	}
	if !containsLog(cfg.CollectLogs, "FlareSystemsManager", getTopic(system.FlareSystemsManagerMetaData, "RewardEpochStarted")) {
		t.Fatalf("reward epoch log collector missing after merge")
	}
}

func TestNormalizeIndexerConfig_FullModeDoesNotInjectFspDefaults(t *testing.T) {
	cfg := IndexerConfig{
		Mode: IndexerModeFull,
		CollectTransactions: []TransactionInfo{
			{
				ContractName: "CustomOnly",
				FuncSig:      "00112233",
			},
		},
		CollectLogs: []LogInfo{
			{
				ContractName: "CustomOnly",
				Topic:        "0xabc",
			},
		},
	}

	normalizeIndexerConfig(&cfg)

	if got, want := len(cfg.CollectTransactions), 1; got != want {
		t.Fatalf("full mode should keep custom tx collectors unchanged: got=%d want=%d", got, want)
	}
	if got, want := len(cfg.CollectLogs), 1; got != want {
		t.Fatalf("full mode should keep custom log collectors unchanged: got=%d want=%d", got, want)
	}
}

func findTransaction(txs []TransactionInfo, contractName, funcSig string) (TransactionInfo, bool) {
	for _, tx := range txs {
		if tx.ContractName == contractName && tx.FuncSig == funcSig {
			return tx, true
		}
	}
	return TransactionInfo{}, false
}

func containsLog(logs []LogInfo, contractName, topic string) bool {
	for _, log := range logs {
		if log.ContractName == contractName && log.Topic == topic {
			return true
		}
	}
	return false
}
