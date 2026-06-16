package fsp

import (
	"context"
	"math/big"
	"time"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/boff"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/core"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/database"

	avxTypes "github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/interfaces"
	"github.com/ethereum/go-ethereum/common"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func backfillFspEventLogs(
	ctx context.Context,
	ci *core.Engine,
	fromBlock, toBlock uint64,
	logAddresses []common.Address,
	logTopics []common.Hash,
) error {
	chunkRange := ci.Params().LogRange
	if chunkRange == 0 {
		chunkRange = 1
	}

	start := time.Now()
	inserted := 0
	logger.Infof("FSP event indexing started: from=%d, to=%d", fromBlock, toBlock)

	for blockStart := fromBlock; blockStart <= toBlock; blockStart += chunkRange {
		blockEnd := min(blockStart+chunkRange-1, toBlock)
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
		inserted += len(dbLogs)
	}

	logger.Infof(
		"FSP event indexing completed: from=%d, to=%d, inserted=%d, duration_ms=%d",
		fromBlock, toBlock, inserted, time.Since(start).Milliseconds(),
	)
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
		// This assumes the FSP reward-epoch backfill filter set contains only
		// explicit address+topic pairs. If we ever need to mix "all topics for
		// address A" with "specific topics for address B", this merged query
		// would be incorrect and should be split into separate queries.
		query.Topics = [][]common.Hash{logTopics}
	}

	logs, err := boff.RetryWithMaxElapsed(
		ctx,
		func() ([]avxTypes.Log, error) {
			ctx, cancelFunc := context.WithTimeout(ctx, config.RPCTimeout)
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

		dbLog := core.BuildDBLogFromRequestedLog(log, ts)
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
