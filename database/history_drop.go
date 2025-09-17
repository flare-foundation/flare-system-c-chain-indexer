package database

import (
	"context"
	"flare-ftso-indexer/boff"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/logger"
	"math/big"
	"sort"
	"time"

	"github.com/pkg/errors"
	"gorm.io/gorm"
)

func DropHistory(
	ctx context.Context,
	db *gorm.DB,
	intervalSeconds, checkInterval uint64,
	client *chain.Client,
	startBlockNumber uint64,
) {
	for {
		time.Sleep(time.Duration(checkInterval) * time.Second)

		logger.Info("starting DropHistory iteration")

		startTime := time.Now()
		_, err := DropHistoryIteration(ctx, db, intervalSeconds, client, startBlockNumber)
		if err == nil {
			duration := time.Since(startTime)
			logger.Info("finished DropHistory iteration in %v", duration)
		} else {
			logger.Error("DropHistory error: %s", err)
		}
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
	ctx context.Context, db *gorm.DB, intervalSeconds uint64, client *chain.Client, startBlockNumber uint64,
) (uint64, error) {
	lastBlockTime, lastBlockNumber, err := getBlockTimestamp(ctx, nil, client)
	if err != nil {
		return 0, errors.Wrap(err, "Failed to get the latest time")
	}

	db = db.WithContext(ctx)

	deleteStartTime := lastBlockTime - intervalSeconds
	deleteStartBlock, err := getNearestBlockByTimestamp(
		ctx, deleteStartTime, db, client, startBlockNumber, lastBlockNumber,
	)
	if err != nil {
		return 0, errors.Wrap(err, "Failed to get the nearest block by timestamp")
	}

	// Delete in specified order to not break foreign keys.
	for _, entity := range deleteOrder {
		if err := deleteInBatches(db, deleteStartTime, entity); err != nil {
			return 0, err
		}
	}

	err = globalStates.Update(db, FirstDatabaseIndexState, deleteStartBlock, deleteStartTime)
	if err != nil {
		return 0, errors.Wrap(err, "Failed to update state in the DB")
	}

	return deleteStartBlock, nil
}

func deleteInBatches(db *gorm.DB, deleteStartTime uint64, entity interface{}) error {
	for {
		result := db.Limit(deleteBatchSize).Where("timestamp < ?", deleteStartTime).Delete(&entity)

		if result.Error != nil {
			return errors.Wrap(result.Error, "Failed to delete historic data in the DB")
		}

		if result.RowsAffected == 0 {
			return nil
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
		logger.Warn("failed to get the nearest block by timestamp from DB: %s", err)
	}

	// A blocknumber of 0 means that no block was found in the DB.
	if blockNumber != 0 {
		return blockNumber, nil
	}

	return getNearestBlockByTimestampFromChain(ctx, timestamp, client, startBlockNumber, lastBlockNumber)
}

const maxBlockTimeDiff = time.Minute

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

	blockTime := block.Timestamp
	if blockTime < timestamp {
		return 0, errors.Errorf(
			"unexpected block time %d for block %d, expected at least %d",
			blockTime, block.Number, timestamp,
		)
	}

	blockTimeDiff := time.Duration(blockTime-timestamp) * time.Second
	if blockTimeDiff > maxBlockTimeDiff {
		return 0, errors.Errorf(
			"block time %d is too far from the requested timestamp %d, diff: %v",
			blockTime, timestamp, blockTimeDiff,
		)
	}

	return block.Number, nil
}

func getNearestBlockByTimestampFromChain(
	ctx context.Context,
	searchTimestamp uint64,
	client *chain.Client,
	startBlockNumber uint64,
	endBlockNumber uint64,
) (uint64, error) {
	// We search over the entire range of blocks from the configured
	// startBlockNumber to the most recent block number.
	//
	// This could potentially be optimized further using some estimate
	// of the block time to reduce the search range, but for now this should
	// be good enough.
	//
	// Once the indexer is running for a while it should be possible to
	// read the required block number from the database instead of using
	// this search process.

	var err error
	i := sort.Search(int(endBlockNumber-startBlockNumber+1), func(i int) bool {
		// The err variable comes from the enclosing function. If it has been
		// set to a non-nil value by a previous iteration of the binary search,
		// we should not overwrite it. Ideally we would exit the binary search
		// early, but the sort.Search function does not provide a way to do
		// that. So instead, we just return false for all future iterations.
		// The results of the search are meaningless in this case.
		if err != nil {
			return false
		}

		blockNumber := startBlockNumber + uint64(i)

		var blockTime uint64
		blockTime, _, err = getBlockTimestamp(ctx, big.NewInt(int64(blockNumber)), client)
		if err != nil {
			return false
		}

		return blockTime >= searchTimestamp
	})
	if err != nil {
		return 0, errors.Wrap(err, "getNearestBlockByTimestampFromChain")
	}

	return startBlockNumber + uint64(i), nil
}
