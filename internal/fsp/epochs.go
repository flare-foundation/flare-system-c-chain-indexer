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

// oldestEpochWithStartInfo finds the lowest epoch in [lo, hi] with recorded
// start data. The FSM writes start info at every epoch switchover, so within
// one deployment it is zero up to some epoch (pre-FSP epochs, or everything
// before a redeployed FSM's bootstrap epoch) and non-zero from there on —
// the monotone split binary search requires.
func oldestEpochWithStartInfo(
	ctx context.Context, fsm fsmReader, lo, hi uint64,
) (uint64, rewardEpochStartInfo, bool, error) {
	var (
		found     bool
		foundID   uint64
		foundInfo rewardEpochStartInfo
	)
	for lo <= hi {
		mid := lo + (hi-lo)/2
		info, err := fsm.GetRewardEpochStartInfo(&bind.CallOpts{Context: ctx}, new(big.Int).SetUint64(mid))
		if err != nil {
			return 0, rewardEpochStartInfo{}, false, errors.Wrapf(err, "getRewardEpochStartInfo(%d)", mid)
		}
		if info.RewardEpochStartTs != 0 {
			found, foundID, foundInfo = true, mid, info
			if mid == 0 {
				break
			}
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}
	return foundID, foundInfo, found, nil
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

// fspEventAnchor resolves the oldest epoch in [lo, hi] with recorded start
// data and returns the anchor of its metadata-event window: the recorded
// RandomAcquisitionStarted block/timestamp — the first event of the epoch's
// signing-policy protocol — which stays correct however long the epoch start
// was delayed. Epochs without random-acquisition data (an FSM deployment's
// bootstrap epoch) fall back to the nominal lead window below the epoch
// start; ok is false when no epoch in range has start data.
func fspEventAnchor(ctx context.Context, fsm fsmReader, lo, hi uint64) (eventAnchor, bool, error) {
	epochID, startInfo, ok, err := oldestEpochWithStartInfo(ctx, fsm, lo, hi)
	if err != nil || !ok {
		return eventAnchor{}, false, err
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

// retentionMarginSeconds keeps the history-drop boundary a safety margin
// below the event anchor, so deletion never races the exact block the
// backfill guarantees coverage from.
const retentionMarginSeconds = uint64(60 * 60)

// fspRetentionBoundary returns the timestamp below which history drop may
// delete: a safety margin below the event anchor of the oldest epoch whose
// metadata the indexer must serve (two epochs before the history_epochs
// window, matching the startup backfill). It is recomputed from chain state
// on every call, so the retention window slides forward as epochs switch
// over — and, because the anchor is recorded epoch data rather than
// history_epochs times a nominal duration, it stays correct when an epoch
// start is delayed. Returns 0 (delete nothing) while no epoch has start data.
func fspRetentionBoundary(ctx context.Context, fsm fsmReader, historyEpochs uint64) (uint64, error) {
	currentEpochID, err := fspCurrentEpochID(ctx, fsm)
	if err != nil {
		return 0, err
	}

	firstEpochID := saturatingSub(historyStartEpochID(currentEpochID, historyEpochs), 2)
	anchor, ok, err := fspEventAnchor(ctx, fsm, firstEpochID, currentEpochID)
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
	anchor, ok, err := fspEventAnchor(ctx, fsm, saturatingSub(startEpochID, 2), startEpochID)
	if err != nil || !ok {
		return 0, false, err
	}
	return anchor.block, true, nil
}
