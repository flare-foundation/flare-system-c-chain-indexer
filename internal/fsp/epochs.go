package fsp

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	systemcontract "github.com/flare-foundation/go-flare-common/pkg/contracts/system"
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

// fspEventLeadBlocks is how far before the oldest reward epoch's start block the
// FSP event backfill begins, to capture the signing-policy protocol that
// precedes that epoch. Block offset sized for ~1s blocks (overestimates on
// chains with slower blocks).
const fspEventLeadBlocks = uint64(2 * 60 * 60) // ~2h

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

// fspEventBackfillStartBlock returns the block from which to backfill FSP events.
// It is ~fspEventLeadBlocks before the start of the oldest reward epoch we need
// — two epochs before startEpochID, so consumers also have the boundary events
// of the epoch preceding the oldest one they reconstruct. Events are then
// scanned continuously from here up to where full-block catchup takes over; a
// continuous range (not per-epoch windows) is required because community reward
// offers can be submitted at any point during an epoch.
func fspEventBackfillStartBlock(
	ctx context.Context,
	fsm *systemcontract.FlareSystemsManagerCaller,
	startEpochID uint64,
) (uint64, error) {
	firstEpochID := uint64(0)
	if startEpochID > 2 {
		firstEpochID = startEpochID - 2
	}

	info, err := fsm.GetRewardEpochStartInfo(&bind.CallOpts{Context: ctx}, new(big.Int).SetUint64(firstEpochID))
	if err != nil {
		return 0, errors.Wrapf(err, "getRewardEpochStartInfo(%d)", firstEpochID)
	}

	if info.RewardEpochStartBlock <= fspEventLeadBlocks {
		return 0, nil
	}
	return info.RewardEpochStartBlock - fspEventLeadBlocks, nil
}
