package indexer

import (
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/logger"
	"fmt"

	"gorm.io/gorm/clause"
)

type DatabaseStructData struct {
	Transactions     []*database.Transaction
	Logs             []*database.Log
	LogToTransaction map[int]int
}

func NewDatabaseStructData() *DatabaseStructData {
	transactionBatch := DatabaseStructData{}
	transactionBatch.Transactions = make([]*database.Transaction, 0)
	transactionBatch.Logs = make([]*database.Log, 0)
	transactionBatch.LogToTransaction = make(map[int]int)

	return &transactionBatch
}

func (ci *BlockIndexer) saveData(data *DatabaseStructData, states *database.DBStates,
	lastDBIndex, lastDBTimestamp int, errChan chan error) {
	var err error

	databaseTx := ci.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			databaseTx.Rollback()
		}
	}()
	if len(data.Transactions) != 0 {
		err = databaseTx.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(data.Transactions, database.DBTransactionBatchesSize).Error
		if err != nil {
			databaseTx.Rollback()
			errChan <- fmt.Errorf("saveData: CreateInBatches1: %w", err)
			return
		}
	}
	// transactions have now been given their unique id by the DB
	// (this does not work if some data was already in the DB, we catch this case later)
	for logIndex, transactionIndex := range data.LogToTransaction {
		data.Logs[logIndex].TransactionID = data.Transactions[transactionIndex].ID
	}

	if len(data.Logs) != 0 {
		err = databaseTx.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(data.Logs, database.DBTransactionBatchesSize).Error
		if err != nil {
			if err.Error()[:10] != "Error 1452" {
				databaseTx.Rollback()
				errChan <- fmt.Errorf("saveData: CreateInBatches2: %w", err)
				return
			} else {
				// the case where data was already in the DB, we need to obtain IDs of transactions
				logger.Info("Some transactions already in the DB, updating values")
				fetchTransactions := make([]database.Transaction, 0)
				hashes := make([]string, len(data.Transactions))
				hashToID := make(map[string]uint64)
				for i, e := range data.Transactions {
					hashes[i] = e.Hash
				}

				err = databaseTx.Where("hash IN ?", hashes).Find(&fetchTransactions).Error
				if err != nil {
					databaseTx.Rollback()
					errChan <- fmt.Errorf("saveData: Find: %w", err)
					return
				}
				if len(fetchTransactions) != len(data.Transactions) {
					databaseTx.Rollback()
					errChan <- fmt.Errorf("saveData: Wrong number of transactions")
					return
				}
				for _, tx := range fetchTransactions {
					hashToID[tx.Hash] = tx.ID
				}

				for logIndex, transactionIndex := range data.LogToTransaction {
					data.Logs[logIndex].TransactionID = hashToID[data.Transactions[transactionIndex].Hash]
				}
				err = databaseTx.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(data.Logs, database.DBTransactionBatchesSize).Error
				if err != nil {
					databaseTx.Rollback()
					errChan <- fmt.Errorf("saveData: CreateInBatches3: %w", err)
					return
				}
			}
		}
	}

	err = states.Update(ci.db, database.LastDatabaseIndexState, lastDBIndex, lastDBTimestamp)
	if err != nil {
		databaseTx.Rollback()
		errChan <- fmt.Errorf("saveData: Update: %w", err)
		return
	}
	err = databaseTx.Commit().Error
	if err != nil {
		errChan <- fmt.Errorf("saveData: Commit: %w", err)

	}
	errChan <- nil
}
