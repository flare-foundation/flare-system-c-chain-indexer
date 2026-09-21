package check

import (
	"errors"
	"testing"

	"github.com/cenkalti/backoff/v5"
	"github.com/ethereum/go-ethereum"
)

func TestRangeLimit(t *testing.T) {
	limit, rejected := rangeLimit(errors.New("requested too many blocks from 133216808 to 133217807, maximum is set to 30"))
	if !rejected || limit != 30 {
		t.Errorf("rangeLimit(rejection) = (%d, %t), want (30, true)", limit, rejected)
	}
	if _, rejected := rangeLimit(errors.New("context deadline exceeded")); rejected {
		t.Error("a timeout is not a range rejection")
	}
	if _, rejected := rangeLimit(nil); rejected {
		t.Error("nil is not a range rejection")
	}
}

func TestBlockError(t *testing.T) {
	var permanent *backoff.PermanentError

	if err := blockError(ethereum.NotFound, "block %d", 7); !errors.As(err, &permanent) {
		t.Error("a missing block should end startup")
	}
	if err := blockError(errors.New("503 Service Unavailable"), "block %d", 7); errors.As(err, &permanent) {
		t.Error("a transient failure should stay retryable")
	}
}
