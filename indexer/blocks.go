package indexer

import (
	"context"
	"flare-ftso-indexer/boff"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
)

type blockBatch struct {
	blocks []*chain.Block
	mu     sync.RWMutex
}

func newBlockBatch(batchSize uint64) *blockBatch {
	return &blockBatch{blocks: make([]*chain.Block, batchSize)}
}

func (ci *BlockIndexer) fetchBlock(ctx context.Context, index *uint64) (*chain.Block, error) {
	indexBigInt := indexToBigInt(index)

	return boff.RetryWithMaxElapsed(
		ctx,
		func() (*chain.Block, error) {
			ctx, cancelFunc := context.WithTimeout(ctx, config.Timeout)
			defer cancelFunc()

			return ci.client.BlockByNumber(ctx, indexBigInt)
		},
		"fetchBlock",
	)
}

func (ci *BlockIndexer) fetchBlockHeader(ctx context.Context, index *uint64) (*chain.Header, error) {
	indexBigInt := indexToBigInt(index)

	return boff.RetryWithMaxElapsed(
		ctx,
		func() (*chain.Header, error) {
			ctx, cancelFunc := context.WithTimeout(ctx, config.Timeout)
			defer cancelFunc()

			return ci.client.HeaderByNumber(ctx, indexBigInt)
		},
		"fetchBlockHeader",
	)
}

func indexToBigInt(index *uint64) *big.Int {
	if index == nil {
		return nil
	}

	return new(big.Int).SetUint64(*index)
}

func (ci *BlockIndexer) fetchLastBlockIndex(ctx context.Context) (uint64, uint64, error) {
	lastBlock, err := ci.fetchBlockHeader(ctx, nil)
	if err != nil {
		return 0, 0, errors.Wrap(err, "fetchBlockHeader last")
	}

	lastBlockNumber := lastBlock.Number().Uint64()
	if lastBlockNumber < ci.params.Confirmations {
		return 0, 0, fmt.Errorf("not enough confirmations for, latest block %d, confirmations required %d", lastBlockNumber, ci.params.Confirmations)
	}

	latestConfirmedNumber := lastBlockNumber - ci.params.Confirmations
	latestConfirmedHeader, err := ci.fetchBlockHeader(ctx, &latestConfirmedNumber)
	if err != nil {
		return 0, 0, errors.Wrap(err, "fetchBlockHeader latestConfirmed")
	}

	return latestConfirmedNumber, latestConfirmedHeader.Time(), nil
}

func (ci *BlockIndexer) fetchBlockTimestamp(ctx context.Context, index uint64) (uint64, error) {
	lastBlock, err := ci.fetchBlockHeader(ctx, &index)
	if err != nil {
		return 0, errors.Wrap(err, "fetchBlockHeader")
	}

	return lastBlock.Time(), nil
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
		var policy transactionsPolicy

		for _, address := range []common.Address{*contractAddress, undefinedAddress} {
			if val, ok := ci.transactions[address]; ok {
				for _, sig := range []functionSignature{funcSig, undefinedFuncSig} {
					if pol, ok := val[sig]; ok {
						check = true
						policy.status = policy.status || pol.status
						policy.collectEvents = policy.collectEvents || pol.collectEvents
						policy.collectSignature = policy.collectSignature || pol.collectSignature
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
