package indexer

import (
	"context"
	"encoding/hex"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/logger"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/coreth/core/types"
	"github.com/cenkalti/backoff/v4"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
)

type transactionsBatch struct {
	transactions []*chain.Transaction
	toBlock      []*chain.Block
	toIndex      []uint64
	toReceipt    []*chain.Receipt
	toPolicy     []transactionsPolicy
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
	tb.toBlock = append(tb.toBlock, block)
	tb.toIndex = append(tb.toIndex, index)
	tb.toReceipt = append(tb.toReceipt, receipt)
	tb.toPolicy = append(tb.toPolicy, policy)
}

func countReceipts(txBatch *transactionsBatch) int {
	i := 0
	for _, e := range txBatch.toReceipt {
		if e != nil {
			i++
		}
	}

	return i
}

func (ci *BlockIndexer) getTransactionsReceipt(
	ctx context.Context, txBatch *transactionsBatch, start, stop int,
) error {
	bOff := backoff.NewExponentialBackOff()
	bOff.MaxElapsedTime = config.BackoffMaxElapsedTime

	for i := start; i < stop; i++ {
		txBatch.mu.RLock()
		tx := *txBatch.transactions[i]
		policy := txBatch.toPolicy[i]
		txBatch.mu.RUnlock()

		var receipt *chain.Receipt

		if policy.status || policy.collectEvents {
			err := backoff.RetryNotify(
				func() (err error) {
					ctx, cancelFunc := context.WithTimeout(ctx, config.Timeout)
					defer cancelFunc()

					receipt, err = ci.client.TransactionReceipt(ctx, tx.Hash())
					return err
				},
				bOff,
				func(err error, d time.Duration) {
					logger.Debug("TransactionReceipt error: %s. Will retry after %s", err, d)
				},
			)

			if err != nil {
				return errors.Wrap(err, "getTransactionsReceipt")
			}
		}

		txBatch.mu.Lock()
		txBatch.toReceipt[i] = receipt
		txBatch.mu.Unlock()
	}

	return nil
}

func (ci *BlockIndexer) processTransactions(txBatch *transactionsBatch, data *databaseStructData) error {
	txBatch.mu.RLock()
	defer txBatch.mu.RUnlock()

	for i := range txBatch.transactions {
		tx := txBatch.transactions[i]
		block := txBatch.toBlock[i]
		receipt := txBatch.toReceipt[i]
		txIndex := txBatch.toIndex[i]
		policy := txBatch.toPolicy[i]

		dbTx, err := buildDBTx(tx, receipt, block, txIndex, policy.collectSignature)
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
	tx *chain.Transaction, receipt *chain.Receipt, block *chain.Block, txIndex uint64, collectSignature bool,
) (*database.Transaction, error) {
	txData := hex.EncodeToString(tx.Data())
	funcSig := txData[:8]

	fromAddress, err := tx.FromAddress() // todo: this is a bit slow
	if err != nil {
		return nil, errors.Wrap(err, "types.Sender")
	}

	status := uint64(2)
	if receipt != nil {
		status = receipt.Status()
	}

	var signature *string
	if collectSignature {
		logger.Debug("Collecting signature for transaction %s", tx.Hash().Hex())

		sig, err := packSignature(tx)
		if err != nil {
			return nil, errors.Wrap(err, "packSignature")
		}

		signature = &sig
	} else {
		logger.Debug("Skipping signature collection for transaction %s", tx.Hash().Hex())
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
		Signature:        signature,
	}, nil
}

func packSignature(tx *chain.Transaction) (string, error) {
	v, r, s := tx.RawSignatureValues()
	if v == nil || r == nil || s == nil {
		return "", errors.New("transaction missing signature values")
	}

	sig := make([]byte, 65)
	copy(sig[0:32], common.LeftPadBytes(r.Bytes(), 32)) // pad to 32 bytes
	copy(sig[32:64], common.LeftPadBytes(s.Bytes(), 32))

	// Convert v to a byte. First check that it is a valid value.
	if v.Sign() < 0 {
		return "", errors.New("transaction v value is negative")
	}
	if v.BitLen() > 8 {
		return "", errors.New("transaction v value is too large")
	}

	sig[64] = byte(v.Uint64())
	return hex.EncodeToString(sig), nil
}

func buildDBLog(dbTx *database.Transaction, log *types.Log, block *chain.Block) (*database.Log, error) {
	if blockNum := block.Number(); blockNum.Cmp(new(big.Int).SetUint64(log.BlockNumber)) != 0 {
		return nil, errors.Errorf("block number mismatch %s != %d", blockNum, log.BlockNumber)
	}

	var topics [numTopics]string

	for j := 0; j < numTopics; j++ {
		if len(log.Topics) > j {
			topics[j] = log.Topics[j].Hex()[2:]
		} else {
			topics[j] = nullTopic
		}
	}

	return &database.Log{
		TransactionID:   dbTx.ID,
		Address:         log.Address.Hex()[2:],
		Data:            hex.EncodeToString(log.Data),
		Topic0:          topics[0],
		Topic1:          topics[1],
		Topic2:          topics[2],
		Topic3:          topics[3],
		TransactionHash: log.TxHash.Hex()[2:],
		LogIndex:        uint64(log.Index),
		Timestamp:       block.Time(),
		BlockNumber:     log.BlockNumber,
	}, nil
}
