package indexer

import (
	"encoding/hex"
	"flare-ftso-indexer/indexer/abi"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/core/types"
)

type BlockBatch struct {
	Blocks []*types.Block
	sync.Mutex
}

type TransactionsBatch struct {
	Transactions []*types.Transaction
	toBlock      []*types.Block
	sync.Mutex
}

func NewBlockBatch(batchSize int) *BlockBatch {
	blockBatch := BlockBatch{}
	blockBatch.Blocks = make([]*types.Block, batchSize)

	return &blockBatch
}

func NewTransactionsBatch() *TransactionsBatch {
	transactionBatch := TransactionsBatch{}
	transactionBatch.Transactions = make([]*types.Transaction, 0)
	transactionBatch.toBlock = make([]*types.Block, 0)

	return &transactionBatch
}

func (ci *BlockIndexer) requestBlocks(blockBatch *BlockBatch, start, stop, listIndex, lastIndex int, errChan chan error) {
	for i := start; i < stop; i++ {
		var block *types.Block
		var err error
		if i > lastIndex {
			block = &types.Block{}
		} else {
			for j := 0; j < 10; j++ {
				block, err = ci.client.BlockByNumber(ci.ctx, big.NewInt(int64(i)))
				if err == nil {
					if j > 0 {
						fmt.Println(j)
					}
					break
				}
			}
			if err != nil {
				errChan <- err
				return
			}
		}
		blockBatch.Lock()
		blockBatch.Blocks[listIndex+i-start] = block
		blockBatch.Unlock()
	}

	errChan <- nil
}

func (ci *BlockIndexer) processBlocks(blockBatch *BlockBatch, batchTransactions *TransactionsBatch, start, stop int, errChan chan error) {
	for i := start; i < stop; i++ {
		block := blockBatch.Blocks[i]
		for _, tx := range block.Transactions() {
			txData := hex.EncodeToString(tx.Data())
			if len(txData) < 8 {
				continue
			}
			// todo: check contract's address
			_, ok := abi.FtsoPrefixToFuncCall[txData[:8]]
			if !ok {
				continue
			}

			batchTransactions.Lock()
			batchTransactions.Transactions = append(batchTransactions.Transactions, tx)
			batchTransactions.toBlock = append(batchTransactions.toBlock, block)
			batchTransactions.Unlock()
		}
	}
	errChan <- nil
}
