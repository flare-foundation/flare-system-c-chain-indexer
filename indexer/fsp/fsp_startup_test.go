package fsp

import (
	"testing"

	"flare-ftso-indexer/database"
)

func TestResolveCatchupBlock(t *testing.T) {
	plan := &fspStartupTargets{
		fullStartBlock:  100,
		eventStartBlock: 80,
	}

	testCases := []struct {
		name               string
		firstEventRange    *database.State
		lastDB             *database.State
		firstFullIndex     *database.State
		wantCatchupFrom    uint64
		wantBackfillEvents bool
	}{
		{
			name:               "missing full index states triggers full catchup and event backfill",
			firstEventRange:    nil,
			lastDB:             nil,
			firstFullIndex:     nil,
			wantCatchupFrom:    100,
			wantBackfillEvents: true,
		},
		{
			name:               "missing full index states still skips event backfill when event range state covers keep window",
			firstEventRange:    &database.State{Index: 70},
			lastDB:             nil,
			firstFullIndex:     nil,
			wantCatchupFrom:    100,
			wantBackfillEvents: false,
		},
		{
			name: "invalid full index range triggers full catchup but can skip event backfill",
			firstEventRange: &database.State{
				Index: 80,
			},
			lastDB: &database.State{
				Index: 120,
			},
			firstFullIndex: &database.State{
				Index: 150,
			},
			wantCatchupFrom:    100,
			wantBackfillEvents: false,
		},
		{
			name: "first full index above full catchup start triggers full catchup but can skip event backfill",
			firstEventRange: &database.State{
				Index: 80,
			},
			lastDB: &database.State{
				Index: 140,
			},
			firstFullIndex: &database.State{
				Index: 110,
			},
			wantCatchupFrom:    100,
			wantBackfillEvents: false,
		},
		{
			name: "last full index below full catchup start triggers full catchup but can skip event backfill",
			firstEventRange: &database.State{
				Index: 80,
			},
			lastDB: &database.State{
				Index: 99,
			},
			firstFullIndex: &database.State{
				Index: 90,
			},
			wantCatchupFrom:    100,
			wantBackfillEvents: false,
		},
		{
			name:            "valid full range with missing event range state backfills event ranges",
			firstEventRange: nil,
			lastDB: &database.State{
				Index: 140,
			},
			firstFullIndex: &database.State{
				Index: 100,
			},
			wantCatchupFrom:    141,
			wantBackfillEvents: true,
		},
		{
			name: "valid full range with stale event range state backfills event ranges",
			firstEventRange: &database.State{
				Index: 85,
			},
			lastDB: &database.State{
				Index: 140,
			},
			firstFullIndex: &database.State{
				Index: 100,
			},
			wantCatchupFrom:    141,
			wantBackfillEvents: true,
		},
		{
			name: "valid full range with event range coverage skips event range backfill",
			firstEventRange: &database.State{
				Index: 70,
			},
			lastDB: &database.State{
				Index: 140,
			},
			firstFullIndex: &database.State{
				Index: 100,
			},
			wantCatchupFrom:    141,
			wantBackfillEvents: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			catchupFrom, backfillEvents := resolveCatchupBlock(
				tc.firstEventRange,
				tc.firstFullIndex,
				tc.lastDB,
				plan,
			)

			if catchupFrom != tc.wantCatchupFrom {
				t.Fatalf("unexpected catchup start: got=%d want=%d", catchupFrom, tc.wantCatchupFrom)
			}
			if backfillEvents != tc.wantBackfillEvents {
				t.Fatalf("unexpected event backfill decision: got=%t want=%t", backfillEvents, tc.wantBackfillEvents)
			}
		})
	}
}
