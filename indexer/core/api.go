package core

import (
	"context"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/contracts"
	"flare-ftso-indexer/database"

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

func (ci *Engine) BackFillBlocks(
	ctx context.Context,
	states *database.DBStates,
	fromBlock uint64,
	toBlock uint64,
) error {
	return ci.backFillBlocks(ctx, states, fromBlock, toBlock)
}
