package fsp

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	systemcontract "github.com/flare-foundation/go-flare-common/pkg/contracts/system"
	"github.com/pkg/errors"
)

// fspEventLeadBlocks is how far before the oldest reward epoch's start block the
// FSP event backfill begins, to capture the signing-policy protocol that
// precedes that epoch. Block offset sized for ~1s blocks (overestimates on
// chains with slower blocks).
const fspEventLeadBlocks = uint64(2 * 60 * 60) // ~2h

func fspCurrentEpochID(
	ctx context.Context,
	fsm *systemcontract.FlareSystemsManagerCaller,
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
