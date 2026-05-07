package config

import (
	"context"
	"flare-ftso-indexer/internal/contracts"
	"strings"
)

func ResolveContractAddresses(ctx context.Context, cfg *Config, resolver *contracts.ContractResolver) error {
	for i := range cfg.Indexer.CollectTransactions {
		transaction := &cfg.Indexer.CollectTransactions[i]

		if strings.TrimSpace(transaction.ContractAddress) == "" {
			contractAddress, err := resolver.ResolveByName(ctx, transaction.ContractName)
			if err != nil {
				return err
			}
			transaction.ContractAddress = contractAddress.Hex()
		}
	}

	for i := range cfg.Indexer.CollectLogs {
		log := &cfg.Indexer.CollectLogs[i]

		if strings.TrimSpace(log.ContractAddress) == "" {
			contractAddress, err := resolver.ResolveByName(ctx, log.ContractName)
			if err != nil {
				return err
			}
			log.ContractAddress = contractAddress.Hex()
		}
	}

	return nil
}
