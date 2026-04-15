package config

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/flare-foundation/go-flare-common/pkg/contracts/calculator"
	"github.com/flare-foundation/go-flare-common/pkg/contracts/fdchub"
	"github.com/flare-foundation/go-flare-common/pkg/contracts/fumanager"
	"github.com/flare-foundation/go-flare-common/pkg/contracts/offers"
	"github.com/flare-foundation/go-flare-common/pkg/contracts/registry"
	"github.com/flare-foundation/go-flare-common/pkg/contracts/relay"
	"github.com/flare-foundation/go-flare-common/pkg/contracts/system"
)

var fspCollectTransactions = []TransactionInfo{
	// FSP submission transactions
	{
		ContractName: "Submission",
		FuncSig:      "6c532fae",
	},
	{
		ContractName: "Submission",
		FuncSig:      "9d00c9fd",
	},
	{
		ContractName: "Submission",
		FuncSig:      "57eed580",
	},
	// FSP finalization transactions, only needed for reward calculation
	{
		ContractName:  "Relay",
		FuncSig:       "b59589d1",
		Status:        true,
		CollectEvents: true,
	},
}

// Events emitted during the Singing Policy protocol window, can use selective indexing
var rewardEpochLogs = []LogInfo{
	{ContractName: "FlareSystemsManager", Topic: getTopic(system.FlareSystemsManagerMetaData, "VotePowerBlockSelected")},
	{ContractName: "FlareSystemsManager", Topic: getTopic(system.FlareSystemsManagerMetaData, "RandomAcquisitionStarted")},
	{ContractName: "FlareSystemsManager", Topic: getTopic(system.FlareSystemsManagerMetaData, "RewardEpochStarted")},
	{ContractName: "VoterRegistry", Topic: getTopic(registry.RegistryMetaData, "VoterRegistered")},
	{ContractName: "FlareSystemsCalculator", Topic: getTopic(calculator.CalculatorMetaData, "VoterRegistrationInfo")},
	{ContractName: "Relay", Topic: getTopic(relay.RelayMetaData, "SigningPolicyInitialized")},
	{ContractName: "FtsoRewardOffersManager", Topic: getTopic(offers.OffersMetaData, "InflationRewardsOffered")},
	{ContractName: "FtsoRewardOffersManager", Topic: getTopic(offers.OffersMetaData, "RewardsOffered")},
	{ContractName: "FastUpdateIncentiveManager", Topic: getTopic(fumanager.FUManagerMetaData, "InflationRewardsOffered")},
	{ContractName: "FdcHub", Topic: getTopic(fdchub.FdcHubMetaData, "InflationRewardsOffered")},
	// Updated contracts with new event signatures:
	{ContractName: "VoterRegistry", Topic: "0xbfb6cd90b6e2668916d9e034926c84f40bcf94094b0d625ec8eecfdeb2150ae1"},          // VoterRegistryNext.VoterRegistered
	{ContractName: "FlareSystemsCalculator", Topic: "0xc49a5cabcc0776ace8cfd024e155bc303ee5e492b29d59f1ff7dbafa0b34a04b"}, // FlareSystemsCalculatorNext.VoterRegistrationInfo
}

// Events emitted anytime during voting rounds, requires full indexing
var roundLogs = []LogInfo{
	{ContractName: "FastUpdater"},
	{ContractName: "FastUpdateIncentiveManager"},
	{ContractName: "FdcHub"},
}

func FspCollectTransactions() []TransactionInfo {
	result := make([]TransactionInfo, len(fspCollectTransactions))
	copy(result, fspCollectTransactions)
	return result
}

func FspRewardEpochLogs() []LogInfo {
	result := make([]LogInfo, len(rewardEpochLogs))
	copy(result, rewardEpochLogs)
	return result
}

func FspRoundLogs() []LogInfo {
	result := make([]LogInfo, len(roundLogs))
	copy(result, roundLogs)
	return result
}

// FspCollectLogs combines Reward epoch metadata and round events for full indexing
func FspCollectLogs() []LogInfo {
	logs := FspRewardEpochLogs()
	logIx := make(map[string]int, len(logs))
	for i := range logs {
		logIx[logDedupKey(&logs[i])] = i
	}

	for _, roundLog := range FspRoundLogs() {
		key := logDedupKey(&roundLog)
		if _, ok := logIx[key]; ok {
			continue
		}

		logIx[key] = len(logs)
		logs = append(logs, roundLog)
	}

	return logs
}

func getTopic(meta *bind.MetaData, eventName string) string {
	parsedABI, err := meta.GetAbi()
	if err != nil {
		panic(err)
	}

	event, ok := parsedABI.Events[eventName]
	if !ok {
		panic("event not found in ABI: " + eventName)
	}

	return event.ID.Hex()
}
