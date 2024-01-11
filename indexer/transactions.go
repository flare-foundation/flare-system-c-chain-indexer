package indexer

import (
	"context"
	"encoding/hex"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
)

type TransactionsBatch struct {
	Transactions []*types.Transaction
	toBlock      []*types.Block
	toIndex      []uint64
	toReceipt    []*types.Receipt
	toPolicy     [][2]bool
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

func (ci *BlockIndexer) getTransactionsReceipt(
	transactionBatch *TransactionsBatch, start, stop int,
) error {
	var receipt *types.Receipt
	var err error
	for i := start; i < stop; i++ {
		tx := transactionBatch.Transactions[i]
		if transactionBatch.toPolicy[i][0] || transactionBatch.toPolicy[i][1] {
			for j := 0; j < config.ReqRepeats; j++ {
				ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(ci.params.TimeoutMillis)*time.Millisecond)
				receipt, err = ci.client.TransactionReceipt(ctx, tx.Hash())
				cancelFunc()
				if err == nil {
					break
				}
			}
			if err != nil {
				return errors.Wrap(err, "getTransactionsReceipt")
			}
		} else {
			receipt = nil
		}

		transactionBatch.toReceipt[i] = receipt
	}

	return nil
}

func (ci *BlockIndexer) processTransactions(transactionBatch *TransactionsBatch) (*DatabaseStructData, error) {
	data := NewDatabaseStructData()
	for i, tx := range transactionBatch.Transactions {
		block := transactionBatch.toBlock[i]
		txData := hex.EncodeToString(tx.Data())
		funcSig := txData[:8]
		fromAddress, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx) // todo: this is a bit slow
		if err != nil {
			return nil, fmt.Errorf("processTransactions: Sender: %w", err)
		}
		status := uint64(2)
		if transactionBatch.toReceipt[i] != nil {
			status = transactionBatch.toReceipt[i].Status
		}
		base := database.BaseEntity{ID: database.TransactionId}
		dbTx := &database.Transaction{
			BaseEntity:       base,
			Hash:             tx.Hash().Hex()[2:],
			FunctionSig:      funcSig,
			Input:            txData,
			BlockNumber:      block.NumberU64(),
			BlockHash:        block.Hash().Hex()[2:],
			TransactionIndex: transactionBatch.toIndex[i],
			FromAddress:      strings.ToLower(fromAddress.Hex()[2:]),
			ToAddress:        strings.ToLower(tx.To().Hex()[2:]),
			Status:           status,
			Value:            tx.Value().Text(16),
			GasPrice:         tx.GasPrice().String(),
			Gas:              tx.Gas(),
			Timestamp:        block.Time(),
		}
		data.Transactions = append(data.Transactions, dbTx)
		database.TransactionId += 1

		// if it was chosen to get the logs of the transaction we process it
		if transactionBatch.toReceipt[i] != nil && transactionBatch.toPolicy[i][1] {
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
				}
				data.Logs = append(data.Logs, dbLog)
				data.LogHashIndexCheck[dbLog.TransactionHash+strconv.Itoa(int(dbLog.LogIndex))] = true
			}
		}
	}

	return data, nil
}
