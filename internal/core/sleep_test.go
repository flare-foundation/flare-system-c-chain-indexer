package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSleepCtx(t *testing.T) {
	start := time.Now()
	if err := sleepCtx(context.Background(), 10*time.Millisecond); err != nil {
		t.Errorf("sleepCtx = %v, want nil after the duration", err)
	}
	if time.Since(start) < 10*time.Millisecond {
		t.Error("sleepCtx returned before the duration elapsed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("sleepCtx on a cancelled context = %v, want context.Canceled", err)
	}
}
