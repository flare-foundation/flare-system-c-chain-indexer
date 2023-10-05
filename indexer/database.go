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

func (ci *BlockIndexer) state() (database.State, int, int, error) {
	currentState, err := database.FetchState(ci.db, ci.StateName)
	if err != nil {
		return database.State{}, 0, 0, err
	}

	startIndex := max(int(currentState.NextDBIndex), ci.params.StartIndex)
	// if the dataset is empty, set the first index
	if currentState.FirstDBIndex == currentState.NextDBIndex {
		currentState.FirstDBIndex = uint64(startIndex)
	}

	// todo: change to header by number when mocking is available
	var lastBlock *types.Block
	for j := 0; j < config.ReqRepeats; j++ {
		ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(ci.params.TimeoutMillis)*time.Millisecond)
		lastBlock, err = ci.client.BlockByNumber(ctx, nil)
		cancelFunc()
		if err == nil {
			break
		}
	}
	if err != nil {
		return database.State{}, 0, 0, err
	}
	lastIndex := int(lastBlock.NumberU64())
	currentState.UpdateLastIndex(lastIndex)
	err = database.UpdateState(ci.db, &currentState)
	if err != nil {
		return database.State{}, 0, 0, err
	}
	lastIndex = min(ci.params.StopIndex, lastIndex)

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
