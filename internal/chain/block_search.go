package chain

import (
	"context"
	"math"
	"math/big"

	"github.com/flare-foundation/go-flare-common/pkg/logger"
	"github.com/pkg/errors"
)

const searchWindowBlocks = uint64(5 * 24 * 3600) // Heuristic: 5 days of blocks, assuming 1 sec per block

// blockTimeLookup returns the timestamp of a block by number. It is the only
// chain dependency of the search, injected so the search/guard logic can be
// unit-tested against a synthetic chain without an RPC node.
type blockTimeLookup func(ctx context.Context, blockNumber uint64) (uint64, error)

// GetNearestBlockByTimestampFromChain returns the first block in [startBlockNumber, endBlockNumber]
// whose timestamp is greater than or equal to searchTimestamp.
func GetNearestBlockByTimestampFromChain(
	ctx context.Context,
	searchTimestamp uint64,
	client *Client,
	startBlockNumber uint64,
	endBlockNumber uint64,
) (uint64, error) {
	return nearestBlockByTimestamp(
		ctx, searchTimestamp, startBlockNumber, endBlockNumber,
		func(ctx context.Context, blockNumber uint64) (uint64, error) {
			return blockTimestampByNumber(ctx, client, blockNumber)
		},
	)
}

func nearestBlockByTimestamp(
	ctx context.Context,
	searchTimestamp uint64,
	startBlockNumber uint64,
	endBlockNumber uint64,
	blockTime blockTimeLookup,
) (uint64, error) {
	logger.Debugf(
		"Block search starting: search_timestamp=%d, start_block=%d, end_block=%d",
		searchTimestamp, startBlockNumber, endBlockNumber,
	)

	searchStartBlockNumber := startBlockNumber
	searchEndBlockNumber := endBlockNumber

	if startBlockNumber == 0 {
		// If start block was not specified in config, try to reduce the search space by going back in steps of 5 days
		// until we find a block earlier than searchTimestamp.
		// This is to avoid querying for very old blocks during binary search - the RPC node might not have full block history.
		startCandidate := endBlockNumber
		candidateBlockTime := uint64(math.MaxUint64)

		var err error
		for candidateBlockTime > searchTimestamp {
			// Guard the uint64 subtraction: never step below genesis. On short
			// chains / freshly reset nodes endBlockNumber can be below
			// searchWindowBlocks, and a searchTimestamp older than genesis would
			// otherwise loop until startCandidate underflows past 0 and we query a
			// non-existent block. Floor at 0 and let the binary search resolve.
			if startCandidate < searchWindowBlocks {
				startCandidate = 0
				break
			}

			startCandidate -= searchWindowBlocks
			candidateBlockTime, err = blockTime(ctx, startCandidate)
			if err != nil {
				return 0, errors.Wrap(err, "GetNearestBlockByTimestampFromChain")
			}
		}

		searchStartBlockNumber = startCandidate
		// Clamp to endBlockNumber: when the window is floored at genesis the
		// nominal upper bound can exceed the chain head.
		searchEndBlockNumber = min(startCandidate+searchWindowBlocks, endBlockNumber)
		logger.Debugf("Block search window narrowed: from=%d, to=%d", searchStartBlockNumber, searchEndBlockNumber)
	}

	blockNumber, err := binarySearchBlockByTimestamp(
		ctx, searchTimestamp, searchStartBlockNumber, searchEndBlockNumber, blockTime,
	)
	if err != nil {
		return 0, errors.Wrap(err, "GetNearestBlockByTimestampFromChain")
	}

	logger.Debugf("Block search complete: block=%d", blockNumber)
	return blockNumber, nil
}

func binarySearchBlockByTimestamp(
	ctx context.Context,
	searchTimestamp uint64,
	startBlockNumber uint64,
	endBlockNumber uint64,
	blockTime blockTimeLookup,
) (uint64, error) {
	low := startBlockNumber
	high := endBlockNumber

	for low < high {
		mid := low + (high-low)/2

		ts, err := blockTime(ctx, mid)
		if err != nil {
			return 0, err
		}

		if ts >= searchTimestamp {
			high = mid
		} else {
			low = mid + 1
		}
	}

	return low, nil
}

func blockTimestampByNumber(ctx context.Context, client *Client, blockNumber uint64) (uint64, error) {
	block, err := client.BlockByNumber(ctx, new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return 0, err
	}

	return block.Time(), nil
}
