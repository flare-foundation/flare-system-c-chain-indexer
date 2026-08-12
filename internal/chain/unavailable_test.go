package chain

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ava-labs/coreth/interfaces"
	"github.com/ethereum/go-ethereum"
)

func TestIsBlockUnavailable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"no error", nil, false},
		// A null RPC result is how both clients report a block they do not have.
		{"coreth sentinel", interfaces.NotFound, true},
		{"go-ethereum sentinel", ethereum.NotFound, true},
		{"wrapped sentinel", fmt.Errorf("fetchBlockHeader: %w", interfaces.NotFound), true},
		// Nodes that answer with an explicit JSON-RPC error instead.
		{"explicit message", errors.New("requested block is not available: node was state synced"), true},
		{"pruned message", errors.New("block 123 has been pruned"), true},
		{"missing block message", errors.New("block not found"), true},
		// Transient failures must stay retryable.
		{"connection refused", errors.New("dial tcp 127.0.0.1:9650: connect: connection refused"), false},
		{"timeout", errors.New("context deadline exceeded"), false},
		{"rate limited", errors.New("429 Too Many Requests"), false},
		{"server error", errors.New("502 Bad Gateway"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsBlockUnavailable(tc.err); got != tc.want {
				t.Fatalf("IsBlockUnavailable(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}
