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

	return dropHistoryBelow(ctx, db, lastBlockTime-intervalSeconds)
}

// dropHistoryBelow runs two symmetric passes sharing one boundary: logs first
// (they hold the FK on transactions), then transactions+blocks. Each pass
// deletes below the boundary and maintains its own coverage floor from its own
// table; in FSP mode logs are retained further back than blocks, so the floors
// diverge until the log-only region is consumed. Should the two boundaries
// ever be split, logs must be retained at least as long as blocks, or
// first_database_block loses its all-logs-present guarantee.
func dropHistoryBelow(ctx context.Context, db *gorm.DB, deleteStartTime uint64) error {
	db = db.WithContext(ctx)

	if err := dropAndRaiseFloor(db, deleteStartTime, FirstDatabaseFSPEventIndexState, firstSurvivingLog, Log{}); err != nil {
		return err
	}
	return dropAndRaiseFloor(db, deleteStartTime, FirstDatabaseIndexState, firstSurvivingBlock, Transaction{}, Block{})
}

// dropAndRaiseFloor deletes the given entities below the boundary, then raises
// the floor state to the first surviving row of the floor's own table — but
// only once the boundary has passed the floor. The floor is a coverage
// guarantee written at startup, not an observation: an iteration that cannot
// have deleted anything at or above it must leave it alone.
func dropAndRaiseFloor(
	db *gorm.DB,
	deleteStartTime uint64,
	floorState string,
	firstSurviving func(*gorm.DB) (uint64, uint64, error),
	entities ...interface{},
) error {
	for _, entity := range entities {
		if err := DeleteInBatches(db, deleteStartTime, entity); err != nil {
			return err
		}
	}

	floor, err := GetState(db, floorState)
	if err != nil {
		return errors.Wrapf(err, "read floor state %s", floorState)
	}
	if IsSet(floor) && deleteStartTime <= floor.BlockTimestamp {
		return nil
	}

	index, timestamp, err := firstSurviving(db)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return errors.Wrapf(err, "find first surviving row for %s", floorState)
	}

	return UpdateState(db, floorState, index, timestamp)
}

func firstSurvivingBlock(db *gorm.DB) (uint64, uint64, error) {
	var block Block
	if err := db.Order("number ASC").First(&block).Error; err != nil {
		return 0, 0, err
	}
	return block.Number, block.Timestamp, nil
}

func firstSurvivingLog(db *gorm.DB) (uint64, uint64, error) {
	var log Log
	if err := db.Order("block_number ASC").First(&log).Error; err != nil {
		return 0, 0, err
	}
	return log.BlockNumber, log.Timestamp, nil
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
