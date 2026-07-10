package fsp

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/pkg/errors"
)

// rewardEpochStartInfo and randomAcquisitionInfo alias the anonymous return
// structs of the generated FlareSystemsManager bindings.
type rewardEpochStartInfo = struct {
	RewardEpochStartTs    uint64
	RewardEpochStartBlock uint64
}

type randomAcquisitionInfo = struct {
	RandomAcquisitionStartTs    uint64
	RandomAcquisitionStartBlock uint64
	RandomAcquisitionEndTs      uint64
	RandomAcquisitionEndBlock   uint64
}

// fsmReader is the read-only subset of the FlareSystemsManager bindings that
// FSP startup planning and retention depend on.
type fsmReader interface {
	GetCurrentRewardEpochId(opts *bind.CallOpts) (*big.Int, error)
	GetRewardEpochStartInfo(opts *bind.CallOpts, rewardEpochID *big.Int) (rewardEpochStartInfo, error)
	GetRandomAcquisitionInfo(opts *bind.CallOpts, rewardEpochID *big.Int) (randomAcquisitionInfo, error)
}

// historyStartEpochID returns the oldest reward epoch whose full data the
// indexer aims to serve: historyEpochs epochs ending at the current one.
// historyEpochs == 0 is treated as just the current epoch.
func historyStartEpochID(currentEpochID, historyEpochs uint64) uint64 {
	if historyEpochs == 0 {
		historyEpochs = 1
	}
	if historyEpochs-1 >= currentEpochID {
		return 0
	}
	return currentEpochID - (historyEpochs - 1)
}

func epochStartInfo(ctx context.Context, fsm fsmReader, epochID uint64) (rewardEpochStartInfo, error) {
	info, err := fsm.GetRewardEpochStartInfo(&bind.CallOpts{Context: ctx}, new(big.Int).SetUint64(epochID))
	if err != nil {
		return rewardEpochStartInfo{}, errors.Wrapf(err, "getRewardEpochStartInfo(%d)", epochID)
	}
	return info, nil
}

// resolveStartEpoch returns the reward epoch the indexer serves history from:
// desiredEpochID when the FSM has start data recorded for it — a single read,
// the steady-state path — otherwise the oldest epoch above it that does.
// getRewardEpochStartInfo is a bare storage read that returns zeros for
// epochs the current FSM deployment never started (pre-FSP epochs, or
// anything before a redeployed FSM's bootstrap epoch), and the deployment
// exposes no getter for where its data begins, so the fallback shrinks the
// history window one epoch at a time until it reaches recorded data. ok is
// false when no epoch up to currentEpochID has start data (a deployment
// still in its bootstrap epoch).
func resolveStartEpoch(
	ctx context.Context, fsm fsmReader, desiredEpochID, currentEpochID uint64,
) (uint64, rewardEpochStartInfo, bool, error) {
	for epochID := desiredEpochID; epochID <= currentEpochID; epochID++ {
		info, err := epochStartInfo(ctx, fsm, epochID)
		if err != nil {
			return 0, rewardEpochStartInfo{}, false, err
		}
		if info.RewardEpochStartTs != 0 {
			return epochID, info, true, nil
		}
	}
	return 0, rewardEpochStartInfo{}, false, nil
}

func saturatingSub(a, b uint64) uint64 {
	if a <= b {
		return 0
	}
	return a - b
}

// fspEventLeadBlocks / fspEventLeadSeconds approximate how far before a
// reward epoch's start its signing-policy protocol begins (2h nominal lead,
// blocks sized for ~1s block times). Used only as a fallback for epochs
// without recorded random-acquisition data; everywhere else the anchor comes
// from the exact values the contract recorded.
const (
	fspEventLeadBlocks  = uint64(2 * 60 * 60)
	fspEventLeadSeconds = uint64(2 * 60 * 60)
)

func fspCurrentEpochID(
	ctx context.Context,
	fsm fsmReader,
) (uint64, error) {
	epochIDBig, err := fsm.GetCurrentRewardEpochId(&bind.CallOpts{Context: ctx})
	if err != nil {
		return 0, errors.Wrap(err, "getCurrentRewardEpochId")
	}
	if epochIDBig == nil {
		return 0, errors.New("getCurrentRewardEpochId returned nil")
	}

	return epochIDBig.Uint64(), nil
}

// eventAnchor is the point from which an epoch's metadata-event window is
// fully covered.
type eventAnchor struct {
	block     uint64
	timestamp uint64
}

// fspEventAnchor returns the anchor of the metadata-event window that serving
// epochs from startEpochID requires: the recorded RandomAcquisitionStarted
// block/timestamp — the first event of an epoch's signing-policy protocol,
// correct however long the epoch start was delayed — of the epoch two before
// startEpochID, so consumers also have the boundary events of the epochs
// preceding the oldest one they reconstruct. When that epoch has no start
// data (startEpochID sits within two epochs of the deployment's bootstrap),
// the anchor walks up to the oldest epoch that does, at most startEpochID
// itself. Epochs without random-acquisition data fall back to the nominal
// lead window below the epoch start; ok is false when no probed epoch has
// start data.
func fspEventAnchor(ctx context.Context, fsm fsmReader, startEpochID uint64) (eventAnchor, bool, error) {
	for epochID := saturatingSub(startEpochID, 2); epochID <= startEpochID; epochID++ {
		startInfo, err := epochStartInfo(ctx, fsm, epochID)
		if err != nil {
			return eventAnchor{}, false, err
		}
		if startInfo.RewardEpochStartTs == 0 {
			continue
		}

		raInfo, err := fsm.GetRandomAcquisitionInfo(&bind.CallOpts{Context: ctx}, new(big.Int).SetUint64(epochID))
		if err != nil {
			return eventAnchor{}, false, errors.Wrapf(err, "getRandomAcquisitionInfo(%d)", epochID)
		}
		if raInfo.RandomAcquisitionStartBlock != 0 {
			return eventAnchor{
				block:     raInfo.RandomAcquisitionStartBlock,
				timestamp: raInfo.RandomAcquisitionStartTs,
			}, true, nil
		}

		return eventAnchor{
			block:     saturatingSub(startInfo.RewardEpochStartBlock, fspEventLeadBlocks),
			timestamp: saturatingSub(startInfo.RewardEpochStartTs, fspEventLeadSeconds),
		}, true, nil
	}
	return eventAnchor{}, false, nil
}

// retentionMarginSeconds keeps the history-drop boundary a safety margin
// below the event anchor, so deletion never races the exact block the
// backfill guarantees coverage from.
const retentionMarginSeconds = uint64(60 * 60)

// fspRetentionBoundary returns the timestamp below which history drop may
// delete: a safety margin below the event anchor of the configured
// history_epochs window's start, via the same fspEventAnchor as the startup
// backfill. It is recomputed from chain state on every call, so the
// retention window slides forward as epochs switch over — and, because the
// anchor is recorded epoch data rather than history_epochs times a nominal
// duration, it stays correct when an epoch start is delayed. Returns 0
// (delete nothing) while the window's start has no recorded data yet: unlike
// startup, retention does not shrink the window to where data begins, so
// after an FSM redeploy nothing is deleted until the configured window again
// lies fully within recorded epochs. Startup's catchup anchor is always at
// or above the configured window's, so retention never deletes a region the
// backfill guaranteed.
func fspRetentionBoundary(ctx context.Context, fsm fsmReader, historyEpochs uint64) (uint64, error) {
	currentEpochID, err := fspCurrentEpochID(ctx, fsm)
	if err != nil {
		return 0, err
	}

	anchor, ok, err := fspEventAnchor(ctx, fsm, historyStartEpochID(currentEpochID, historyEpochs))
	if err != nil || !ok {
		return 0, err
	}

	return saturatingSub(anchor.timestamp, retentionMarginSeconds), nil
}

// fspEventBackfillAnchor returns the block from which to backfill FSP events:
// the event anchor of the epoch two before startEpochID, so consumers also
// have the boundary events of the epoch preceding the oldest one they
// reconstruct. Events are then scanned continuously from here up to where
// full-block catchup takes over; a continuous range (not per-epoch windows)
// is required because community reward offers can be submitted at any point
// during an epoch. ok is false when no epoch in range has start data
// (nothing to backfill).
func fspEventBackfillAnchor(
	ctx context.Context,
	fsm fsmReader,
	startEpochID uint64,
) (uint64, bool, error) {
	anchor, ok, err := fspEventAnchor(ctx, fsm, startEpochID)
	if err != nil || !ok {
		return 0, false, err
	}
	return anchor.block, true, nil
}
