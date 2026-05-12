package core

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/boff"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/database"

	"github.com/ava-labs/coreth/core/types"
	"github.com/pkg/errors"
)

type transactionsBatch struct {
	transactions []*chain.Transaction
	blocks       []*chain.Block
	indices      []uint64
	receipts     []*chain.Receipt
	policies     []transactionsPolicy
	mu           sync.RWMutex
}

func (tb *transactionsBatch) Add(
	tx *chain.Transaction,
	block *chain.Block,
	index uint64,
	receipt *chain.Receipt,
	policy transactionsPolicy,
) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.transactions = append(tb.transactions, tx)
	tb.blocks = append(tb.blocks, block)
	tb.indices = append(tb.indices, index)
	tb.receipts = append(tb.receipts, receipt)
	tb.policies = append(tb.policies, policy)
}

func countReceipts(txBatch *transactionsBatch) int {
	i := 0
	for _, e := range txBatch.receipts {
		if e != nil {
			i++
		}
	}

	return i
}

func (ci *Engine) getTransactionsReceipt(
	ctx context.Context, txBatch *transactionsBatch, start, stop int,
) error {
	for i := start; i < stop; i++ {
		txBatch.mu.RLock()
		tx := *txBatch.transactions[i]
		policy := txBatch.policies[i]
		txBatch.mu.RUnlock()

		var receipt *chain.Receipt

		if policy.status || policy.collectEvents {
			var err error

			receipt, err = boff.RetryWithMaxElapsed(
				ctx,
				func() (*chain.Receipt, error) {
					ctx, cancelFunc := context.WithTimeout(ctx, config.Timeout)
					defer cancelFunc()

					return ci.client.TransactionReceipt(ctx, tx.Hash())
				},
				"getTransactionsReceipt",
			)

			if err != nil {
				return errors.Wrap(err, "getTransactionsReceipt")
			}
		}

		txBatch.mu.Lock()
		txBatch.receipts[i] = receipt
		txBatch.mu.Unlock()
	}

	return nil
}

func (ci *Engine) processTransactions(txBatch *transactionsBatch, data *databaseStructData) error {
	txBatch.mu.RLock()
	defer txBatch.mu.RUnlock()

	for i := range txBatch.transactions {
		tx := txBatch.transactions[i]
		block := txBatch.blocks[i]
		receipt := txBatch.receipts[i]
		txIndex := txBatch.indices[i]
		policy := txBatch.policies[i]

		dbTx, err := buildDBTx(tx, receipt, block, txIndex)
		if err != nil {
			return err
		}

		data.Transactions = append(data.Transactions, dbTx)
		database.TransactionId.Add(1)

		// if it was chosen to get the logs of the transaction we process it
		if receipt != nil && policy.collectEvents {
			for _, log := range receipt.Logs() {
				dbLog, err := buildDBLog(dbTx, log, block)
				if err != nil {
					return err
				}

				data.Logs = append(data.Logs, dbLog)

				key := fmt.Sprintf("%s%d", dbLog.TransactionHash, dbLog.LogIndex)
				data.LogHashIndexCheck[key] = true
			}
		}
	}

	return nil
}

func buildDBTx(
	tx *chain.Transaction, receipt *chain.Receipt, block *chain.Block, txIndex uint64,
) (*database.Transaction, error) {
	txData := hex.EncodeToString(tx.Data())
	// Guard against tx data shorter than a 4-byte selector (e.g. plain transfers)
	// reaching this code path with an "undefined" func_sig filter.
	funcSig := txData
	if len(funcSig) > 8 {
		funcSig = funcSig[:8]
	}

	fromAddress, err := tx.FromAddress() // todo: this is a bit slow
	if err != nil {
		return nil, errors.Wrap(err, "types.Sender")
	}

	status := uint64(2)
	if receipt != nil {
		status = receipt.Status()
	}

	base := database.BaseEntity{ID: database.TransactionId.Load()}
	return &database.Transaction{
		BaseEntity:       base,
		Hash:             tx.Hash().Hex()[2:],
		FunctionSig:      funcSig,
		Input:            txData,
		BlockNumber:      block.Number().Uint64(),
		BlockHash:        block.Hash().Hex()[2:],
		TransactionIndex: txIndex,
		FromAddress:      strings.ToLower(fromAddress.Hex()[2:]),
		ToAddress:        strings.ToLower(tx.To().Hex()[2:]),
		Status:           status,
		Value:            tx.Value().Text(16),
		GasPrice:         tx.GasPrice().String(),
		Gas:              tx.Gas(),
		Timestamp:        block.Time(),
	}, nil
}

func buildDBLog(dbTx *database.Transaction, log *types.Log, block *chain.Block) (*database.Log, error) {
	if blockNum := block.Number(); blockNum.Cmp(new(big.Int).SetUint64(log.BlockNumber)) != 0 {
		return nil, errors.Errorf("block number mismatch %s != %d", blockNum, log.BlockNumber)
	}

	dbLog := BuildDBLogFromRequestedLog(log, block.Time())
	dbLog.TransactionID = dbTx.ID
	return dbLog, nil
}
