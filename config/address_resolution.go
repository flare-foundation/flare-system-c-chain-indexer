package config

import (
	"context"
	"flare-ftso-indexer/contracts/contractregistry"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
)

var contractRegistryAddress = common.HexToAddress("0xaD67FE66660Fb8dFE9d6b1b4240d8650e30F6019") // Same on all networks

func ResolveContractAddresses(ctx context.Context, cfg *Config, rpcClient bind.ContractCaller) error {
	var contractRegistry *contractregistry.ContractRegistryCaller
	var err error

	cache := make(map[string]string)
	resolveAddressByName := func(contractName string) (string, error) {
		if contractRegistry == nil {
			contractRegistry, err = contractregistry.NewContractRegistryCaller(contractRegistryAddress, rpcClient)
			if err != nil {
				return "", errors.Wrap(err, "initialize contractRegistry")
			}
		}

		name := strings.TrimSpace(contractName)
		if name == "" {
			return "", errors.New("contract name is required")
		}

		if address, ok := cache[name]; ok {
			return address, nil
		}

		address, err := contractRegistry.GetContractAddressByName(&bind.CallOpts{Context: ctx}, name)
		if err != nil {
			return "", errors.Wrapf(err, "resolve contract address for %s", name)
		}
		if address == (common.Address{}) {
			return "", errors.Errorf("contract %q resolved to zero address", name)
		}

		addressHex := address.Hex()
		cache[name] = addressHex

		return addressHex, nil
	}

	for i := range cfg.Indexer.CollectTransactions {
		transaction := &cfg.Indexer.CollectTransactions[i]

		if strings.TrimSpace(transaction.ContractAddress) == "" {
			contractAddress, err := resolveAddressByName(transaction.ContractName)
			if err != nil {
				return err
			}
			transaction.ContractAddress = contractAddress
		}
	}

	for i := range cfg.Indexer.CollectLogs {
		log := &cfg.Indexer.CollectLogs[i]

		if strings.TrimSpace(log.ContractAddress) == "" {
			contractAddress, err := resolveAddressByName(log.ContractName)
			if err != nil {
				return err
			}
			log.ContractAddress = contractAddress
		}
	}

	return nil
}
