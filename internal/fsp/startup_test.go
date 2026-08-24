package fsp

import (
	"errors"
	"testing"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/database"

	"github.com/cenkalti/backoff/v5"
	"github.com/ethereum/go-ethereum"
)

var errTransient = errors.New("context deadline exceeded")

func states(rows map[database.StateName][2]uint64) coverage {
	out := make(coverage, len(rows))
	for name, row := range rows {
		out[name] = database.State{Index: row[0], BlockTimestamp: row[1]}
	}

	return out
}

func TestCoverageBlockFloor(t *testing.T) {
	const lookback = uint64(5000)

	tests := []struct {
		name      string
		rows      map[database.StateName][2]uint64
		wantFloor uint64
		want      bool
	}{
		{
			name: "empty database",
			rows: nil,
		},
		{
			name: "floor at the lookback target",
			rows: map[database.StateName][2]uint64{
				database.BlockFloor:  {2000, lookback},
				database.LastIndexed: {9000, 9999},
			},
			wantFloor: 2000,
			want:      true,
		},
		{
			name: "floor newer than the lookback target",
			rows: map[database.StateName][2]uint64{
				database.BlockFloor:  {2000, lookback + 1},
				database.LastIndexed: {9000, 9999},
			},
		},
		{
			name: "floor without a timestamp",
			rows: map[database.StateName][2]uint64{
				database.BlockFloor:  {2000, 0},
				database.LastIndexed: {9000, 9999},
			},
		},
		{
			name: "last indexed below the floor",
			rows: map[database.StateName][2]uint64{
				database.BlockFloor:  {2000, lookback},
				database.LastIndexed: {1999, 4000},
			},
		},
		{
			name: "floor without last indexed",
			rows: map[database.StateName][2]uint64{
				database.BlockFloor: {2000, lookback},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			floor, indexed := states(test.rows).blockFloor(lookback)

			if indexed != test.want {
				t.Fatalf("indexed = %t, want %t", indexed, test.want)
			}
			if indexed && floor != test.wantFloor {
				t.Errorf("floor = %d, want %d", floor, test.wantFloor)
			}
		})
	}
}

func TestCoverageEventsIndexed(t *testing.T) {
	const anchor = uint64(1000)

	tests := []struct {
		name string
		rows map[database.StateName][2]uint64
		want bool
	}{
		{name: "empty database", rows: nil},
		{
			name: "log floor at the anchor",
			rows: map[database.StateName][2]uint64{database.LogFloor: {anchor, 1}},
			want: true,
		},
		{
			name: "log floor above the anchor",
			rows: map[database.StateName][2]uint64{database.LogFloor: {anchor + 1, 1}},
		},
		{
			// A fully indexed range proves coverage only for the collectors that
			// filled it, so it is not event coverage on its own.
			name: "indexed range covers the anchor without a log floor",
			rows: map[database.StateName][2]uint64{
				database.BlockFloor:  {anchor - 1, 1},
				database.LastIndexed: {anchor + 1, 1},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := states(test.rows).eventsIndexed(anchor); got != test.want {
				t.Errorf("eventsIndexed = %t, want %t", got, test.want)
			}
		})
	}
}

func TestProbeError(t *testing.T) {
	var permanent *backoff.PermanentError

	if err := probeError(ethereum.NotFound, "block %d", 7); !errors.As(err, &permanent) {
		t.Error("a missing block should end startup")
	}
	if err := probeError(errTransient, "block %d", 7); errors.As(err, &permanent) {
		t.Error("a transient failure should stay retryable")
	}
}

func TestCoverageCatchupFrom(t *testing.T) {
	const fullStart = uint64(2000)

	tests := []struct {
		name string
		rows map[database.StateName][2]uint64
		want uint64
	}{
		{name: "empty database starts at the window floor", rows: nil, want: fullStart},
		{
			name: "indexed range covering the floor resumes after it",
			rows: map[database.StateName][2]uint64{
				database.BlockFloor:  {1000, 1},
				database.LastIndexed: {9000, 1},
			},
			want: 9001,
		},
		{
			name: "indexed range above the floor restarts at it",
			rows: map[database.StateName][2]uint64{
				database.BlockFloor:  {fullStart + 1, 1},
				database.LastIndexed: {9000, 1},
			},
			want: fullStart,
		},
		{
			name: "indexed range ending below the floor restarts at it",
			rows: map[database.StateName][2]uint64{
				database.BlockFloor:  {1000, 1},
				database.LastIndexed: {1500, 1},
			},
			want: fullStart,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := states(test.rows).catchupFrom(fullStart); got != test.want {
				t.Errorf("catchupFrom = %d, want %d", got, test.want)
			}
		})
	}
}
