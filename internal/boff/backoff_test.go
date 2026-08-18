package boff

import (
	"context"
	"errors"
	"testing"
)

// A permanent error must stop the loop on the first attempt and surface
// unchanged. Startup relies on this: its retry has no maximum elapsed time, so
// anything not marked permanent is retried forever.
func TestPermanentStopsRetryImmediately(t *testing.T) {
	sentinel := errors.New("node does not have the block")
	attempts := 0

	_, err := Retry(context.Background(), func() (struct{}, error) {
		attempts++
		return struct{}{}, Permanent(sentinel)
	}, "permanent")

	if attempts != 1 {
		t.Fatalf("made %d attempts, want 1", attempts)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want the wrapped error unchanged", err)
	}
}

// An ordinary error keeps being retried, which is what keeps a briefly
// unreachable node from being treated as a verdict.
func TestOrdinaryErrorIsRetried(t *testing.T) {
	attempts := 0

	_, err := Retry(context.Background(), func() (struct{}, error) {
		attempts++
		if attempts < 3 {
			return struct{}{}, errors.New("connection refused")
		}
		return struct{}{}, nil
	}, "transient")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("made %d attempts, want 3", attempts)
	}
}
