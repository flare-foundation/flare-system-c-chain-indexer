package indexer

import (
	"flare-ftso-indexer/database"

	"github.com/pkg/errors"
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

func (ci *BlockIndexer) saveData(
	data *DatabaseStructData, states *database.DBStates, lastDBIndex, lastDBTimestamp int,
) error {
	databaseTx := ci.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			databaseTx.Rollback()
		}
	}()
	if len(data.Transactions) != 0 {
		// insert transactions in the database, if an entry already exists, give error
		err := databaseTx.CreateInBatches(data.Transactions, database.DBTransactionBatchesSize).Error
		if err != nil {
			databaseTx.Rollback()
			return errors.Wrap(err, "saveData: CreateInBatches1")
		}
	}

	if len(data.Logs) != 0 {
		// insert logs in the database, if an entry already exists, give error
		err := databaseTx.CreateInBatches(data.Logs, database.DBTransactionBatchesSize).Error
		if err != nil {
			databaseTx.Rollback()
			return errors.Wrap(err, "saveData: CreateInBatches2")
		}
	}

	err := states.Update(ci.db, database.LastDatabaseIndexState, lastDBIndex, lastDBTimestamp)
	if err != nil {
		databaseTx.Rollback()
		return errors.Wrap(err, "saveData: Update")
	}

	err = databaseTx.Commit().Error
	if err != nil {
		return errors.Wrap(err, "saveData: Commit")
	}

	return nil
}
