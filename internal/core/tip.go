package core

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/boff"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/database"

	"github.com/flare-foundation/go-flare-common/pkg/logger"
	"github.com/pkg/errors"
)

// ErrBehind is returned by continuous indexing when an indexed block is at
// least indexer.max_lag_seconds of chain time behind the chain tip. It is
// permanent: the retry loop stops, the process exits, and the restart catches
// up in batch mode.
var ErrBehind = errors.New("indexing fell behind the chain")

// chainTip is one observation of the confirmed tip: block number and block
// timestamp. Immutable, so readers always see a matching pair.
type chainTip struct {
	number    uint64
	timestamp uint64
}

// tipWatcher polls the confirmed chain tip in its own goroutine and serves the
// latest observation to the indexing loop.
type tipWatcher struct {
	last atomic.Pointer[chainTip]
}

// watchTip starts a tipWatcher polling every new_block_check_millis until ctx
// is done or the returned stop is called. It is seeded with seed and no
// timestamp, so tip is usable at once and checkLag stays off until the first
// poll lands.
func (ci *Engine) watchTip(ctx context.Context, seed uint64) (*tipWatcher, func()) {
	ctx, stop := context.WithCancel(ctx)

	w := new(tipWatcher)
	w.set(seed, 0)

	go w.run(ctx, ci, ci.pollInterval())

	return w, stop
}

// run polls the tip every interval until ctx is done. Each read goes through
// the RPC retry budget; a read that still fails is logged and retried on the
// next tick. Every observation is written to the last_chain_block state.
func (w *tipWatcher) run(ctx context.Context, ci *Engine, interval time.Duration) {
	for {
		number, timestamp, err := ci.fetchLastBlockIndex(ctx)

		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			logger.Warnf("Cannot read the chain tip: %s", err)
		default:
			w.set(number, timestamp)

			if err := database.UpdateState(ci.db, database.ChainTip, number, timestamp); err != nil {
				logger.Warnf("Could not record the chain tip: %s", err)
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// set stores an observation.
func (w *tipWatcher) set(number, timestamp uint64) {
	w.last.Store(&chainTip{number: number, timestamp: timestamp})
}

// tip is the last observed tip, zero before any observation.
func (w *tipWatcher) tip() uint64 {
	if last := w.last.Load(); last != nil {
		return last.number
	}

	return 0
}

// checkLag returns ErrBehind when the chain time from blockTimestamp to the
// observed tip's timestamp is at least maxLag. A maxLag of zero disables the
// check, as does a tip without a timestamp.
func (w *tipWatcher) checkLag(maxLag time.Duration, block, blockTimestamp uint64) error {
	last := w.last.Load()
	if maxLag <= 0 || last == nil || last.timestamp == 0 || blockTimestamp >= last.timestamp {
		return nil
	}

	lag := time.Duration(last.timestamp-blockTimestamp) * time.Second
	if lag < maxLag {
		return nil
	}

	return boff.Permanent(fmt.Errorf(
		"%w: block=%d, chain_tip=%d, lag_seconds=%d, max_lag_seconds=%d. "+
			"The indexer exits so that its restart catches up in batch mode. "+
			"If this repeats, the RPC node is too slow for continuous indexing: "+
			"use a closer or less loaded node, or raise indexer.max_lag_seconds",
		ErrBehind, block, last.number, int64(lag.Seconds()), int64(maxLag.Seconds()),
	))
}
