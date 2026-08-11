package config

import (
	"strings"
	"testing"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"

	"github.com/flare-foundation/go-flare-common/pkg/contracts/fumanager"
	"github.com/flare-foundation/go-flare-common/pkg/contracts/system"
)

func TestApplyFspCollectors_MergesDefaultAndUserCollectors(t *testing.T) {
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

	ApplyFspCollectors(&cfg, chain.ChainIDFlare)

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
	if !containsLog(cfg.CollectLogs, "VoterRegistry", "0xbfb6cd90b6e2668916d9e034926c84f40bcf94094b0d625ec8eecfdeb2150ae1") {
		t.Fatalf("upgraded VoterRegistered topic missing after merge")
	}
	if !containsLog(cfg.CollectLogs, "FlareSystemsCalculator", "0xc49a5cabcc0776ace8cfd024e155bc303ee5e492b29d59f1ff7dbafa0b34a04b") {
		t.Fatalf("upgraded VoterRegistrationInfo topic missing after merge")
	}
	for _, eventName := range []string{"SignUptimeVoteEnabled", "UptimeVoteSigned"} {
		if !containsLog(cfg.CollectLogs, "FlareSystemsManager", getTopic(system.FlareSystemsManagerMetaData, eventName)) {
			t.Fatalf("%s log collector missing after merge", eventName)
		}
	}
}

// The FCC contracts are per-network round logs, so they follow the chain ID and
// networks without a deployment must not inherit another network's addresses.
func TestApplyFspCollectors_NetworkRoundLogs(t *testing.T) {
	songbirdTee := "0x5C2dE0DeFC3FDBbF8e12c12bD0b1629Ed37DC767"

	cfg := IndexerConfig{Mode: IndexerModeFsp}
	ApplyFspCollectors(&cfg, chain.ChainIDSongbird)
	if !containsLogAddress(cfg.CollectLogs, songbirdTee, teeInstructionsSentTopic) {
		t.Fatalf("songbird FlareTeeManager filter missing after merge")
	}

	cfg = IndexerConfig{Mode: IndexerModeFsp}
	ApplyFspCollectors(&cfg, chain.ChainIDFlare)
	if containsLogAddress(cfg.CollectLogs, songbirdTee, teeInstructionsSentTopic) {
		t.Fatalf("flare must not inherit songbird's FCC address")
	}

	// A hand-pinned entry must not be duplicated by the built-in.
	cfg = IndexerConfig{
		Mode:        IndexerModeFsp,
		CollectLogs: []LogInfo{{ContractAddress: songbirdTee, Topic: teeInstructionsSentTopic}},
	}
	ApplyFspCollectors(&cfg, chain.ChainIDSongbird)
	if got := countLogAddress(cfg.CollectLogs, songbirdTee, teeInstructionsSentTopic); got != 1 {
		t.Fatalf("expected a single FlareTeeManager filter, got %d", got)
	}
}

func TestApplyFspCollectors_FullModeDoesNotInjectFspDefaults(t *testing.T) {
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

	ApplyFspCollectors(&cfg, chain.ChainIDSongbird)

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

func countLogAddress(logs []LogInfo, contractAddress, topic string) int {
	count := 0
	for _, log := range logs {
		if strings.EqualFold(log.ContractAddress, contractAddress) && strings.EqualFold(log.Topic, topic) {
			count++
		}
	}
	return count
}

func containsLogAddress(logs []LogInfo, contractAddress, topic string) bool {
	return countLogAddress(logs, contractAddress, topic) > 0
}
