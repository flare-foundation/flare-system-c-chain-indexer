package database

import (
	"context"
	"math/big"
	"time"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/boff"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"

	"github.com/flare-foundation/go-flare-common/pkg/logger"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

const (
	deleteBatchesPauseAfter    = 10
	deleteBatchesPauseDuration = 100 * time.Millisecond
)

func DropHistory(
	ctx context.Context,
	db *gorm.DB,
	intervalSeconds, checkInterval uint64,
	client *chain.Client,
) {
	for {
		logger.Infof("Starting history drop iteration")

		startTime := time.Now()
		err := dropHistoryIteration(ctx, db, intervalSeconds, client)
		if err == nil {
			logger.Infof("Finished history drop iteration: duration_ms=%d", time.Since(startTime).Milliseconds())
		} else {
			logger.Errorf("History drop error: %s", err)
		}

		time.Sleep(time.Duration(checkInterval) * time.Second)
	}
}

var deleteOrder = []interface{}{
	Log{},
	Transaction{},
	Block{},
}

// Only delete up to 1000 items in a single DB transaction to avoid lock
// timeouts.
const deleteBatchSize = 1000

func dropHistoryIteration(
	ctx context.Context, db *gorm.DB, intervalSeconds uint64, client *chain.Client,
) error {
	lastBlockTime, _, err := getBlockTimestamp(ctx, nil, client)
	if err != nil {
		return errors.Wrap(err, "Failed to get the latest time")
	}

	db = db.WithContext(ctx)
	deleteStartTime := lastBlockTime - intervalSeconds

	// Delete in specified order to not break foreign keys.
	for _, entity := range deleteOrder {
		if err := DeleteInBatches(db, deleteStartTime, entity); err != nil {
			return err
		}
	}

	var firstBlock Block
	err = db.Order("number ASC").First(&firstBlock).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return errors.Wrap(err, "find first surviving block")
	}

	if err := updateStateIfLower(db, FirstDatabaseIndexState, firstBlock.Number, firstBlock.Timestamp); err != nil {
		return errors.Wrap(err, "Failed to update state in the DB")
	}
	if err := updateStateIfLower(db, FirstDatabaseFSPEventIndexState, firstBlock.Number, firstBlock.Timestamp); err != nil {
		return errors.Wrap(err, "Failed to update FSP event state in the DB")
	}

	return nil
}

func updateStateIfLower(db *gorm.DB, stateName string, index, timestamp uint64) error {
	globalStates.mu.RLock()
	state := globalStates.States[stateName]
	globalStates.mu.RUnlock()

	if state != nil && state.Index >= index {
		return nil
	}

	return globalStates.Update(db, stateName, index, timestamp)
}

// GetStartBlock returns the block number to start indexing from based on the history drop parameter.
func GetStartBlock(
	ctx context.Context, historyDropIntervalSeconds uint64, client *chain.Client, configuredStartBlock uint64,
) (uint64, error) {
	lastBlockTime, lastBlockNumber, err := getBlockTimestamp(ctx, nil, client)
	if err != nil {
		return 0, errors.Wrap(err, "Failed to get the latest block")
	}

	deleteStartTime := lastBlockTime - historyDropIntervalSeconds

	// This function is only ever called when starting with an empty DB state
	// so we can skip the DB check and jump straight to the chain search.
	return chain.GetNearestBlockByTimestampFromChain(
		ctx, deleteStartTime, client, configuredStartBlock, lastBlockNumber,
	)
}

func DeleteInBatches(db *gorm.DB, deleteStartTime uint64, entity interface{}) error {
	batchCount := 0

	for {
		result := db.Limit(deleteBatchSize).Where("timestamp < ?", deleteStartTime).Delete(&entity)

		if result.Error != nil {
			return errors.Wrap(result.Error, "Failed to delete historic data in the DB")
		}

		if result.RowsAffected == 0 {
			return nil
		}

		// Take a rest every so often to avoid locking up the database too much
		batchCount++
		if batchCount%deleteBatchesPauseAfter == 0 {
			logger.Debugf("History drop progress: entity=%T, deleted=%d", entity, batchCount*deleteBatchSize)
			time.Sleep(deleteBatchesPauseDuration)
		}
	}
}

func getBlockTimestamp(ctx context.Context, index *big.Int, client *chain.Client) (uint64, uint64, error) {
	block, err := boff.RetryWithMaxElapsed(
		ctx,
		func() (*chain.Block, error) {
			ctx, cancelFunc := context.WithTimeout(ctx, config.Timeout)
			defer cancelFunc()

			return client.BlockByNumber(ctx, index)
		},
		"getBlockTimestamp",
	)

	if err != nil {
		return 0, 0, errors.Wrap(err, "getBlockByTimestamp")
	}

	return block.Time(), block.Number().Uint64(), nil
}
