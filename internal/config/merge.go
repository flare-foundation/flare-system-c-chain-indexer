package config

import "strings"

// mergeFspCollectors combines the default and user specified transaction and log configs
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
