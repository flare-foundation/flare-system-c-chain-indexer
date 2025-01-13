package indexer

import (
	"flare-ftso-indexer/database"

	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type databaseStructData struct {
	Blocks            []*database.Block
	Transactions      []*database.Transaction
	Logs              []*database.Log
	LogHashIndexCheck map[string]bool
}

func newDatabaseStructData() *databaseStructData {
	return &databaseStructData{
		LogHashIndexCheck: make(map[string]bool),
	}
}

func (ci *BlockIndexer) saveData(
	data *databaseStructData, states *database.DBStates, lastDBIndex, lastDBTimestamp uint64,
) error {
	return ci.db.Transaction(func(tx *gorm.DB) error {
		if len(data.Blocks) != 0 {
			err := tx.Clauses(clause.Insert{Modifier: "IGNORE"}).
				CreateInBatches(data.Blocks, database.DBTransactionBatchesSize).
				Error
			if err != nil {
				return errors.Wrap(err, "saveData: CreateInBatches0")
			}
		}

		if len(data.Transactions) != 0 {
			// insert transactions in the database, if an entry already exists, do nothing
			err := tx.Clauses(clause.Insert{Modifier: "IGNORE"}).
				CreateInBatches(data.Transactions, database.DBTransactionBatchesSize).
				Error
			if err != nil {
				return errors.Wrap(err, "saveData: CreateInBatches1")
			}
		}

		if len(data.Logs) != 0 {
			// insert logs in the database, if an entry already exists, do nothing
			err := tx.Clauses(clause.Insert{Modifier: "IGNORE"}).
				CreateInBatches(data.Logs, database.DBTransactionBatchesSize).
				Error
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
