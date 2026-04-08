package fsp

import (
	"context"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer/core"
	"flare-ftso-indexer/logger"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	systemcontract "github.com/flare-foundation/go-flare-common/pkg/contracts/system"
	"github.com/pkg/errors"
	"gorm.io/gorm"
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

	logAddresses, logTopics, err := resolveFspContractAddresses(ctx, ci.ContractResolver())
	if err != nil {
		return 0, err
	}

	fsmCaller, err := systemcontract.NewFlareSystemsManagerCaller(fsmAddress, ci.Client())
	if err != nil {
		return 0, errors.Wrap(err, "bind FlareSystemsManager caller")
	}

	plan, err := computeStartupPlan(ctx, ci, fsmCaller, latestConfirmedNumber, latestConfirmedTimestamp)
	if err != nil {
		return 0, err
	}

	states, err := database.LoadDBStates(ctx, ci.DB())
	if err != nil {
		return 0, errors.Wrap(err, "database.LoadDBStates")
	}

	firstFspEventRangeBlock := states.States[database.FirstDatabaseIndexState]
	lastDBBlock := states.States[database.LastDatabaseIndexState]
	firstFullIndexBlock := states.States[database.FirstFullIndexState]
	catchupFromBlock, backfillEventRanges := resolveCatchupBlock(
		firstFspEventRangeBlock,
		lastDBBlock,
		firstFullIndexBlock,
		plan,
	)

	// Mark startup as incomplete until all startup backfill data is saved.
	if err := states.Update(ci.DB(), database.FirstDatabaseIndexState, 0, 0); err != nil {
		return 0, errors.Wrap(err, "reset first database state")
	}
	if err := states.Update(ci.DB(), database.LastDatabaseIndexState, 0, 0); err != nil {
		return 0, errors.Wrap(err, "reset last database state")
	}
	if err := states.Update(ci.DB(), database.LastChainIndexState, latestConfirmedNumber, latestConfirmedTimestamp); err != nil {
		return 0, errors.Wrap(err, "update last chain state")
	}

	logger.Info(
		"Startup backfill: backfillFspEventRanges=%t, index blocks: %d to %d, index FSP events for ranges: %+v",
		backfillEventRanges,
		catchupFromBlock,
		latestConfirmedNumber,
		plan.fspEventRanges,
	)

	if backfillEventRanges {
		if err := backfillEventRangesLogs(ctx, ci, plan.fspEventRanges, logAddresses, logTopics); err != nil {
			return 0, errors.Wrap(err, "backfill FSP event ranges")
		}
	} else {
		logger.Info("Skipping FSP event backfill, already indexed")
	}

	if err := ci.BackFillBlocks(
		ctx,
		states,
		catchupFromBlock,
		latestConfirmedNumber,
	); err != nil {
		return 0, errors.Wrap(err, "backfill FSP catchup range")
	}

	firstIndex := plan.keepFromBlock
	firstTimestamp := plan.keepFromTimestamp
	if err := ci.DB().Transaction(func(tx *gorm.DB) error {
		if err := states.Update(tx, database.FirstDatabaseIndexState, firstIndex, firstTimestamp); err != nil {
			return errors.Wrap(err, "update first database state")
		}
		if err := states.Update(tx, database.FirstFullIndexState, plan.fullIndexStartBlock, plan.fullIndexStartTimestamp); err != nil {
			return errors.Wrap(err, "update first full index state")
		}
		if err := states.Update(tx, database.LastDatabaseIndexState, latestConfirmedNumber, latestConfirmedTimestamp); err != nil {
			return errors.Wrap(err, "update last database state")
		}
		if err := states.Update(tx, database.LastChainIndexState, latestConfirmedNumber, latestConfirmedTimestamp); err != nil {
			return errors.Wrap(err, "update last chain state")
		}
		return nil
	}); err != nil {
		return 0, err
	}

	logger.Info(
		"FSP startup backfill complete: first=%d last=%d",
		firstIndex,
		latestConfirmedNumber,
	)

	return latestConfirmedNumber, nil
}

// resolveCatchupBlock decides where catchup should start on startup and whether FSP event ranges should be backfilled.
//
// It returns:
//   - catchupFromBlock: first block for full block catchup backfill
//   - backfillEventRanges: true when FSP event range backfill must run
//
// Full block catchup starts from fullIndexStartBlock when the dedicated full
// range start state and last DB state do not describe a valid/covering range.
//
// FSP event range backfill is decided independently from FirstDatabaseIndexState
// and runs when that coverage starts after keepFromBlock (or state is missing).
func resolveCatchupBlock(
	firstFspEventRangeState *database.State,
	lastDBState *database.State,
	firstFullIndexState *database.State,
	plan *fspStartupPlan,
) (uint64, bool) {
	catchupFromBlock := plan.fullIndexStartBlock
	if firstFullIndexState != nil &&
		lastDBState != nil &&
		firstFullIndexState.Index <= lastDBState.Index &&
		firstFullIndexState.Index <= plan.fullIndexStartBlock &&
		lastDBState.Index >= plan.fullIndexStartBlock {
		catchupFromBlock = lastDBState.Index + 1
	}

	backfillEventRanges := firstFspEventRangeState == nil ||
		firstFspEventRangeState.Index == 0 ||
		firstFspEventRangeState.Index > plan.keepFromBlock

	return catchupFromBlock, backfillEventRanges
}

// computeStartupPlan
func computeStartupPlan(
	ctx context.Context,
	ci *core.Engine,
	fsm *systemcontract.FlareSystemsManagerCaller,
	latestConfirmedNumber uint64,
	latestConfirmedTimestamp uint64,
) (*fspStartupPlan, error) {
	fullIndexStartBlock, startEpochID, err := getFullIndexStartBlock(
		ctx,
		ci,
		fsm,
		latestConfirmedNumber,
		latestConfirmedTimestamp,
	)
	if err != nil {
		return nil, err
	}

	fullIndexStartTimestamp, err := ci.FetchBlockTimestamp(ctx, fullIndexStartBlock)
	if err != nil {
		return nil, errors.Wrap(err, "fetch FSP catchup start timestamp")
	}

	eventRanges, err := fspRewardEpochEventRanges(ctx, fsm, startEpochID, latestConfirmedNumber)
	if err != nil {
		return nil, errors.Wrap(err, "compute FSP event ranges")
	}

	earliestKeepBlock := fullIndexStartBlock
	earliestKeepTimestamp := fullIndexStartTimestamp
	for _, eventRange := range eventRanges {
		if eventRange.from < earliestKeepBlock {
			ts, err := ci.FetchBlockTimestamp(ctx, eventRange.from)
			if err != nil {
				return nil, errors.Wrapf(err, "fetch FSP keep-start timestamp for block %d", eventRange.from)
			}

			earliestKeepBlock = eventRange.from
			earliestKeepTimestamp = ts
		}
	}

	return &fspStartupPlan{
		fullIndexStartBlock:     fullIndexStartBlock,
		fullIndexStartTimestamp: fullIndexStartTimestamp,
		keepFromBlock:           earliestKeepBlock,
		keepFromTimestamp:       earliestKeepTimestamp,
		fspEventRanges:          eventRanges,
	}, nil
}

func getFullIndexStartBlock(
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
		startBlock, err := findStartBlockByLookback(
			ctx,
			ci,
			latestConfirmedTimestamp,
			0,
			latestConfirmedNumber,
		)
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

	startBlock, err := findStartBlockByLookback(
		ctx,
		ci,
		epochStartTimestamp,
		1,
		epochStartBlock,
	)
	if err != nil {
		return 0, 0, errors.Wrapf(err, "find start block from epoch %d with lookback", startEpochID)
	}

	return startBlock, startEpochID, nil
}

func findStartBlockByLookback(
	ctx context.Context,
	ci *core.Engine,
	baseTimestamp uint64,
	startBlockNumber uint64,
	endBlockNumber uint64,
) (uint64, error) {
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
		startBlockNumber,
		endBlockNumber,
	)
}
