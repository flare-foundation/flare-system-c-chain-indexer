package contracts

import (
	"context"
	"flare-ftso-indexer/internal/contracts/contractregistry"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
)

var contractRegistryAddress = common.HexToAddress("0xaD67FE66660Fb8dFE9d6b1b4240d8650e30F6019") // Same on all networks.

type ContractResolver struct {
	registry *contractregistry.ContractRegistryCaller
	cache    map[string]common.Address
	mu       sync.RWMutex
}

func NewContractResolver(rpcClient bind.ContractCaller) (*ContractResolver, error) {
	registry, err := contractregistry.NewContractRegistryCaller(contractRegistryAddress, rpcClient)
	if err != nil {
		return nil, errors.Wrap(err, "initialize contract registry caller")
	}

	return &ContractResolver{
		registry: registry,
		cache:    make(map[string]common.Address),
	}, nil
}

func (r *ContractResolver) ResolveByName(ctx context.Context, contractName string) (common.Address, error) {
	name := strings.TrimSpace(contractName)
	if name == "" {
		return common.Address{}, errors.New("contract name is required")
	}

	r.mu.RLock()
	cached, ok := r.cache[name]
	r.mu.RUnlock()
	if ok {
		return cached, nil
	}

	address, err := r.registry.GetContractAddressByName(&bind.CallOpts{Context: ctx}, name)
	if err != nil {
		return common.Address{}, errors.Wrapf(err, "resolve contract address for %s", name)
	}
	if address == (common.Address{}) {
		return common.Address{}, errors.Errorf("contract %q resolved to zero address", name)
	}

	r.mu.Lock()
	r.cache[name] = address
	r.mu.Unlock()

	return address, nil
}
