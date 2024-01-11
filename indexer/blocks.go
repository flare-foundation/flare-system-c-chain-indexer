package indexer

import (
	"context"
	"encoding/hex"
	"flare-ftso-indexer/config"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
)

type BlockBatch struct {
	Blocks []*types.Block
	sync.Mutex
}

func NewBlockBatch(batchSize int) *BlockBatch {
	blockBatch := BlockBatch{}
	blockBatch.Blocks = make([]*types.Block, batchSize)

	return &blockBatch
}

func (ci *BlockIndexer) fetchBlock(ctx context.Context, index int) (*types.Block, error) {
	var block *types.Block
	indexBigInt := new(big.Int)
	if index >= 0 {
		indexBigInt.SetInt64(int64(index))
	} else {
		indexBigInt = nil
	}

	var err error
	for j := 0; j < config.ReqRepeats; j++ {
		ctx, cancelFunc := context.WithTimeout(ctx, time.Duration(ci.params.TimeoutMillis)*time.Millisecond)

		block, err = ci.client.BlockByNumber(ctx, indexBigInt)
		cancelFunc()
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, errors.Wrap(err, "ci.client.BlockByNumber")
	}

	return block, nil
}

func (ci *BlockIndexer) fetchLastBlockIndex(ctx context.Context) (int, int, error) {
	lastBlock, err := ci.fetchBlock(ctx, -1)
	if err != nil {
		return 0, 0, fmt.Errorf("fetchLastBlockIndex: %w", err)
	}

	return int(lastBlock.NumberU64()), int(lastBlock.Time()), nil
}

func (ci *BlockIndexer) fetchBlockTimestamp(ctx context.Context, index int) (int, error) {
	lastBlock, err := ci.fetchBlock(ctx, index)
	if err != nil {
		return 0, fmt.Errorf("fetchBlockTimestamp: %w", err)
	}

	return int(lastBlock.Time()), nil
}

func (ci *BlockIndexer) requestBlocks(
	ctx context.Context, blockBatch *BlockBatch, start, stop, listIndex, lastIndex int,
) error {
	for i := start; i < stop; i++ {
		var block *types.Block
		var err error
		if i > lastIndex {
			block = &types.Block{}
		} else {
			block, err = ci.fetchBlock(ctx, i)
			if err != nil {
				return errors.Wrap(err, "ci.fetchBlock")
			}
		}

		blockBatch.Lock()
		blockBatch.Blocks[listIndex+i-start] = block
		blockBatch.Unlock()
	}

	return nil
}

func (ci *BlockIndexer) processBlocks(
	blockBatch *BlockBatch, batchTransactions *TransactionsBatch, start, stop int,
) {
	for i := start; i < stop; i++ {
		block := blockBatch.Blocks[i]
		for txIndex, tx := range block.Transactions() {
			txData := hex.EncodeToString(tx.Data())
			if len(txData) < 8 {
				continue
			}
			funcSig := txData[:8]
			if tx.To() == nil {
				continue
			}
			contractAddress := strings.ToLower(tx.To().Hex()[2:])
			check := false
			policy := [2]bool{false, false}

			for _, address := range []string{contractAddress, "undefined"} {
				if val, ok := ci.transactions[address]; ok {
					for _, sig := range []string{funcSig, "undefined"} {
						if pol, ok := val[sig]; ok {
							check = true
							policy[0] = policy[0] || pol[0]
							policy[1] = policy[1] || pol[1]
						}
					}
				}
			}

			if check {
				batchTransactions.Lock()
				batchTransactions.Transactions = append(batchTransactions.Transactions, tx)
				batchTransactions.toBlock = append(batchTransactions.toBlock, block)
				batchTransactions.toReceipt = append(batchTransactions.toReceipt, nil)
				batchTransactions.toIndex = append(batchTransactions.toIndex, uint64(txIndex))
				batchTransactions.toPolicy = append(batchTransactions.toPolicy, policy)
				batchTransactions.Unlock()
			}
		}
	}
}
