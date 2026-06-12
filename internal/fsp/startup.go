package fsp

import (
	"context"
	"math/big"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/core"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/database"

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

	eventStartBlock, err := fspEventBackfillStartBlock(ctx, fsmCaller, startEpochID)
	if err != nil {
		return 0, errors.Wrap(err, "compute FSP event backfill start")
	}

	states, err := database.GetStates(
		ci.DB().WithContext(ctx),
		database.BlockFloor,
		database.LastIndexed,
		database.LogFloor,
	)
	if err != nil {
		return 0, errors.Wrap(err, "database.GetStates")
	}

	// Catchup start: continue from where we left off if existing data covers
	// the target start; otherwise (re)start from fullStartBlock.
	catchupFromBlock := fullStartBlock
	firstDb := states[database.BlockFloor]
	lastDb := states[database.LastIndexed]
	if database.IsSet(firstDb) && database.IsSet(lastDb) &&
		firstDb.Index <= fullStartBlock && lastDb.Index >= fullStartBlock {
		catchupFromBlock = lastDb.Index + 1
	}

	// FSP events from eventStartBlock up to the catchup start need a log-only
	// backfill; catchup full-indexes everything from fullStartBlock onward. Skip
	// it when that region is empty or already covered.
	firstFspEvent := states[database.LogFloor]
	backfillEvents := eventStartBlock < fullStartBlock &&
		(!database.IsSet(firstFspEvent) || firstFspEvent.Index > eventStartBlock)

	logger.Infof(
		"FSP startup plan: catchup_from=%d, latest_confirmed=%d, backfill_events=%t, event_start=%d",
		catchupFromBlock,
		latestConfirmedNumber,
		backfillEvents,
		eventStartBlock,
	)

	if backfillEvents {
		logAddresses, logTopics, err := resolveFspContractAddresses(ctx, ci.ContractResolver())
		if err != nil {
			return 0, err
		}
		if err := backfillFspEventLogs(ctx, ci, eventStartBlock, fullStartBlock-1, logAddresses, logTopics); err != nil {
			return 0, errors.Wrap(err, "backfill FSP events")
		}
		eventStartTimestamp, err := ci.FetchBlockTimestamp(ctx, eventStartBlock)
		if err != nil {
			return 0, errors.Wrapf(err, "fetch FSP event-start timestamp for block %d", eventStartBlock)
		}
		if err := database.UpdateState(ci.DB(), database.LogFloor, eventStartBlock, eventStartTimestamp); err != nil {
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
			"Skipping FSP catchup block backfill: start=%d, latest_confirmed=%d",
			catchupFromBlock,
			latestConfirmedNumber,
		)
	}

	logger.Infof(
		"FSP startup backfill complete: target_full_start=%d, target_event_start=%d, last_indexed=%d",
		fullStartBlock,
		eventStartBlock,
		lastIndexed,
	)

	return lastIndexed, nil
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
