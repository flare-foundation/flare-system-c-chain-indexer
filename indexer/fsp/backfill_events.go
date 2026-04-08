package fsp

import (
	"context"
	"flare-ftso-indexer/boff"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer/core"
	"flare-ftso-indexer/logger"
	"math/big"
	"time"

	avxTypes "github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/interfaces"
	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func backfillEventRangesLogs(
	ctx context.Context,
	ci *core.Engine,
	eventRanges []fspBlockRange,
	logAddresses []common.Address,
	logTopics []common.Hash,
) error {
	chunkRange := ci.Params().FspLogFilterRange
	if chunkRange == 0 {
		chunkRange = 1
	}

	for eventRangeIx, eventRange := range eventRanges {
		eventRangeStart := time.Now()
		eventRangeInserted := 0

		logger.Info(
			"FSP event indexing started: %d/%d from=%d to=%d",
			eventRangeIx+1,
			len(eventRanges),
			eventRange.from,
			eventRange.to,
		)

		for blockStart := eventRange.from; blockStart <= eventRange.to; blockStart += chunkRange {
			blockEnd := min(blockStart+chunkRange-1, eventRange.to)
			logs, err := fetchEventRangeLogsChunk(ctx, ci, blockStart, blockEnd, logAddresses, logTopics)
			if err != nil {
				return err
			}
			if len(logs) == 0 {
				continue
			}
			dbLogs, err := buildDBLogs(ctx, ci, logs)
			if err != nil {
				return err
			}
			if err := saveLogs(ci, dbLogs); err != nil {
				return err
			}
			eventRangeInserted += len(dbLogs)
		}

		logger.Info(
			"FSP event indexing completed: %d/%d from=%d to=%d inserted=%d duration_ms=%d",
			eventRangeIx+1,
			len(eventRanges),
			eventRange.from,
			eventRange.to,
			eventRangeInserted,
			time.Since(eventRangeStart).Milliseconds(),
		)
	}

	return nil
}

func fetchEventRangeLogsChunk(
	ctx context.Context,
	ci *core.Engine,
	fromBlock uint64,
	toBlock uint64,
	logAddresses []common.Address,
	logTopics []common.Hash,
) ([]avxTypes.Log, error) {
	query := interfaces.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(toBlock),
		Addresses: logAddresses,
	}
	if len(logTopics) > 0 {
		query.Topics = [][]common.Hash{logTopics}
	}

	logs, err := boff.RetryWithMaxElapsed(
		ctx,
		func() ([]avxTypes.Log, error) {
			ctx, cancelFunc := context.WithTimeout(ctx, config.Timeout)
			defer cancelFunc()

			return ci.Client().FilterLogs(ctx, query)
		},
		"fetchFspEventRangeLogsChunk",
	)
	if err != nil {
		return nil, err
	}

	return logs, nil
}

func buildDBLogs(
	ctx context.Context,
	ci *core.Engine,
	logs []avxTypes.Log,
) ([]*database.Log, error) {
	dbLogs := make([]*database.Log, 0, len(logs))
	blockTimestamps := make(map[uint64]uint64)

	for i := range logs {
		log := &logs[i]

		ts, ok := blockTimestamps[log.BlockNumber]
		if !ok {
			var err error
			ts, err = ci.FetchBlockTimestamp(ctx, log.BlockNumber)
			if err != nil {
				return nil, err
			}
			blockTimestamps[log.BlockNumber] = ts
		}

		dbLog := core.BuildDBLogFromRequestedLog(log, ts, true)
		dbLogs = append(dbLogs, dbLog)
	}

	return dbLogs, nil
}

func saveLogs(ci *core.Engine, logs []*database.Log) error {
	if len(logs) == 0 {
		return nil
	}

	return ci.DB().Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.Insert{Modifier: "IGNORE"}).
			CreateInBatches(logs, database.DBTransactionBatchesSize).
			Error
	})
}
