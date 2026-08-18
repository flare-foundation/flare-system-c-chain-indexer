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

		// Everything else must stay retryable, however much its prose sounds like
		// absence. These read as a missing block to a substring match, and treating
		// an outage as proof a block is gone would end startup on a blip.
		{"service unavailable", errors.New("503 Service Unavailable"), false},
		{"gateway timeout", errors.New("504 Gateway Timeout"), false},
		{"bad gateway", errors.New("502 Bad Gateway"), false},
		{"proxy path not found", errors.New("404 page not found"), false},
		{"method name plus status", errors.New("eth_getBlockByNumber: 503 Service Unavailable"), false},
		{"node phrasing without the sentinel", errors.New("requested block is not available"), false},
		{"connection refused", errors.New("dial tcp 127.0.0.1:9650: connect: connection refused"), false},
		{"timeout", errors.New("context deadline exceeded"), false},
		{"rate limited", errors.New("429 Too Many Requests"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsBlockUnavailable(tc.err); got != tc.want {
				t.Fatalf("IsBlockUnavailable(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}
