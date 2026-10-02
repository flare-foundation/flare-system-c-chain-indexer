// This file contains helper functions for retrying operations with exponential backoff.
// The idea is to avoid repetition with common retry boilerplate code.
package boff

import (
	"context"
	"time"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"

	"github.com/cenkalti/backoff/v5"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
)

func RetryWithMaxElapsed[T any](ctx context.Context, operation func() (T, error), name string) (T, error) {
	return retry(ctx, operation, name, config.BackoffMaxElapsedTime)
}

func Retry[T any](ctx context.Context, operation func() (T, error), name string) (T, error) {
	return retry(ctx, operation, name, 0) // 0 means no max elapsed time
}

// Permanent marks an error as not worth retrying. The enclosing Retry stops
// immediately and returns the wrapped error unchanged, which is what turns a
// misconfiguration into a clean exit rather than an endless retry loop.
func Permanent(err error) error {
	return backoff.Permanent(err)
}

func RetryNoReturn(ctx context.Context, operation func() error, name string) error {
	_, err := Retry(
		ctx,
		func() (struct{}, error) {
			return struct{}{}, operation()
		},
		name,
	)

	return err
}

// retry runs operation with exponential backoff. Each failed attempt is a
// handled failure and is logged at WARN as it is retried. An exhausted budget
// is not logged here: the caller decides what the failure means and logs it
// once, at that layer.
func retry[T any](ctx context.Context, operation func() (T, error), name string, maxElapsedTime time.Duration) (T, error) {
	return backoff.Retry(
		ctx,
		operation,
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
		backoff.WithMaxElapsedTime(maxElapsedTime),
		backoff.WithNotify(
			func(err error, d time.Duration) {
				logger.Warnw("Retrying a failed call", "op", name, "delay_ms", d.Milliseconds(), "error", err)
			},
		),
	)
}
