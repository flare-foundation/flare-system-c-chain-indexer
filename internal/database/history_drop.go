package database

import (
	"context"
	"flare-ftso-indexer/internal/boff"
	"flare-ftso-indexer/internal/chain"
	"flare-ftso-indexer/internal/config"
	"math/big"
	"time"

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
	startBlockNumber uint64,
) {
	for {
		logger.Infof("starting DropHistory iteration")

		startTime := time.Now()
		err := dropHistoryIteration(ctx, db, intervalSeconds, client, startBlockNumber)
		if err == nil {
			duration := time.Since(startTime)
			logger.Infof("finished DropHistory iteration in %v", duration)
		} else {
			logger.Errorf("DropHistory error: %s", err)
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
	ctx context.Context, db *gorm.DB, intervalSeconds uint64, client *chain.Client, startBlockNumber uint64,
) error {
	lastBlockTime, lastBlockNumber, err := getBlockTimestamp(ctx, nil, client)
	if err != nil {
		return errors.Wrap(err, "Failed to get the latest time")
	}

	db = db.WithContext(ctx)

	deleteStartTime := lastBlockTime - intervalSeconds
	deleteStartBlock, err := getNearestBlockByTimestamp(
		ctx, deleteStartTime, db, client, startBlockNumber, lastBlockNumber,
	)
	if err != nil {
		return errors.Wrap(err, "Failed to get the nearest block by timestamp")
	}

	// Delete in specified order to not break foreign keys.
	for _, entity := range deleteOrder {
		if err := DeleteInBatches(db, deleteStartTime, entity); err != nil {
			return err
		}
	}

	if err := updateStateIfLower(db, FirstDatabaseIndexState, deleteStartBlock, deleteStartTime); err != nil {
		return errors.Wrap(err, "Failed to update state in the DB")
	}
	if err := updateStateIfLower(db, FirstDatabaseFSPEventIndexState, deleteStartBlock, deleteStartTime); err != nil {
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
			logger.Debugf("Deleted %d rows of %T so far", batchCount*deleteBatchSize, entity)
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

func getNearestBlockByTimestamp(
	ctx context.Context,
	timestamp uint64,
	db *gorm.DB,
	client *chain.Client,
	startBlockNumber uint64,
	lastBlockNumber uint64,
) (uint64, error) {
	// First try to find a block in the DB with a timestamp close to the requested one.
	// If that fails, we fall back to doing a binary search on the chain.
	blockNumber, err := getNearestBlockByTimestampFromDB(ctx, timestamp, db)
	if err != nil {
		logger.Debugf("failed to get the nearest block by timestamp from DB, will fall back to RPC binary search. err: %s", err)
	}

	// A blocknumber of 0 means that no block was found in the DB.
	if blockNumber != 0 {
		return blockNumber, nil
	}

	return chain.GetNearestBlockByTimestampFromChain(ctx, timestamp, client, startBlockNumber, lastBlockNumber)
}

const maxBlockTimeDiff = time.Minute

func validateNearestDBBlockTimestamp(blockTime, timestamp uint64) (bool, error) {
	if blockTime < timestamp {
		return false, errors.Errorf(
			"unexpected block time %d, expected at least %d",
			blockTime, timestamp,
		)
	}

	blockTimeDiff := time.Duration(blockTime-timestamp) * time.Second
	if blockTimeDiff > maxBlockTimeDiff {
		// This is expected on initial runs or whenever the DB only contains a
		// much newer window of blocks than the requested history-drop cutoff.
		// In that case we should quietly fall back to RPC binary search.
		return false, nil
	}

	return true, nil
}

func getNearestBlockByTimestampFromDB(ctx context.Context, timestamp uint64, db *gorm.DB) (uint64, error) {
	// First try to find a block in the DB with a similar timestamp.
	block, err := boff.RetryWithMaxElapsed(
		ctx,
		func() (*Block, error) {
			// First try to find a block in the DB with a similar timestamp.
			block := new(Block)
			err := db.Where("timestamp >= ?", timestamp).Order("timestamp ASC").First(block).Error

			if err == nil {
				return block, nil
			}

			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}

			return nil, err
		},
		"getNearestBlockByTimestampFromDB",
	)
	if err != nil {
		return 0, errors.Wrap(err, "getNearestBlockByTimestampFromDB")
	}

	// Block not found in the DB.
	if block == nil || block.Number == 0 {
		return 0, nil
	}

	useBlock, err := validateNearestDBBlockTimestamp(block.Timestamp, timestamp)
	if err != nil {
		return 0, errors.Wrapf(err, "invalid DB block %d", block.Number)
	}
	if !useBlock {
		return 0, nil
	}

	return block.Number, nil
}
