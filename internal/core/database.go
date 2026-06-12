package core

import (
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/database"

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

func (ci *Engine) saveData(
	data *databaseStructData, lastDBIndex, lastDBTimestamp uint64,
) error {
	err := ci.db.Transaction(func(tx *gorm.DB) error {
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

		return nil
	})
	if err != nil {
		return err
	}

	// Advance states only after the data transaction has committed, so they
	// understate rather than overstate coverage. INSERT IGNORE makes the data
	// writes idempotent, so a crash between commit and these calls just causes
	// the next batch to re-process and self-correct.
	if first := lowestBlock(data.Blocks); first != nil {
		err := database.CreateStateIfMissing(ci.db, database.FirstDatabaseIndexState, first.Number, first.Timestamp)
		if err != nil {
			return errors.Wrap(err, "saveData: CreateStateIfMissing")
		}
	}

	return database.UpdateState(ci.db, database.LastDatabaseIndexState, lastDBIndex, lastDBTimestamp)
}

func lowestBlock(blocks []*database.Block) *database.Block {
	var first *database.Block
	for _, b := range blocks {
		if first == nil || b.Number < first.Number {
			first = b
		}
	}
	return first
}
