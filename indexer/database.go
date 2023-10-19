package indexer

import (
	"context"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"reflect"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
)

type DatabaseStructData struct {
	Transactions  []*database.FtsoTransaction
	Commits       []*database.Commit
	Reveals       []*database.Reveal
	Signatures    []*database.SignatureData
	Finalizations []*database.Finalization
	RewardOffers  []*database.RewardOffer
}

func NewDatabaseStructData() *DatabaseStructData {
	transactionBatch := DatabaseStructData{}
	transactionBatch.Transactions = make([]*database.FtsoTransaction, 0)
	transactionBatch.Commits = make([]*database.Commit, 0)

	return &transactionBatch
}

func (ci *BlockIndexer) dbStates() (map[string]*database.States, error) {
	states := make(map[string]*database.States)
	for _, name := range database.StateNames {
		var state database.States
		err := ci.db.Where(&database.States{Name: name}).First(&state).Error
		if err != nil {
			return nil, err
		}
		states[name] = &state
	}

	return states, nil
}

func (ci *BlockIndexer) fetchLastBlockIndex() (int, error) {
	// todo: change to header by number when mocking is available
	var lastBlock *types.Block
	var err error
	for j := 0; j < config.ReqRepeats; j++ {
		ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(ci.params.TimeoutMillis)*time.Millisecond)
		lastBlock, err = ci.client.BlockByNumber(ctx, nil)
		cancelFunc()
		if err == nil {
			break
		}
	}
	if err != nil {
		return 0, err
	}

	return int(lastBlock.NumberU64()), nil
}

func (ci *BlockIndexer) saveData(data *DatabaseStructData, states map[string]*database.States, errChan chan error) {
	var err error

	databaseTx := ci.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			databaseTx.Rollback()
		}
	}()

	if len(data.Transactions) != 0 {
		err = databaseTx.Create(data.Transactions).Error
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

	for _, name := range database.StateNames {
		err = databaseTx.Save(states[name]).Error
		if err != nil {
			databaseTx.Rollback()
			errChan <- err
			return
		}
	}

	errChan <- databaseTx.Commit().Error
}
