package indexer

import (
	"flare-ftso-indexer/database"
	"reflect"
)

type DatabaseStructData struct {
	Transactions  []*database.FtsoTransaction
	Logs          []*database.FtsoLog
	Commits       []*database.Commit
	Reveals       []*database.Reveal
	Signatures    []*database.SignatureData
	Finalizations []*database.Finalization
	RewardOffers  []*database.RewardOffer
}

func NewDatabaseStructData() *DatabaseStructData {
	transactionBatch := DatabaseStructData{}
	transactionBatch.Transactions = make([]*database.FtsoTransaction, 0)
	transactionBatch.Logs = make([]*database.FtsoLog, 0)
	transactionBatch.Commits = make([]*database.Commit, 0)
	transactionBatch.Reveals = make([]*database.Reveal, 0)
	transactionBatch.Signatures = make([]*database.SignatureData, 0)
	transactionBatch.Finalizations = make([]*database.Finalization, 0)
	transactionBatch.RewardOffers = make([]*database.RewardOffer, 0)

	return &transactionBatch
}

func (ci *BlockIndexer) saveData(data *DatabaseStructData, states *database.DBStates,
	newIndex int, errChan chan error) {
	var err error

	databaseTx := ci.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			databaseTx.Rollback()
		}
	}()
	// todo: ignore tx if it is already in DB
	if len(data.Transactions) != 0 {
		err = databaseTx.Create(data.Transactions).Error
		if err != nil {
			databaseTx.Rollback()
			errChan <- err
			return
		}
	}

	if len(data.Logs) != 0 {
		err = databaseTx.Create(data.Logs).Error
		if err != nil {
			databaseTx.Rollback()
			errChan <- err
			return
		}
	}

	for _, slice := range []interface{}{data.Commits, data.Reveals,
		data.Signatures, data.Finalizations, data.RewardOffers} {
		if reflect.ValueOf(slice).Len() != 0 {
			// check if the option to save is chosen
			typeOf := reflect.ValueOf(slice).Index(0).Type().String()[1:]
			if _, ok := ci.optTables[database.InterfaceTypeToMethod[typeOf]]; ok {
				err = databaseTx.Create(slice).Error
				if err != nil {
					databaseTx.Rollback()
					errChan <- err
					return
				}
			}
		}
	}
	err = states.Update(ci.db, database.NextDatabaseIndexState, newIndex)
	if err != nil {
		databaseTx.Rollback()
		errChan <- err
		return
	}

	errChan <- databaseTx.Commit().Error
}
