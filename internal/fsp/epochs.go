package fsp

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	systemcontract "github.com/flare-foundation/go-flare-common/pkg/contracts/system"
	"github.com/pkg/errors"
)

// Window around each reward-epoch start block used to capture FSP reward epoch events.
// Values are block offsets, sized for ~1s blocks (overestimates on chains with slower blocks).
const (
	fspWindowBeforeBlocks = uint64(2 * 60 * 60) // ~2h before SigningPolicy protocol start
	fspWindowAfterBlocks  = uint64(15 * 60)     // ~15min after, to capture inflation reward offers
)

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

func fspRewardEpochEventRanges(
	ctx context.Context,
	fsm *systemcontract.FlareSystemsManagerCaller,
	startEpochID uint64,
	latestConfirmedBlock uint64,
) ([]fspBlockRange, error) {
	eventRanges := make([]fspBlockRange, 0, 3)
	firstEpochID := uint64(0)
	if startEpochID > 2 {
		firstEpochID = startEpochID - 2
	}

	for epochID := firstEpochID; epochID <= startEpochID; epochID++ {
		info, err := fsm.GetRewardEpochStartInfo(&bind.CallOpts{Context: ctx}, new(big.Int).SetUint64(epochID))
		if err != nil {
			return nil, errors.Wrapf(err, "getRewardEpochStartInfo(%d)", epochID)
		}

		startBlock := info.RewardEpochStartBlock
		fromBlock := uint64(0)
		if startBlock > fspWindowBeforeBlocks {
			fromBlock = startBlock - fspWindowBeforeBlocks
		}
		toBlock := startBlock + fspWindowAfterBlocks
		if toBlock > latestConfirmedBlock {
			toBlock = latestConfirmedBlock
		}

		eventRanges = append(eventRanges, fspBlockRange{from: fromBlock, to: toBlock})
	}

	return mergeFspBlockRanges(eventRanges), nil
}

func mergeFspBlockRanges(ranges []fspBlockRange) []fspBlockRange {
	if len(ranges) == 0 {
		return nil
	}

	merged := make([]fspBlockRange, 0, len(ranges))
	current := ranges[0]

	for i := 1; i < len(ranges); i++ {
		next := ranges[i]
		overlapsOrAdjacent := next.from <= current.to
		if !overlapsOrAdjacent && current.to != ^uint64(0) {
			overlapsOrAdjacent = next.from == current.to+1
		}
		if overlapsOrAdjacent {
			if next.to > current.to {
				current.to = next.to
			}
			continue
		}

		merged = append(merged, current)
		current = next
	}

	merged = append(merged, current)
	return merged
}
