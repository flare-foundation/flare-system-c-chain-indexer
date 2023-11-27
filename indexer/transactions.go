package indexer

import (
	"context"
	"encoding/hex"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
)

type TransactionsBatch struct {
	Transactions []*types.Transaction
	toBlock      []*types.Block
	toIndex      []uint64
	toReceipt    []*types.Receipt
	sync.Mutex
}

func NewTransactionsBatch() *TransactionsBatch {
	transactionBatch := TransactionsBatch{}
	transactionBatch.Transactions = make([]*types.Transaction, 0)
	transactionBatch.toBlock = make([]*types.Block, 0)
	transactionBatch.toReceipt = make([]*types.Receipt, 0)

	return &transactionBatch
}

func countReceipts(txs *TransactionsBatch) int {
	i := 0
	for _, e := range txs.toReceipt {
		if e != nil {
			i++
		}
	}

	return i
}

func (ci *BlockIndexer) getTransactionsReceipt(transactionBatch *TransactionsBatch,
	start, stop int, errChan chan error) {
	var receipt *types.Receipt
	var err error
	for i := start; i < stop; i++ {
		tx := transactionBatch.Transactions[i]
		txData := hex.EncodeToString(tx.Data())
		funcSig := txData[:8]
		contractAddress := strings.ToLower(tx.To().Hex()[2:])
		if ci.transactions[contractAddress][funcSig][0] || ci.transactions[contractAddress][funcSig][1] {
			for j := 0; j < config.ReqRepeats; j++ {
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(ci.params.TimeoutMillis)*time.Millisecond)
				receipt, err = ci.client.TransactionReceipt(ctx, tx.Hash())
				cancelFunc()
				if err == nil {
					break
				}
			}
			if err != nil {
				errChan <- fmt.Errorf("getTransactionsReceipt: %w", err)
				return
			}
		} else {
			receipt = nil
		}

		transactionBatch.toReceipt[i] = receipt
	}

	errChan <- nil
}

func (ci *BlockIndexer) processTransactions(transactionBatch *TransactionsBatch) (*DatabaseStructData, error) {
	data := NewDatabaseStructData()
	logsIndex := 0
	for i, tx := range transactionBatch.Transactions {
		block := transactionBatch.toBlock[i]
		txData := hex.EncodeToString(tx.Data())
		funcSig := txData[:8]
		contractAddress := strings.ToLower(tx.To().Hex()[2:])
		fromAddress, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx) // todo: this is a bit slow
		if err != nil {
			return nil, fmt.Errorf("processTransactions: Sender: %w", err)
		}
		status := uint64(2)
		if transactionBatch.toReceipt[i] != nil {
			status = transactionBatch.toReceipt[i].Status
		}

		dbTx := &database.Transaction{
			Hash:             tx.Hash().Hex()[2:],
			FunctionSig:      funcSig,
			Input:            txData,
			BlockNumber:      block.NumberU64(),
			BlockHash:        block.Hash().Hex()[2:],
			TransactionIndex: transactionBatch.toIndex[i],
			FromAddress:      fromAddress.Hex()[2:],
			ToAddress:        tx.To().Hex()[2:],
			Status:           status,
			Value:            tx.Value().Text(16),
			GasPrice:         tx.GasPrice().String(),
			Gas:              tx.Gas(),
			Timestamp:        block.Time(),
		}
		data.Transactions = append(data.Transactions, dbTx)

		// if it was chosen to get the logs of the transaction we process it
		if transactionBatch.toReceipt[i] != nil && ci.transactions[contractAddress][funcSig][1] {
			receipt := transactionBatch.toReceipt[i]
			for _, log := range receipt.Logs {
				topics := make([]string, 4)
				for j := 0; j < 4; j++ {
					if len(log.Topics) > j {
						topics[j] = log.Topics[j].Hex()[2:]
					} else {
						topics[j] = "NULL"
					}
				}
				dbLog := &database.Log{
					Address:   log.Address.Hex()[2:],
					Data:      hex.EncodeToString(log.Data),
					Topic0:    topics[0],
					Topic1:    topics[1],
					Topic2:    topics[2],
					Topic3:    topics[3],
					LogIndex:  uint64(log.Index),
					Timestamp: block.Time(),
				}
				data.Logs = append(data.Logs, dbLog)
				data.LogToTransaction[logsIndex] = i
				logsIndex += 1
			}
		}
	}

	return data, nil
}
