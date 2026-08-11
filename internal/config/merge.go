package config

import (
	"strings"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"
)

// ApplyFspCollectors merges the built-in FSP collectors for chainID into cfg,
// and is a no-op in full mode. Kept out of config parsing because some
// built-ins are network-specific, and the chain ID is only known once the node
// is reachable. Must run before contract names are resolved to addresses.
func ApplyFspCollectors(cfg *IndexerConfig, chainID chain.ChainID) {
	if !cfg.IsFspMode() {
		return
	}

	cfg.CollectTransactions, cfg.CollectLogs = mergeFspCollectors(chainID, cfg.CollectTransactions, cfg.CollectLogs)
}

// mergeFspCollectors combines the default and user specified transaction and log configs
func mergeFspCollectors(
	chainID chain.ChainID,
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

	logs := FspCollectLogs(chainID)
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
	topic = strings.TrimPrefix(topic, "0x")
	// "undefined" and empty both mean "every topic" to the engine, so they must
	// key alike or the same filter is issued twice.
	if topic == undefined {
		topic = ""
	}
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
