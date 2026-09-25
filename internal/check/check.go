// Package check holds the startup checks: what the indexer verifies about its
// dependencies before it starts work.
package check

import (
	"context"
	"fmt"
	"math/big"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/boff"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
)

// rangeRejection is how the node words a rejected eth_getLogs block range.
const rangeRejection = "requested too many blocks from %d to %d, maximum is set to %d"

// LogRange issues one eth_getLogs over logRange blocks ending at the latest
// block, filtered on a topic no log has. A node that rejects the range ends
// startup at once with the limit to set. Any other failure keeps the RPC retry
// budget, since the node reports a server-side timeout as a JSON-RPC error too.
func LogRange(ctx context.Context, client *chain.Client, logRange uint64) error {
	_, err := boff.RetryWithMaxElapsed(ctx, func() ([]types.Log, error) {
		callCtx, cancel := context.WithTimeout(ctx, 2*config.RPCTimeout)
		defer cancel()

		header, err := client.HeaderByNumber(callCtx, nil)
		if err != nil {
			return nil, err
		}

		tip := header.Number.Uint64()
		logs, err := client.FilterLogs(callCtx, ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(tip - min(tip, logRange-1)),
			ToBlock:   new(big.Int).SetUint64(tip),
			Topics:    [][]common.Hash{{{}}},
		})
		if _, rejected := rangeLimit(err); rejected {
			return nil, boff.Permanent(err)
		}

		return logs, err
	}, "check.LogRange")
	if err == nil {
		return nil
	}

	if limit, rejected := rangeLimit(err); rejected {
		return fmt.Errorf("eth_getLogs over indexer.log_range=%d blocks failed: %w. Set indexer.log_range to at most %d", logRange, err, limit)
	}

	return fmt.Errorf("eth_getLogs over indexer.log_range=%d blocks failed: %w", logRange, err)
}

// rangeLimit reads the node's block limit out of a rangeRejection error.
func rangeLimit(err error) (limit uint64, rejected bool) {
	if err == nil {
		return 0, false
	}

	var from, to uint64
	_, scanErr := fmt.Sscanf(err.Error(), rangeRejection, &from, &to, &limit)

	return limit, scanErr == nil
}

// blockError ends startup when the block is definitively missing; anything else
// — a timeout, a 503, a rate limit — stays retryable. The re-check is load
// bearing: backoff.Retry unwraps the loop's PermanentError before returning it.
func blockError(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}

	wrapped := errors.Wrapf(err, format, args...)
	if chain.IsBlockUnavailable(err) {
		return boff.Permanent(wrapped)
	}

	return wrapped
}

// Block checks that the node serves the block, describing a failure with
// format. "No such block" is not retried: it will not become available.
func Block(
	ctx context.Context, client *chain.Client, block uint64, format string, args ...any,
) error {
	_, err := boff.RetryWithMaxElapsed(ctx, func() (*types.Header, error) {
		callCtx, cancel := context.WithTimeout(ctx, config.RPCTimeout)
		defer cancel()

		header, err := client.HeaderByNumber(callCtx, new(big.Int).SetUint64(block))
		if chain.IsBlockUnavailable(err) {
			return nil, boff.Permanent(err)
		}

		return header, err
	}, "probeBlock")

	return blockError(err, format, args...)
}
