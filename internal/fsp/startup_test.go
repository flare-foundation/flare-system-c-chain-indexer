package fsp

import (
	"context"
	"strings"
	"testing"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/boff"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestEnsureBlockAvailablePassesWhenTheNodeHasTheBlock(t *testing.T) {
	var asked uint64
	calls := 0
	probe := func(_ context.Context, block uint64) error {
		asked = block
		calls++
		return nil
	}

	require.NoError(t, ensureBlockAvailable(context.Background(), probe, 66529154, 421, 0))
	require.Equal(t, uint64(66529154), asked, "the anchor block itself must be probed")
	require.Equal(t, 1, calls)
}

// A state synced node discarded the blocks before its sync point. The error has
// to name the block and why it is needed, since the operator's only options are
// a different node or a smaller window.
func TestEnsureBlockAvailableReportsAMissingBlock(t *testing.T) {
	calls := 0
	probe := func(context.Context, uint64) error {
		calls++
		return errors.New("not found")
	}

	err := ensureBlockAvailable(context.Background(), probe, 66529154, 421, 3)
	require.Error(t, err)
	for _, want := range []string{"66529154", "reward epoch 421", "history_epochs=3", "not found"} {
		require.Contains(t, err.Error(), want)
	}
	require.Equal(t, 1, calls, "a missing block is not transient and must not be retried")
}

// A node that is briefly unreachable is not a verdict on the block, so the error
// must stay retryable: the caller's loop has to keep asking until the node either
// serves the block or says it does not have it.
func TestEnsureBlockAvailableRetriesTransientFailures(t *testing.T) {
	probes := 0
	probe := func(context.Context, uint64) error {
		probes++
		if probes < 3 {
			return errors.New("dial tcp 127.0.0.1:9650: connect: connection refused")
		}
		return nil
	}

	_, err := boff.Retry(context.Background(), func() (struct{}, error) {
		return struct{}{}, ensureBlockAvailable(context.Background(), probe, 66529154, 421, 0)
	}, "ensureBlockAvailable")

	require.NoError(t, err, "a transient failure must not be fatal")
	require.Equal(t, 3, probes, "the node must be asked again until it answers")
}

// ...and the transient error must not claim the block is missing, which would
// send an operator looking for the wrong problem.
func TestEnsureBlockAvailableDoesNotBlameTheBlockOnTransientFailures(t *testing.T) {
	probe := func(context.Context, uint64) error {
		return errors.New("context deadline exceeded")
	}

	err := ensureBlockAvailable(context.Background(), probe, 66529154, 421, 0)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "cannot serve block")
	require.Contains(t, err.Error(), "probe block 66529154")
}

// The retry loop around startup has no maximum elapsed time, so a missing block
// must be permanent or the indexer retries it forever instead of exiting.
func TestEnsureBlockAvailableDoesNotRetryForever(t *testing.T) {
	probes := 0
	probe := func(context.Context, uint64) error {
		probes++
		return errors.New("not found")
	}

	attempts := 0
	_, err := boff.Retry(context.Background(), func() (struct{}, error) {
		attempts++
		return struct{}{}, ensureBlockAvailable(context.Background(), probe, 66529154, 421, 0)
	}, "ensureBlockAvailable")

	require.Error(t, err)
	require.Equal(t, 1, attempts, "a missing block must stop the retry loop on the first attempt")
	require.Equal(t, 1, probes, "and the node must be asked exactly once")
	require.False(t, strings.Contains(err.Error(), "PermanentError"), "the caller should see the underlying error")
}
