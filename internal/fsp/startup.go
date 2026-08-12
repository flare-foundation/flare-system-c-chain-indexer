package fsp

import (
	"context"
	"math/big"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/boff"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/core"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/database"

	systemcontract "github.com/flare-foundation/go-flare-common/pkg/contracts/system"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
	"github.com/pkg/errors"
)

const fspFsmContractName = "FlareSystemsManager"

// fspTxLookbackSeconds is how far the full-block window reaches below its
// base (the confirmed tip for history_epochs=0, the oldest served epoch's
// start otherwise). It must be large enough that reward calculation for the
// oldest served epoch has its full submission data, which extends some way
// before the epoch's first voting round. Sized generously — well beyond that
// requirement — so it need not track the calculator's exact lookback, which
// lives in another repo and may change independently. Signing-policy events
// and reward offers do not depend on this window; they ride the selective
// event backfill anchored on recorded epoch data.
const fspTxLookbackSeconds = uint64(60 * 60)

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

	// eventStartBlock is zero when no epoch has recorded start data to anchor on.
	fullStartBlock, eventStartBlock, err := resolveStartPlan(
		ctx, ci, fsmCaller, latestConfirmedNumber, latestConfirmedTimestamp,
	)
	if err != nil {
		return 0, err
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
	// it when there is no anchor to backfill from, or when that region is empty
	// or already covered.
	firstFspEvent := states[database.LogFloor]
	backfillEvents := eventStartBlock > 0 && eventStartBlock < fullStartBlock &&
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
	} else if eventStartBlock == 0 {
		logger.Warnf("Skipping FSP event backfill: no reward epoch has FSP start data to anchor on")
	} else if eventStartBlock >= fullStartBlock {
		logger.Infof("Skipping FSP event backfill: event window is covered by the full catchup range")
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

// resolveStartPlan works out the epoch window first, then the event anchor, and
// only then searches for the full-indexing start block. That order matters: the
// anchor is the oldest block FSP mode needs, it comes from contract state rather
// than block history, and it bounds the timestamp search below so the search
// never probes blocks the configuration does not require.
//
// The returned eventStartBlock is zero when no epoch has recorded start data to
// anchor on, in which case there is nothing to backfill.
func resolveStartPlan(
	ctx context.Context,
	ci *core.Engine,
	fsm fsmReader,
	latestConfirmedNumber uint64,
	latestConfirmedTimestamp uint64,
) (fullStartBlock, eventStartBlock uint64, err error) {
	currentEpochID, err := fspCurrentEpochID(ctx, fsm)
	if err != nil {
		return 0, 0, err
	}

	// Base of the full-block lookback window: the oldest served epoch's start,
	// or the confirmed tip when serving only the current epoch.
	baseTimestamp, baseBlock := latestConfirmedTimestamp, latestConfirmedNumber
	startEpochID := currentEpochID
	params := ci.Params()

	if params.HistoryEpochs > 0 {
		desiredEpochID := historyStartEpochID(currentEpochID, params.HistoryEpochs)
		resolvedEpochID, info, ok, err := resolveStartEpoch(ctx, fsm, desiredEpochID, currentEpochID)
		if err != nil {
			return 0, 0, err
		}

		if !ok {
			// An FSM deployment still in its bootstrap epoch: fall back to the tip
			// rather than resolving a zero start block, which would full-index from
			// genesis.
			logger.Warnf("Current reward epoch %d has no FSP start data yet; falling back to the confirmed tip", currentEpochID)
		} else {
			startEpochID = resolvedEpochID
			if resolvedEpochID > desiredEpochID {
				logger.Errorf(
					"history_epochs=%d requests reward epoch %d, but this FSM deployment's start data begins at epoch %d; catching up from there — lower history_epochs to fit the deployment",
					params.HistoryEpochs, desiredEpochID, resolvedEpochID,
				)
			}
			// Base the lookback on the epoch's start once it is confirmed; until then
			// the tip is as far as indexing can go anyway.
			if info.RewardEpochStartBlock <= latestConfirmedNumber {
				baseTimestamp, baseBlock = info.RewardEpochStartTs, info.RewardEpochStartBlock
			}
		}
	}

	eventStartBlock, err = fspEventBackfillAnchor(ctx, fsm, startEpochID)
	if err != nil {
		return 0, 0, errors.Wrap(err, "compute FSP event backfill start")
	}

	if eventStartBlock == 0 {
		// No recorded epoch data means no older block can be required, so there is
		// nothing to look back for and nothing to bound a search with. The caller
		// logs the skipped backfill.
		return baseBlock, 0, nil
	}

	if err := ensureBlockAvailable(ctx, blockProbe(ci), eventStartBlock, startEpochID, params.HistoryEpochs); err != nil {
		return 0, 0, err
	}

	fullStartBlock, err = findStartBlockByLookback(ctx, ci, baseTimestamp, baseBlock, eventStartBlock)
	if err != nil {
		return 0, 0, errors.Wrapf(err, "find start block for epoch %d with lookback", startEpochID)
	}

	return fullStartBlock, eventStartBlock, nil
}

// blockProbe asks the node for a block header, retrying transient failures like
// any other RPC read but giving up as soon as the node reports it does not have
// the block, since no amount of retrying changes that answer.
func blockProbe(ci *core.Engine) func(context.Context, uint64) error {
	return func(ctx context.Context, block uint64) error {
		_, err := boff.RetryWithMaxElapsed(ctx, func() (*chain.Header, error) {
			callCtx, cancel := context.WithTimeout(ctx, config.RPCTimeout)
			defer cancel()

			header, err := ci.Client().HeaderByNumber(callCtx, new(big.Int).SetUint64(block))
			if chain.IsBlockUnavailable(err) {
				return nil, boff.Permanent(err)
			}

			return header, err
		}, "probeAnchorBlock")

		return err
	}
}

// ensureBlockAvailable fails, permanently, when the node cannot serve the oldest
// block FSP mode needs. Nodes that were state synced discard the blocks before
// their sync point, and without this check the missing data surfaces much later
// as an opaque RPC error inside the startup retry loop, which retries it
// forever. The block number comes from contract state, so it is known before any
// historical data is read.
//
// Only a node that answers "no such block" is fatal. A transient failure is
// returned as an ordinary error so the caller's retry loop keeps trying until the
// node either serves the block or says it does not have it.
func ensureBlockAvailable(
	ctx context.Context,
	probe func(context.Context, uint64) error,
	block, startEpochID, historyEpochs uint64,
) error {
	err := probe(ctx, block)
	switch {
	case err == nil:
		return nil
	case !chain.IsBlockUnavailable(err):
		return errors.Wrapf(err, "probe block %d for reward epoch %d", block, startEpochID)
	default:
		return boff.Permanent(errors.Errorf(
			"node cannot serve block %d, the oldest block FSP mode needs: it anchors the event backfill for reward epoch %d, "+
				"required by indexer.history_epochs=%d. Use a node with history back to that block. Underlying error: %s",
			block, startEpochID, historyEpochs, err,
		))
	}
}

// findStartBlockByLookback resolves the full-indexing start: lookback seconds
// below baseTimestamp. lowestBlock bounds the search from below — it is the
// event anchor, which is always at or below the result and always required, so
// the search cannot probe a block the node is not already expected to have.
func findStartBlockByLookback(
	ctx context.Context, ci *core.Engine, baseTimestamp, endBlockNumber, lowestBlock uint64,
) (uint64, error) {
	if endBlockNumber == 0 {
		return 0, nil
	}

	searchTimestamp := saturatingSub(baseTimestamp, fspTxLookbackSeconds)

	return chain.GetNearestBlockByTimestampFromChain(
		ctx,
		searchTimestamp,
		ci.Client(),
		min(lowestBlock, endBlockNumber),
		endBlockNumber,
	)
}
