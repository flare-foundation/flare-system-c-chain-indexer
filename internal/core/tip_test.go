package core

import (
	"errors"
	"testing"
	"time"
)

func TestTipWatcherSet(t *testing.T) {
	w := new(tipWatcher)

	if tip := w.tip(); tip != 0 {
		t.Errorf("tip before any observation = %d, want 0", tip)
	}

	w.set(100, 1000)
	w.set(101, 1002)
	if tip := w.tip(); tip != 101 {
		t.Errorf("tip = %d, want 101", tip)
	}
}

func TestTipWatcherCheckLag(t *testing.T) {
	const tip, tipTime uint64 = 160, 10000

	w := new(tipWatcher)
	if err := w.checkLag(time.Minute, 100, tipTime-3600); err != nil {
		t.Errorf("no observation yet disables the check, got %v", err)
	}

	w.set(tip, 0)
	if err := w.checkLag(time.Minute, 100, tipTime-3600); err != nil {
		t.Errorf("a seeded tip without a timestamp disables the check, got %v", err)
	}

	w.set(tip, tipTime)
	if err := w.checkLag(0, 100, tipTime-3600); err != nil {
		t.Errorf("max lag 0 disables the check, got %v", err)
	}
	if err := w.checkLag(time.Minute, 100, tipTime-59); err != nil {
		t.Errorf("under the limit is fine, got %v", err)
	}
	if err := w.checkLag(time.Minute, 161, tipTime+2); err != nil {
		t.Errorf("a block past the observed tip is no lag, got %v", err)
	}

	err := w.checkLag(time.Minute, 100, tipTime-60)
	if !errors.Is(err, ErrBehind) {
		t.Fatalf("at the limit is behind, got %v", err)
	}
	want := "indexing fell behind the chain: block=100, chain_tip=160, lag_seconds=60, max_lag_seconds=60. " +
		"The indexer exits so that its restart catches up in batch mode. " +
		"If this repeats, the RPC node is too slow for continuous indexing: " +
		"use a closer or less loaded node, or raise indexer.max_lag_seconds"
	if err.Error() != want {
		t.Errorf("error = %q\nwant    %q", err, want)
	}
}
