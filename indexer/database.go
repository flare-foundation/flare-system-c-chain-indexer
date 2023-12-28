package indexer

import (
	"flare-ftso-indexer/database"
	"fmt"
)

type DatabaseStructData struct {
	Transactions      []*database.Transaction
	Logs              []*database.Log
	LogHashIndexCheck map[string]bool
}

func NewDatabaseStructData() *DatabaseStructData {
	data := DatabaseStructData{}
	data.Transactions = make([]*database.Transaction, 0)
	data.Logs = make([]*database.Log, 0)
	data.LogHashIndexCheck = make(map[string]bool)

	return &data
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
		// insert transactions in the database, if an entry already exists, give error
		err = databaseTx.CreateInBatches(data.Transactions, database.DBTransactionBatchesSize).Error
		if err != nil {
			databaseTx.Rollback()
			errChan <- fmt.Errorf("saveData: CreateInBatches1: %w", err)
			return
		}
	}

	if len(data.Logs) != 0 {
		// insert logs in the database, if an entry already exists, give error
		err = databaseTx.CreateInBatches(data.Logs, database.DBTransactionBatchesSize).Error
		if err != nil {
			databaseTx.Rollback()
			errChan <- fmt.Errorf("saveData: CreateInBatches2: %w", err)
			return
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
