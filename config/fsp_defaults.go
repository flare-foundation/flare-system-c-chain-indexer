package config

import (
	"strings"

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
	{
		ContractName:  "Relay",
		FuncSig:       "b59589d1",
		Status:        true,
		CollectEvents: true,
	},
}

var rewardEpochLogs = []LogInfo{
	{ContractName: "FlareSystemsManager", Topic: mustEventTopic(system.FlareSystemsManagerMetaData, "VotePowerBlockSelected")},
	{ContractName: "FlareSystemsManager", Topic: mustEventTopic(system.FlareSystemsManagerMetaData, "RandomAcquisitionStarted")},
	{ContractName: "FlareSystemsManager", Topic: mustEventTopic(system.FlareSystemsManagerMetaData, "RewardEpochStarted")},
	{ContractName: "FlareSystemsManager", Topic: "0x154b0214ae62d8a5548c1eac25fabd87c38b04932a217732e1022f3118da67f3"}, // FlareSystemsManager.RewardEpochStarted
	{ContractName: "VoterRegistry", Topic: mustEventTopic(registry.RegistryMetaData, "VoterRegistered")},
	{ContractName: "VoterRegistry", Topic: "0xbfb6cd90b6e2668916d9e034926c84f40bcf94094b0d625ec8eecfdeb2150ae1"}, // VoterRegistryNext.VoterRegistered
	{ContractName: "FlareSystemsCalculator", Topic: mustEventTopic(calculator.CalculatorMetaData, "VoterRegistrationInfo")},
	{ContractName: "FlareSystemsCalculator", Topic: "0xc49a5cabcc0776ace8cfd024e155bc303ee5e492b29d59f1ff7dbafa0b34a04b"}, // FlareSystemsCalculatorNext.VoterRegistrationInfo
	{ContractName: "Relay", Topic: mustEventTopic(relay.RelayMetaData, "SigningPolicyInitialized")},
	{ContractName: "FtsoRewardOffersManager", Topic: mustEventTopic(offers.OffersMetaData, "InflationRewardsOffered")},
	{ContractName: "FtsoRewardOffersManager", Topic: mustEventTopic(offers.OffersMetaData, "RewardsOffered")},
	{ContractName: "FastUpdateIncentiveManager", Topic: mustEventTopic(fumanager.FUManagerMetaData, "InflationRewardsOffered")},
	{ContractName: "FdcHub", Topic: mustEventTopic(fdchub.FdcHubMetaData, "InflationRewardsOffered")},
}

func mustEventTopic(meta *bind.MetaData, eventName string) string {
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

func mergeFspCollectors(
	userTxs []TransactionInfo,
	userLogs []LogInfo,
) ([]TransactionInfo, []LogInfo) {
	txs := FspCollectTransactions()
	txIx := make(map[string]int, len(txs))
	for i := range txs {
		txIx[txDedupKey(&txs[i])] = i
	}

	for i := range userTxs {
		user := userTxs[i]
		key := txDedupKey(&user)
		if idx, ok := txIx[key]; ok {
			txs[idx] = mergeTxInfo(txs[idx], user)
			continue
		}

		txIx[key] = len(txs)
		txs = append(txs, user)
	}

	logs := FspCollectLogs()
	logIx := make(map[string]int, len(logs))
	for i := range logs {
		logIx[logDedupKey(&logs[i])] = i
	}

	for i := range userLogs {
		user := userLogs[i]
		key := logDedupKey(&user)
		if idx, ok := logIx[key]; ok {
			logs[idx] = mergeLogInfo(logs[idx], user)
			continue
		}

		logIx[key] = len(logs)
		logs = append(logs, user)
	}

	return txs, logs
}

func txDedupKey(tx *TransactionInfo) string {
	funcSig := strings.ToLower(strings.TrimSpace(tx.FuncSig))
	funcSig = strings.TrimPrefix(funcSig, "0x")
	return contractDedupKey(tx.ContractAddress, tx.ContractName) + "|sig:" + funcSig
}

func logDedupKey(log *LogInfo) string {
	topic := strings.ToLower(strings.TrimSpace(log.Topic))
	return contractDedupKey(log.ContractAddress, log.ContractName) + "|topic:" + topic
}

func contractDedupKey(contractAddress string, contractName string) string {
	name := strings.ToLower(strings.TrimSpace(contractName))
	if name != "" {
		return "name:" + name
	}

	address := strings.ToLower(strings.TrimSpace(contractAddress))
	return "addr:" + address
}

func mergeTxInfo(base TransactionInfo, additional TransactionInfo) TransactionInfo {
	result := base

	if strings.TrimSpace(result.ContractAddress) == "" {
		result.ContractAddress = additional.ContractAddress
	}
	if strings.TrimSpace(result.ContractName) == "" {
		result.ContractName = additional.ContractName
	}
	if strings.TrimSpace(result.FuncSig) == "" {
		result.FuncSig = additional.FuncSig
	}

	result.Status = result.Status || additional.Status
	result.CollectEvents = result.CollectEvents || additional.CollectEvents
	result.Signature = result.Signature || additional.Signature

	return result
}

func mergeLogInfo(base LogInfo, additional LogInfo) LogInfo {
	result := base
	if strings.TrimSpace(result.ContractAddress) == "" {
		result.ContractAddress = additional.ContractAddress
	}
	if strings.TrimSpace(result.ContractName) == "" {
		result.ContractName = additional.ContractName
	}
	if strings.TrimSpace(result.Topic) == "" {
		result.Topic = additional.Topic
	}

	return result
}
