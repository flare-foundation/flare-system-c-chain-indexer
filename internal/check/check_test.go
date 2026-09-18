package check

import (
	"errors"
	"testing"
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
