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
	"github.com/pkg/errors"
)

type TransactionsBatch struct {
	Transactions []*types.Transaction
	toBlock      []*types.Block
	toIndex      []uint64
	toReceipt    []*types.Receipt
	toPolicy     []transactionsPolicy
	sync.Mutex
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
	ctx context.Context, transactionBatch *TransactionsBatch, start, stop int,
) error {
	for i := start; i < stop; i++ {
		tx := transactionBatch.Transactions[i]
		var receipt *types.Receipt

		if transactionBatch.toPolicy[i].status || transactionBatch.toPolicy[i].collectEvents {
			var err error

			for j := 0; j < config.ReqRepeats; j++ {
				ctx, cancelFunc := context.WithTimeout(ctx, time.Duration(ci.params.TimeoutMillis)*time.Millisecond)
				receipt, err = ci.client.TransactionReceipt(ctx, tx.Hash())
				cancelFunc()
				if err == nil {
					break
				}
			}

			if err != nil {
				return errors.Wrap(err, "getTransactionsReceipt")
			}
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
		database.TransactionId += 1 // TODO should this be atomic/locked?

		// if it was chosen to get the logs of the transaction we process it
		if transactionBatch.toReceipt[i] != nil && transactionBatch.toPolicy[i].collectEvents {
			receipt := transactionBatch.toReceipt[i]

			for _, log := range receipt.Logs {
				var topics [numTopics]string

				for j := 0; j < numTopics; j++ {
					if len(log.Topics) > j {
						topics[j] = log.Topics[j].Hex()[2:]
					} else {
						topics[j] = nullTopic
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

				key := fmt.Sprintf("%s%d", dbLog.TransactionHash, dbLog.LogIndex)
				data.LogHashIndexCheck[key] = true
			}
		}
	}

	return data, nil
}
