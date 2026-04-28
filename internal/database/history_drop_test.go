package database

import (
	"testing"
	"time"
)

func TestValidateNearestDBBlockTimestamp(t *testing.T) {
	t.Run("uses exact timestamp", func(t *testing.T) {
		useBlock, err := validateNearestDBBlockTimestamp(1000, 1000)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !useBlock {
			t.Fatal("expected DB block to be used")
		}
	})

	t.Run("uses nearby newer timestamp", func(t *testing.T) {
		useBlock, err := validateNearestDBBlockTimestamp(1000+uint64(30*time.Second/time.Second), 1000)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !useBlock {
			t.Fatal("expected DB block to be used")
		}
	})

	t.Run("falls back when DB window is much newer", func(t *testing.T) {
		useBlock, err := validateNearestDBBlockTimestamp(1000+uint64((2*time.Minute)/time.Second), 1000)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if useBlock {
			t.Fatal("expected fallback to RPC search")
		}
	})

	t.Run("errors when DB block is older than requested", func(t *testing.T) {
		useBlock, err := validateNearestDBBlockTimestamp(999, 1000)
		if err == nil {
			t.Fatal("expected error")
		}
		if useBlock {
			t.Fatal("did not expect DB block to be used")
		}
	})
}
