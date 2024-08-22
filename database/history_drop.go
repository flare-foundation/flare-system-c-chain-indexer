package database

import (
	"context"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/logger"
	"math/big"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

func DropHistory(
	ctx context.Context, db *gorm.DB, intervalSeconds, checkInterval uint64, client *chain.Client,
) {
	for {
		logger.Info("starting DropHistory iteration")

		startTime := time.Now()
		_, err := DropHistoryIteration(ctx, db, intervalSeconds, client)
		if err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
			duration := time.Since(startTime)
			logger.Info("finished DropHistory iteration in %v", duration)
		} else {
			logger.Error("DropHistory error: %s", err)
		}

		time.Sleep(time.Duration(checkInterval) * time.Second)
	}
}

var deleteOrder []interface{} = []interface{}{
	Log{},
	Transaction{},
	Block{},
}

// Only delete up to 1000 items in a single DB transaction to avoid lock
// timeouts.
const deleteBatchSize = 1000

func DropHistoryIteration(
	ctx context.Context, db *gorm.DB, intervalSeconds uint64, client *chain.Client,
) (uint64, error) {
	lastBlockTime, _, err := getBlockTimestamp(ctx, nil, client)
	if err != nil {
		return 0, errors.Wrap(err, "Failed to get the latest time")
	}

	deleteStart := lastBlockTime - intervalSeconds

	db = db.WithContext(ctx)

	// Delete in specified order to not break foreign keys.
	for _, entity := range deleteOrder {
		if err := deleteInBatches(db, deleteStart, entity); err != nil {
			return 0, err
		}
	}

	var firstBlockNumber uint64
	err = db.Transaction(func(tx *gorm.DB) error {
		var firstBlock Block
		err = tx.Order("number").First(&firstBlock).Error
		if err != nil {
			return errors.Wrap(err, "Failed to get first block in the DB")
		}

		firstBlockNumber = firstBlock.Number

		err = globalStates.Update(tx, FirstDatabaseIndexState, firstBlockNumber, firstBlock.Timestamp)
		if err != nil {
			return errors.Wrap(err, "Failed to update state in the DB")
		}

		logger.Info("Deleted blocks up to index %d", firstBlock.Number)

		return nil
	})

	return firstBlockNumber, err
}

func deleteInBatches(db *gorm.DB, deleteStart uint64, entity interface{}) error {
	for {
		result := db.Limit(deleteBatchSize).Where("timestamp < ?", deleteStart).Delete(&entity)

		if result.Error != nil {
			return errors.Wrap(result.Error, "Failed to delete historic data in the DB")
		}

		if result.RowsAffected == 0 {
			return nil
		}
	}
}

func getBlockTimestamp(ctx context.Context, index *big.Int, client *chain.Client) (uint64, uint64, error) {
	bOff := backoff.NewExponentialBackOff()
	bOff.MaxElapsedTime = config.BackoffMaxElapsedTime

	var block *chain.Block
	err := backoff.RetryNotify(
		func() (err error) {
			ctx, cancelFunc := context.WithTimeout(ctx, config.Timeout)
			defer cancelFunc()

			block, err = client.BlockByNumber(ctx, index)
			return err
		},
		bOff,
		func(err error, d time.Duration) {
			logger.Debug("getBlockTimestamp error: %s - retrying after %v", err, d)
		},
	)

	if err != nil {
		return 0, 0, errors.Wrap(err, "getBlockByTimestamp")
	}

	return block.Time(), block.Number().Uint64(), nil
}
