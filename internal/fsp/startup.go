package fsp

import (
	"context"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/check"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/core"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/database"

	systemcontract "github.com/flare-foundation/go-flare-common/pkg/contracts/system"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
	"github.com/pkg/errors"
)

const fspFsmContractName = "FlareSystemsManager"

// fspTxLookbackSeconds is how far full-block indexing reaches below its base —
// the confirmed tip, or the oldest served epoch's start. Sized generously so
// reward calculation for that epoch has all of its submission data.
const fspTxLookbackSeconds = uint64(60 * 60)

// historyHint says what a missing block means for the node in use.
const historyHint = "use a node with history back to it"

// startPlan is what startup does: full-index from catchupFrom, and backfill FSP
// event logs over [eventsFrom, eventsTo] when eventsFrom is set.
type startPlan struct {
	catchupFrom uint64
	eventsFrom  uint64 // 0 when the events are already indexed
	eventsTo    uint64
}

// IndexStartup catches the database up to the chain tip and returns the last
// indexed block.
func IndexStartup(ctx context.Context, ci *core.Engine) (uint64, error) {
	tip, tipTimestamp, err := ci.FetchLastBlockIndex(ctx)
	if err != nil {
		return 0, errors.Wrap(err, "fetch last block")
	}

	fsm, err := newFsmCaller(ctx, ci)
	if err != nil {
		return 0, err
	}

	// What the database covers decides how much history the node has to serve.
	states, err := database.GetStates(
		ci.DB().WithContext(ctx), database.BlockFloor, database.LastIndexed, database.LogFloor,
	)
	if err != nil {
		return 0, errors.Wrap(err, "read states")
	}

	cov := coverage(states)

	plan, err := planStartup(ctx, ci, fsm, cov, tip, tipTimestamp)
	if err != nil {
		return 0, err
	}

	logger.Infof(
		"FSP startup plan: catchup_from=%d, events=[%d, %d], latest_confirmed=%d",
		plan.catchupFrom, plan.eventsFrom, plan.eventsTo, tip,
	)

	if plan.eventsFrom > 0 {
		if err := backfillEvents(ctx, ci, plan.eventsFrom, plan.eventsTo); err != nil {
			return 0, err
		}
	}

	lastIndexed := tip
	if plan.catchupFrom <= tip {
		if err := check.Block(
			ctx, ci.Client(), plan.catchupFrom, "block %d unavailable; %s", plan.catchupFrom, historyHint,
		); err != nil {
			return 0, err
		}

		if lastIndexed, err = ci.IndexHistory(ctx, plan.catchupFrom); err != nil {
			return 0, errors.Wrap(err, "catchup")
		}
	}

	logger.Infof("FSP startup complete: last_indexed=%d", lastIndexed)

	return lastIndexed, nil
}

// planStartup resolves the start blocks, reading from the node only what the
// database does not already cover.
func planStartup(
	ctx context.Context,
	ci *core.Engine,
	fsm fsmReader,
	cov coverage,
	tip, tipTimestamp uint64,
) (startPlan, error) {
	historyEpochs := ci.Params().HistoryEpochs

	startEpoch, baseBlock, baseTimestamp, err := lookbackBase(ctx, fsm, historyEpochs, tip, tipTimestamp)
	if err != nil {
		return startPlan{}, err
	}

	// The anchor is the oldest block FSP mode needs, known from contract state
	// before any history is read.
	anchor, err := fspEventBackfillAnchor(ctx, fsm, startEpoch)
	if err != nil {
		return startPlan{}, errors.Wrap(err, "resolve event anchor")
	}
	if anchor == 0 {
		logger.Warnf("No reward epoch has FSP start data to anchor on; skipping the event backfill")

		return startPlan{catchupFrom: cov.catchupFrom(baseBlock)}, nil
	}

	lookback := saturatingSub(baseTimestamp, fspTxLookbackSeconds)
	floor, blocksIndexed := cov.blockFloor(lookback)
	eventsIndexed := cov.eventsIndexed(anchor)

	// With both regions indexed nothing below the indexed range is read, so a
	// node without that history is fine.
	if !blocksIndexed || !eventsIndexed {
		if err := check.Block(
			ctx, ci.Client(), anchor,
			"event anchor %d for reward epoch %d (history_epochs=%d) unavailable; %s",
			anchor, startEpoch, historyEpochs, historyHint,
		); err != nil {
			return startPlan{}, err
		}
	}

	fullStart := floor
	if !blocksIndexed && baseBlock > 0 {
		// The anchor bounds the search from below, so it never probes a block the
		// configuration does not require; a zero bound would widen the search past
		// it.
		fullStart, err = chain.GetNearestBlockByTimestampFromChain(
			ctx, lookback, ci.Client(), min(anchor, baseBlock), baseBlock,
		)
		if err != nil {
			return startPlan{}, errors.Wrapf(err, "find start block for epoch %d", startEpoch)
		}
	}

	// Backfill up to where catchup starts, not to the window floor: blocks below
	// that are the ones nobody fetches now, and their events are only covered if
	// FSP's collectors filled them, which is what the log floor records. An anchor
	// at or above the catchup start needs no backfill because catchup covers it,
	// so no log floor is recorded this run; the next start, by which point the
	// anchor sits below the indexed range, records one.
	plan := startPlan{catchupFrom: cov.catchupFrom(fullStart)}
	if !eventsIndexed && anchor < plan.catchupFrom {
		plan.eventsFrom, plan.eventsTo = anchor, plan.catchupFrom-1
	}

	return plan, nil
}

// coverage is what the database guarantees, and decides how much history the
// node has to serve.
type coverage map[database.StateName]database.State

// catchupFrom returns the first block to index: after the indexed range when it
// already reaches fullStart, otherwise fullStart itself.
func (c coverage) catchupFrom(fullStart uint64) uint64 {
	floor, last := c[database.BlockFloor], c[database.LastIndexed]
	if database.IsSet(floor) && database.IsSet(last) &&
		floor.Index <= fullStart && last.Index >= fullStart {
		return last.Index + 1
	}

	return fullStart
}

// blockFloor reports the indexed block floor, and whether it reaches at or below
// lookbackTimestamp. Compared by timestamp because the lookback target is one,
// and resolving it to a block is the search this decision skips.
func (c coverage) blockFloor(lookbackTimestamp uint64) (uint64, bool) {
	floor, last := c[database.BlockFloor], c[database.LastIndexed]
	indexed := database.IsSet(floor) && database.IsSet(last) &&
		last.Index >= floor.Index && floor.BlockTimestamp > 0 &&
		floor.BlockTimestamp <= lookbackTimestamp

	return floor.Index, indexed
}

// eventsIndexed reports whether FSP event logs are indexed from block or below.
// Only the log floor answers this: a fully indexed range proves coverage for
// the collectors that filled it, which need not have been FSP's.
func (c coverage) eventsIndexed(block uint64) bool {
	floor := c[database.LogFloor]

	return database.IsSet(floor) && floor.Index <= block
}

// backfillEvents indexes FSP event logs over [from, to] and records the floor.
func backfillEvents(ctx context.Context, ci *core.Engine, from, to uint64) error {
	addresses, topics, err := resolveFspContractAddresses(ctx, ci.ContractResolver())
	if err != nil {
		return err
	}
	if err := backfillFspEventLogs(ctx, ci, from, to, addresses, topics); err != nil {
		return errors.Wrap(err, "backfill FSP events")
	}

	timestamp, err := ci.FetchBlockTimestamp(ctx, from)
	if err != nil {
		return errors.Wrapf(err, "fetch timestamp of block %d", from)
	}

	return database.LowerStateFloor(ci.DB(), database.LogFloor, from, timestamp)
}

// newFsmCaller binds the FlareSystemsManager reader.
func newFsmCaller(ctx context.Context, ci *core.Engine) (*systemcontract.FlareSystemsManagerCaller, error) {
	address, err := ci.ContractResolver().ResolveByName(ctx, fspFsmContractName)
	if err != nil {
		return nil, err
	}

	caller, err := systemcontract.NewFlareSystemsManagerCaller(address, ci.Client())
	if err != nil {
		return nil, errors.Wrap(err, "bind FlareSystemsManager")
	}

	return caller, nil
}
