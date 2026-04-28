package fsp

import (
	"context"
	"flare-ftso-indexer/internal/chain"
	"flare-ftso-indexer/internal/core"
	"flare-ftso-indexer/internal/database"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	systemcontract "github.com/flare-foundation/go-flare-common/pkg/contracts/system"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
	"github.com/pkg/errors"
)

const fspFsmContractName = "FlareSystemsManager"

func IndexStartup(ctx context.Context, ci *core.Engine) (uint64, error) {
	latestConfirmedNumber, latestConfirmedTimestamp, err := ci.FetchLastBlockIndex(ctx)
	if err != nil {
		return 0, errors.Wrap(err, "ci.FetchLastBlockIndex")
	}

	fsmAddress, err := ci.ContractResolver().ResolveByName(ctx, fspFsmContractName)
	if err != nil {
		return 0, err
	}
	fsmCaller, err := systemcontract.NewFlareSystemsManagerCaller(fsmAddress, ci.Client())
	if err != nil {
		return 0, errors.Wrap(err, "bind FlareSystemsManager caller")
	}

	fullStartBlock, startEpochID, err := resolveFullStartBlock(
		ctx, ci, fsmCaller, latestConfirmedNumber, latestConfirmedTimestamp,
	)
	if err != nil {
		return 0, err
	}

	eventRanges, err := fspRewardEpochEventRanges(ctx, fsmCaller, startEpochID, latestConfirmedNumber)
	if err != nil {
		return 0, errors.Wrap(err, "compute FSP event ranges")
	}

	// Trim ranges that overlap the catchup region — catchup full-indexes those
	// blocks and naturally captures the same events.
	eventRanges = trimEventRanges(eventRanges, fullStartBlock)

	states, err := database.LoadDBStates(ctx, ci.DB())
	if err != nil {
		return 0, errors.Wrap(err, "database.LoadDBStates")
	}

	// Catchup start: continue from where we left off if existing data covers
	// the target start; otherwise (re)start from fullStartBlock.
	catchupFromBlock := fullStartBlock
	firstDb := states.States[database.FirstDatabaseIndexState]
	lastDb := states.States[database.LastDatabaseIndexState]
	if database.IsSet(firstDb) && database.IsSet(lastDb) &&
		firstDb.Index <= fullStartBlock && lastDb.Index >= fullStartBlock {
		catchupFromBlock = lastDb.Index + 1
	}

	// FSP event backfill is needed iff we don't already have events at or below
	// the lowest event range start.
	eventStartBlock := lowestRangeFrom(eventRanges, fullStartBlock)
	firstFspEvent := states.States[database.FirstDatabaseFSPEventIndexState]
	backfillEventRanges := !(database.IsSet(firstFspEvent) && firstFspEvent.Index <= eventStartBlock)

	logger.Infof(
		"FSP startup plan: catchup blocks from=%d, latest confirmed=%d, backfill FSP event ranges=%t, ranges=%+v",
		catchupFromBlock,
		latestConfirmedNumber,
		backfillEventRanges,
		eventRanges,
	)

	if backfillEventRanges && len(eventRanges) > 0 {
		logAddresses, logTopics, err := resolveFspContractAddresses(ctx, ci.ContractResolver())
		if err != nil {
			return 0, err
		}
		if err := backfillEventRangesLogs(ctx, ci, eventRanges, logAddresses, logTopics); err != nil {
			return 0, errors.Wrap(err, "backfill FSP event ranges")
		}
		eventStartTimestamp, err := ci.FetchBlockTimestamp(ctx, eventStartBlock)
		if err != nil {
			return 0, errors.Wrapf(err, "fetch FSP event-start timestamp for block %d", eventStartBlock)
		}
		if err := states.Update(ci.DB(), database.FirstDatabaseFSPEventIndexState, eventStartBlock, eventStartTimestamp); err != nil {
			return 0, errors.Wrap(err, "set first FSP event index state")
		}
	} else {
		logger.Infof("Skipping FSP event backfill, already indexed")
	}

	lastIndexed := latestConfirmedNumber
	if catchupFromBlock <= latestConfirmedNumber {
		lastIndexed, err = ci.IndexHistory(ctx, catchupFromBlock)
		if err != nil {
			return 0, errors.Wrap(err, "backfill FSP catchup range")
		}
	} else {
		logger.Infof(
			"Skipping FSP catchup block backfill: start=%d is above latest confirmed=%d",
			catchupFromBlock,
			latestConfirmedNumber,
		)
	}

	logger.Infof(
		"FSP startup backfill complete: targetFullStart=%d targetEventStart=%d lastIndexed=%d",
		fullStartBlock,
		eventStartBlock,
		lastIndexed,
	)

	return lastIndexed, nil
}

// trimEventRanges drops any range whose `from` is at or above fullStartBlock,
// and clips the `to` of any range that straddles to fullStartBlock-1. The
// catchup phase will full-index everything from fullStartBlock onward, so
// running FilterLogs over the same blocks would just duplicate work.
func trimEventRanges(ranges []fspBlockRange, fullStartBlock uint64) []fspBlockRange {
	if fullStartBlock == 0 {
		return ranges
	}
	out := make([]fspBlockRange, 0, len(ranges))
	for _, r := range ranges {
		if r.from >= fullStartBlock {
			continue
		}
		if r.to >= fullStartBlock {
			r.to = fullStartBlock - 1
		}
		out = append(out, r)
	}
	return out
}

// lowestRangeFrom returns the lowest `from` across the given ranges, or
// fallback if there are none.
func lowestRangeFrom(ranges []fspBlockRange, fallback uint64) uint64 {
	lowest := fallback
	for _, r := range ranges {
		if r.from < lowest {
			lowest = r.from
		}
	}
	return lowest
}

func resolveFullStartBlock(
	ctx context.Context,
	ci *core.Engine,
	fsm *systemcontract.FlareSystemsManagerCaller,
	latestConfirmedNumber uint64,
	latestConfirmedTimestamp uint64,
) (uint64, uint64, error) {
	currentEpochID, err := fspCurrentEpochID(ctx, fsm)
	if err != nil {
		return 0, 0, err
	}

	params := ci.Params()
	if params.HistoryEpochs == 0 {
		startBlock, err := findStartBlockByLookback(ctx, ci, latestConfirmedTimestamp, latestConfirmedNumber)
		if err != nil {
			return 0, 0, errors.Wrap(err, "find FSP catchup start block by timestamp")
		}

		return startBlock, currentEpochID, nil
	}

	startEpochID := uint64(0)
	if params.HistoryEpochs-1 < currentEpochID {
		startEpochID = currentEpochID - (params.HistoryEpochs - 1)
	}

	info, err := fsm.GetRewardEpochStartInfo(&bind.CallOpts{Context: ctx}, new(big.Int).SetUint64(startEpochID))
	if err != nil {
		return 0, 0, errors.Wrapf(err, "getRewardEpochStartInfo(%d)", startEpochID)
	}

	epochStartBlock := info.RewardEpochStartBlock
	if epochStartBlock > latestConfirmedNumber {
		return latestConfirmedNumber, 0, nil
	}

	epochStartTimestamp, err := ci.FetchBlockTimestamp(ctx, epochStartBlock)
	if err != nil {
		return 0, 0, errors.Wrapf(err, "fetch start epoch timestamp for block %d", epochStartBlock)
	}

	startBlock, err := findStartBlockByLookback(ctx, ci, epochStartTimestamp, epochStartBlock)
	if err != nil {
		return 0, 0, errors.Wrapf(err, "find start block from epoch %d with lookback", startEpochID)
	}

	return startBlock, startEpochID, nil
}

func findStartBlockByLookback(ctx context.Context, ci *core.Engine, baseTimestamp uint64, endBlockNumber uint64) (uint64, error) {
	if endBlockNumber == 0 {
		return 0, nil
	}

	params := ci.Params()
	searchTimestamp := uint64(0)
	if baseTimestamp > params.FspTxLookbackSeconds {
		searchTimestamp = baseTimestamp - params.FspTxLookbackSeconds
	}

	return chain.GetNearestBlockByTimestampFromChain(
		ctx,
		searchTimestamp,
		ci.Client(),
		0,
		endBlockNumber,
	)
}
