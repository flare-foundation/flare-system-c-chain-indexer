package config

import (
	"slices"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"

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

// Reward-epoch metadata events (signing-policy protocol window plus epoch
// lifecycle), can use selective indexing
var rewardEpochLogs = []LogInfo{
	{ContractName: "FlareSystemsManager", Topic: getTopic(system.FlareSystemsManagerMetaData, "VotePowerBlockSelected")},
	{ContractName: "FlareSystemsManager", Topic: getTopic(system.FlareSystemsManagerMetaData, "RandomAcquisitionStarted")},
	{ContractName: "FlareSystemsManager", Topic: getTopic(system.FlareSystemsManagerMetaData, "RewardEpochStarted")},
	{ContractName: "FlareSystemsManager", Topic: getTopic(system.FlareSystemsManagerMetaData, "SignUptimeVoteEnabled")},
	{ContractName: "FlareSystemsManager", Topic: getTopic(system.FlareSystemsManagerMetaData, "UptimeVoteSigned")},
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

const (
	teeInstructionsSentTopic      = "0xf770e69a9fc05b7180797556ec4cedb6108ce2c56ffa76c84aa087efeb5e6963"
	fdc2AttestationRequestedTopic = "0x57c4413905bb1b444f93a5eab5a942fae34c0fcaa1c25cc595ce0b990310f5de"

	// undefined is the config sentinel for "match anything".
	undefined = "undefined"
)

// FCC fee events, read by reward calculation. Round logs like the FdcHub ones
// above, but addressed explicitly because these contracts are not in the
// ContractRegistry yet; once they are, move them to roundLogs as contract_name
// entries and drop this map. Flare has no deployment yet, hence no entry.
var networkRoundLogs = map[chain.ChainID][]LogInfo{
	chain.ChainIDSongbird: {
		{ContractAddress: "0x5C2dE0DeFC3FDBbF8e12c12bD0b1629Ed37DC767", Topic: teeInstructionsSentTopic},      // FlareTeeManager
		{ContractAddress: "0x4234a8f5D255d91d56df53d0cc78c0Cc2B67ACD8", Topic: fdc2AttestationRequestedTopic}, // Fdc2Hub
	},
	chain.ChainIDCoston: {
		{ContractAddress: "0xc4885998f5D792ed88C5Af7a3AaCBe333f017658", Topic: teeInstructionsSentTopic},      // FlareTeeManager
		{ContractAddress: "0x064C7B68B0e2BC87e7bE34e89741485Fcb48FA2F", Topic: fdc2AttestationRequestedTopic}, // Fdc2Hub
	},
	chain.ChainIDCoston2: {
		{ContractAddress: "0x1a9C4A0f9D76c0b1D91d22E24E573a9b377618aE", Topic: teeInstructionsSentTopic},      // FlareTeeManager
		{ContractAddress: "0x04dd3Ba33aC798d400bEc42A26F82f9812A421dc", Topic: fdc2AttestationRequestedTopic}, // Fdc2Hub
	},
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

// FspCollectLogs combines Reward epoch metadata and round events for full indexing
func FspCollectLogs(chainID chain.ChainID) []LogInfo {
	logs := FspRewardEpochLogs()
	logIx := make(map[string]int, len(logs))
	for i := range logs {
		logIx[logDedupKey(&logs[i])] = i
	}

	for _, roundLog := range slices.Concat(roundLogs, networkRoundLogs[chainID]) {
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
