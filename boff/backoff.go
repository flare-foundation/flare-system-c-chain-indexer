// This file contains helper functions for retrying operations with exponential backoff.
// The idea is to avoid repetition with common retry boilerplate code.
package boff

import (
	"context"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/logger"
	"time"

	"github.com/cenkalti/backoff/v5"
)

func RetryWithMaxElapsed[T any](ctx context.Context, operation func() (T, error), name string) (T, error) {
	return backoff.Retry(
		ctx,
		operation,
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
		backoff.WithMaxElapsedTime(config.BackoffMaxElapsedTime),
		backoff.WithNotify(
			func(err error, d time.Duration) {
				logger.Debug("%s error: %s - retrying after %v", name, err, d)
			},
		),
	)
}

func Retry[T any](ctx context.Context, operation func() (T, error), name string) (T, error) {
	return backoff.Retry(
		ctx,
		operation,
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
		backoff.WithNotify(
			func(err error, d time.Duration) {
				logger.Debug("%s error: %s - retrying after %v", name, err, d)
			},
		),
	)
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
