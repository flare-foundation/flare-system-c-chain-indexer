package indexer

import (
	"context"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/logger"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
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
			ctx, cancelFunc := context.WithTimeout(ctx, config.DefaultTimeout)
			defer cancelFunc()

			block, err = ci.client.BlockByNumber(ctx, indexBigInt)
			return err
		},
		bOff,
		func(err error, d time.Duration) {
			logger.Debug("BlockByNumber error: %s after %s", err, d)
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
	ctx context.Context, batch *blockBatch, start, stop, listIndex, lastIndex uint64,
) error {
	for i := start; i < stop; i++ {
		var block *types.Block

		if i > lastIndex {
			block = &types.Block{}
		} else {
			var err error

			block, err = ci.fetchBlock(ctx, &i)
			if err != nil {
				return errors.Wrap(err, "ci.fetchBlock")
			}
		}

		batch.mu.Lock()
		batch.blocks[listIndex+i-start] = block
		batch.mu.Unlock()
	}

	return nil
}

func (ci *BlockIndexer) processBlocks(
	bBatch *blockBatch, txBatch *transactionsBatch, start, stop uint64,
) {
	for i := start; i < stop; i++ {
		ci.processBlockBatch(bBatch, txBatch, i)
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
