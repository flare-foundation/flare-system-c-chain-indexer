package fsp

import (
	"context"
	"flare-ftso-indexer/internal/chain"
	"flare-ftso-indexer/internal/core"
	"flare-ftso-indexer/internal/database"
	"flare-ftso-indexer/internal/logger"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	systemcontract "github.com/flare-foundation/go-flare-common/pkg/contracts/system"
	"github.com/pkg/errors"
)

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

	targets, err := buildStartupTargets(ctx, ci, fsmCaller, latestConfirmedNumber, latestConfirmedTimestamp)
	if err != nil {
		return 0, err
	}

	states, err := database.LoadDBStates(ctx, ci.DB())
	if err != nil {
		return 0, errors.Wrap(err, "database.LoadDBStates")
	}

	catchupFromBlock, backfillEventRanges := resolveCatchupBlock(
		states.States[database.FirstDatabaseFSPEventIndexState],
		states.States[database.FirstDatabaseIndexState],
		states.States[database.LastDatabaseIndexState],
		targets,
	)

	logger.Info(
		"FSP startup plan: catchup blocks from=%d, latest confirmed=%d, backfill FSP event ranges=%t, ranges=%+v",
		catchupFromBlock,
		latestConfirmedNumber,
		backfillEventRanges,
		targets.eventRanges,
	)

	if backfillEventRanges {
		logAddresses, logTopics, err := resolveFspContractAddresses(ctx, ci.ContractResolver())
		if err != nil {
			return 0, err
		}

		if err := backfillEventRangesLogs(ctx, ci, targets.eventRanges, logAddresses, logTopics); err != nil {
			return 0, errors.Wrap(err, "backfill FSP event ranges")
		}

		if err := states.Update(ci.DB(), database.FirstDatabaseFSPEventIndexState, targets.eventStartBlock, targets.eventStartTimestamp); err != nil {
			return 0, errors.Wrap(err, "set first FSP event index state")
		}
	} else {
		logger.Info("Skipping FSP event backfill, already indexed")
	}

	lastIndexed := latestConfirmedNumber
	if catchupFromBlock <= latestConfirmedNumber {
		lastIndexed, err = ci.IndexHistory(ctx, catchupFromBlock)
		if err != nil {
			return 0, errors.Wrap(err, "backfill FSP catchup range")
		}
	} else {
		logger.Info(
			"Skipping FSP catchup block backfill: start=%d is above latest confirmed=%d",
			catchupFromBlock,
			latestConfirmedNumber,
		)
	}

	logger.Info(
		"FSP startup backfill complete: targetFullStart=%d targetEventStart=%d lastIndexed=%d",
		targets.fullStartBlock,
		targets.eventStartBlock,
		lastIndexed,
	)

	return lastIndexed, nil
}

// resolveCatchupBlock decides where catchup should start on startup and whether FSP event ranges should be backfilled.
func resolveCatchupBlock(
	firstFspEvent *database.State,
	firstDbBlock *database.State,
	lastDbBlock *database.State,
	targets *fspStartupTargets,
) (uint64, bool) {
	// If existing fully-indexed data spans the target start, continue from
	// where we left off; otherwise (re)start from targets.fullStartBlock.
	catchupFromBlock := targets.fullStartBlock
	covers := database.IsSet(firstDbBlock) && database.IsSet(lastDbBlock) &&
		firstDbBlock.Index <= targets.fullStartBlock &&
		lastDbBlock.Index >= targets.fullStartBlock
	if covers {
		catchupFromBlock = lastDbBlock.Index + 1
	}

	eventsAlreadyIndexed := database.IsSet(firstFspEvent) && firstFspEvent.Index <= targets.eventStartBlock
	return catchupFromBlock, !eventsAlreadyIndexed
}

func buildStartupTargets(
	ctx context.Context,
	ci *core.Engine,
	fsm *systemcontract.FlareSystemsManagerCaller,
	latestConfirmedNumber uint64,
	latestConfirmedTimestamp uint64,
) (*fspStartupTargets, error) {
	fullStartBlock, startEpochID, err := resolveFullStartBlock(
		ctx,
		ci,
		fsm,
		latestConfirmedNumber,
		latestConfirmedTimestamp,
	)
	if err != nil {
		return nil, err
	}

	fullStartTimestamp, err := ci.FetchBlockTimestamp(ctx, fullStartBlock)
	if err != nil {
		return nil, errors.Wrap(err, "fetch FSP catchup start timestamp")
	}

	eventRanges, err := fspRewardEpochEventRanges(ctx, fsm, startEpochID, latestConfirmedNumber)
	if err != nil {
		return nil, errors.Wrap(err, "compute FSP event ranges")
	}

	eventStartBlock := fullStartBlock
	eventStartTimestamp := fullStartTimestamp
	for _, eventRange := range eventRanges {
		if eventRange.from < eventStartBlock {
			ts, err := ci.FetchBlockTimestamp(ctx, eventRange.from)
			if err != nil {
				return nil, errors.Wrapf(err, "fetch FSP keep-start timestamp for block %d", eventRange.from)
			}

			eventStartBlock = eventRange.from
			eventStartTimestamp = ts
		}
	}

	return &fspStartupTargets{
		fullStartBlock:      fullStartBlock,
		fullStartTimestamp:  fullStartTimestamp,
		eventStartBlock:     eventStartBlock,
		eventStartTimestamp: eventStartTimestamp,
		eventRanges:         eventRanges,
	}, nil
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
