package core

import (
	"context"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/contracts"

	"gorm.io/gorm"
)

func (ci *Engine) DB() *gorm.DB {
	return ci.db
}

func (ci *Engine) Client() *chain.Client {
	return ci.client
}

func (ci *Engine) Params() config.IndexerConfig {
	return ci.params
}

func (ci *Engine) ContractResolver() *contracts.ContractResolver {
	return ci.contractResolver
}

func (ci *Engine) FetchLastBlockIndex(ctx context.Context) (uint64, uint64, error) {
	return ci.fetchLastBlockIndex(ctx)
}

func (ci *Engine) FetchBlockTimestamp(ctx context.Context, block uint64) (uint64, error) {
	return ci.fetchBlockTimestamp(ctx, block)
}
