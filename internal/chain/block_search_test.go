package chain

import (
	"context"
	"fmt"
	"testing"
)

// linearChain is a synthetic chain of blocks [0, maxBlock] where block n has
// timestamp baseTime+n. Querying a block above maxBlock returns an error, so a
// uint64 underflow in the search (which would query a near-MaxUint64 block)
// surfaces as a test failure rather than silently passing.
func linearChain(maxBlock, baseTime uint64) blockTimeLookup {
	return func(_ context.Context, n uint64) (uint64, error) {
		if n > maxBlock {
			return 0, fmt.Errorf("block %d does not exist (chain head %d)", n, maxBlock)
		}
		return baseTime + n, nil
	}
}

// prunedChain is a linearChain whose blocks below earliest were discarded, as on
// a state synced node.
func prunedChain(earliest, maxBlock, baseTime uint64) blockTimeLookup {
	full := linearChain(maxBlock, baseTime)
	return func(ctx context.Context, n uint64) (uint64, error) {
		if n < earliest {
			return 0, fmt.Errorf("block %d is not available (node history starts at %d)", n, earliest)
		}
		return full(ctx, n)
	}
}

// A start block bounds the search from below, so a node missing older blocks is
// searched successfully as long as the caller only asks for blocks it requires.
// FSP mode passes its event anchor here for exactly that reason.
func TestNearestBlockByTimestampStaysAboveStartBlock(t *testing.T) {
	const (
		base     = 1000
		earliest = 900_000
		head     = 1_000_000
	)

	got, err := nearestBlockByTimestamp(
		context.Background(), base+950_000, earliest, head,
		prunedChain(earliest, head, base),
	)
	if err != nil {
		t.Fatalf("search probed a block the node does not have: %v", err)
	}
	if want := uint64(950_000); got != want {
		t.Fatalf("got block %d, want %d", got, want)
	}
}

// Without that bound the descent steps below the node's history and fails. This
// is the failure state synced nodes hit, and it is why the start block matters.
func TestNearestBlockByTimestampFailsBelowPrunedHistory(t *testing.T) {
	const (
		base     = 1000
		earliest = 900_000
		head     = 1_000_000
	)

	_, err := nearestBlockByTimestamp(
		context.Background(), base+950_000, 0, head,
		prunedChain(earliest, head, base),
	)
	if err == nil {
		t.Fatal("expected the unbounded search to probe below the node's history")
	}
}

func TestNearestBlockByTimestamp(t *testing.T) {
	const base = 1000

	tests := []struct {
		name            string
		maxBlock        uint64
		start           uint64
		end             uint64
		searchTimestamp uint64
		want            uint64
	}{
		{
			// Short chain (head far below searchWindowBlocks): the descent must
			// floor at genesis instead of underflowing past zero.
			name: "short chain does not underflow", maxBlock: 100,
			start: 0, end: 100, searchTimestamp: base + 50, want: 50,
		},
		{
			// searchTimestamp older than genesis: must terminate at block 0, not
			// loop subtracting until it underflows.
			name: "timestamp before genesis returns genesis", maxBlock: 100,
			start: 0, end: 100, searchTimestamp: 1, want: 0,
		},
		{
			// Exact boundary: first block whose timestamp >= searchTimestamp.
			name: "short chain exact boundary", maxBlock: 100,
			start: 0, end: 100, searchTimestamp: base + 100, want: 100,
		},
		{
			// Large chain: window narrowing in 5-day steps, then binary search.
			name: "large chain window narrowing", maxBlock: 1_000_000,
			start: 0, end: 1_000_000, searchTimestamp: base + 800_000, want: 800_000,
		},
		{
			// Explicit start block skips window narrowing and binary-searches the
			// given range directly.
			name: "explicit start range", maxBlock: 1_000_000,
			start: 100, end: 200, searchTimestamp: base + 150, want: 150,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nearestBlockByTimestamp(
				context.Background(), tc.searchTimestamp, tc.start, tc.end,
				linearChain(tc.maxBlock, base),
			)
			if err != nil {
				t.Fatalf("unexpected error (possible underflow querying a non-existent block): %v", err)
			}
			if got != tc.want {
				t.Fatalf("got block %d, want %d", got, tc.want)
			}
		})
	}
}
