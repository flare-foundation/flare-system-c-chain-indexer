package indexer

import (
	"flare-ftso-indexer/database"

	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type DatabaseStructData struct {
	Transactions      []*database.Transaction
	Logs              []*database.Log
	LogHashIndexCheck map[string]bool
}

func NewDatabaseStructData() *DatabaseStructData {
	return &DatabaseStructData{
		LogHashIndexCheck: make(map[string]bool),
	}
}

func (ci *BlockIndexer) saveData(
	data *DatabaseStructData, states *database.DBStates, lastDBIndex, lastDBTimestamp int,
) error {
	return ci.db.Transaction(func(tx *gorm.DB) error {
		if len(data.Transactions) != 0 {
			// insert transactions in the database, if an entry already exists, give error
			err := tx.CreateInBatches(data.Transactions, database.DBTransactionBatchesSize).Error
			if err != nil {
				return errors.Wrap(err, "saveData: CreateInBatches1")
			}
		}

		if len(data.Logs) != 0 {
			// insert logs in the database, if an entry already exists, give error
			err := tx.CreateInBatches(data.Logs, database.DBTransactionBatchesSize).Error
			if err != nil {
				return errors.Wrap(err, "saveData: CreateInBatches2")
			}
		}

		err := states.Update(tx, database.LastDatabaseIndexState, lastDBIndex, lastDBTimestamp)
		if err != nil {
			return errors.Wrap(err, "saveData: Update")
		}

		return nil
	})
}
