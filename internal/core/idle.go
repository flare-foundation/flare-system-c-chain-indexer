package core

import (
	"time"

	"github.com/flare-foundation/go-flare-common/pkg/logger"
)

// idleWarner warns when the chain tip has not advanced for
// indexer.no_new_blocks_delay_warning seconds, and repeats the warning at that
// interval while it stays still. A delay of zero disables it.
type idleWarner struct {
	delay       time.Duration
	lastBlock   time.Time
	lastWarning time.Time
}

func newIdleWarner(delaySeconds float64) *idleWarner {
	now := time.Now()

	return &idleWarner{
		delay:       time.Duration(delaySeconds * float64(time.Second)),
		lastBlock:   now,
		lastWarning: now,
	}
}

func (w *idleWarner) blockIndexed() {
	now := time.Now()
	w.lastBlock, w.lastWarning = now, now
}

func (w *idleWarner) warnIfDue(tip uint64) {
	if w.delay <= 0 || time.Since(w.lastWarning) <= w.delay {
		return
	}

	logger.Warnf(
		"Chain tip has not advanced: chain_tip=%d, idle_seconds=%d. "+
			"The node is not producing new confirmed blocks: check that it is synced and healthy",
		tip, int64(time.Since(w.lastBlock).Seconds()),
	)
	w.lastWarning = time.Now()
}
