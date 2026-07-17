package config

import (
	"context"
	"strings"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/contracts"
)

func ResolveContractAddresses(ctx context.Context, cfg *Config, resolver *contracts.ContractResolver) error {
	transactions := make([]TransactionInfo, 0, len(cfg.Indexer.CollectTransactions))
	for _, transaction := range cfg.Indexer.CollectTransactions {
		if strings.TrimSpace(transaction.ContractAddress) != "" {
			transactions = append(transactions, transaction)
			continue
		}

		contractAddresses, err := resolver.ResolveAllByName(ctx, transaction.ContractName)
		if err != nil {
			return err
		}
		for _, contractAddress := range contractAddresses {
			resolved := transaction
			resolved.ContractAddress = contractAddress.Hex()
			transactions = append(transactions, resolved)
		}
	}
	cfg.Indexer.CollectTransactions = transactions

	logs := make([]LogInfo, 0, len(cfg.Indexer.CollectLogs))
	for _, logInfo := range cfg.Indexer.CollectLogs {
		if strings.TrimSpace(logInfo.ContractAddress) != "" {
			logs = append(logs, logInfo)
			continue
		}

		contractAddresses, err := resolver.ResolveAllByName(ctx, logInfo.ContractName)
		if err != nil {
			return err
		}
		for _, contractAddress := range contractAddresses {
			resolved := logInfo
			resolved.ContractAddress = contractAddress.Hex()
			logs = append(logs, resolved)
		}
	}
	cfg.Indexer.CollectLogs = logs

	return nil
}
