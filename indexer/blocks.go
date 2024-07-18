package indexer

import (
	"context"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/logger"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ava-labs/coreth/core/types"
	"github.com/cenkalti/backoff/v4"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
)

type blockBatch struct {
	blocks []*types.Block
	mu     sync.RWMutex
}

func newBlockBatch(batchSize uint64) *blockBatch {
	return &blockBatch{blocks: make([]*types.Block, batchSize)}
}

func (ci *BlockIndexer) fetchBlock(ctx context.Context, index *uint64) (block *types.Block, err error) {
	var indexBigInt *big.Int
	if index != nil {
		indexBigInt = new(big.Int).SetUint64(*index)
	}

	bOff := backoff.NewExponentialBackOff()
	bOff.MaxElapsedTime = config.BackoffMaxElapsedTime

	err = backoff.RetryNotify(
		func() error {
			ctx, cancelFunc := context.WithTimeout(ctx, config.Timeout)
			defer cancelFunc()

			block, err = ci.client.BlockByNumber(ctx, indexBigInt)
			return err
		},
		bOff,
		func(err error, d time.Duration) {
			logger.Debug("BlockByNumber error: %s. Will retry after %s", err, d)
		},
	)

	if err != nil {
		return nil, errors.Wrap(err, "ci.client.BlockByNumber")
	}

	return block, nil
}

func (ci *BlockIndexer) fetchLastBlockIndex(ctx context.Context) (uint64, uint64, error) {
	lastBlock, err := ci.fetchBlock(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("fetchLastBlockIndex: %w", err)
	}

	return lastBlock.NumberU64(), lastBlock.Time(), nil
}

func (ci *BlockIndexer) fetchBlockTimestamp(ctx context.Context, index uint64) (uint64, error) {
	lastBlock, err := ci.fetchBlock(ctx, &index)
	if err != nil {
		return 0, fmt.Errorf("fetchBlockTimestamp: %w", err)
	}

	return lastBlock.Time(), nil
}

func (ci *BlockIndexer) requestBlocks(
	ctx context.Context, start, stop uint64,
) ([]*types.Block, error) {
	blocks := make([]*types.Block, stop-start)

	for i := start; i < stop; i++ {
		block, err := ci.fetchBlock(ctx, &i)
		if err != nil {
			return nil, errors.Wrap(err, "ci.fetchBlock")
		}

		blocks[i-start] = block
	}

	return blocks, nil
}

func (ci *BlockIndexer) processBlocks(
	bBatch *blockBatch, txBatch *transactionsBatch,
) {
	for i := range bBatch.blocks {
		ci.processBlockBatch(bBatch, txBatch, uint64(i))
	}
}

func (ci *BlockIndexer) processBlockBatch(
	bBatch *blockBatch, txBatch *transactionsBatch, i uint64,
) {
	bBatch.mu.RLock()
	defer bBatch.mu.RUnlock()

	block := bBatch.blocks[i]

	for txIndex, tx := range block.Transactions() {
		if tx.To() == nil {
			continue
		}

		txData := tx.Data()
		if len(txData) < 4 {
			continue
		}

		var funcSig functionSignature
		copy(funcSig[:], txData[:4])

		contractAddress := tx.To()
		check := false
		policy := transactionsPolicy{status: false, collectEvents: false}

		for _, address := range []common.Address{*contractAddress, undefinedAddress} {
			if val, ok := ci.transactions[address]; ok {
				for _, sig := range []functionSignature{funcSig, undefinedFuncSig} {
					if pol, ok := val[sig]; ok {
						check = true
						policy.status = policy.status || pol.status
						policy.collectEvents = policy.collectEvents || pol.collectEvents
					}
				}
			}
		}

		if check {
			txBatch.Add(tx, block, uint64(txIndex), nil, policy)
		}
	}
}

func (ci *BlockIndexer) convertBlocksToDB(bBatch *blockBatch) []*database.Block {
	blocks := make([]*database.Block, len(bBatch.blocks))

	for i := range blocks {
		b := bBatch.blocks[i]
		blocks[i] = &database.Block{
			Hash:      b.Hash().Hex()[2:],
			Number:    b.Number().Uint64(),
			Timestamp: b.Time(),
		}
	}

	return blocks
}
