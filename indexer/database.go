package indexer

import (
	"flare-ftso-indexer/database"
	"reflect"
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

func (ci *BlockIndexer) state() (database.State, int, int, error) {
	currentState, err := database.FetchState(ci.db, ci.StateName)
	if err != nil {
		return database.State{}, 0, 0, err
	}
	startIndex := max(int(currentState.NextDBIndex), ci.params.StartIndex)
	// todo: change to header by number when mocking is available
	lastBlock, err := ci.client.BlockByNumber(ci.ctx, nil)
	if err != nil {
		return database.State{}, 0, 0, err
	}
	lastIndex := int(lastBlock.NumberU64())
	currentState.UpdateLastIndex(lastIndex)
	err = database.UpdateState(ci.db, &currentState)
	if err != nil {
		return database.State{}, 0, 0, err
	}

	return currentState, startIndex, lastIndex, nil
}

func (ci *BlockIndexer) saveTransactions(data *DatabaseStructData, currentState database.State, errChan chan error) {
	var err error
	for _, slice := range []interface{}{data.Transactions, data.Commits, data.Reveals,
		data.Signatures, data.Finalizations, data.RewardOffers} {
		if reflect.ValueOf(slice).Len() != 0 {
			err = ci.db.Create(slice).Error
			if err != nil {
				errChan <- err
				return
			}
		}
	}

	err = database.UpdateState(ci.db, &currentState)
	if err != nil {
		errChan <- err
	}
	errChan <- nil
}
