package database

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The DB-backed tests reuse the HISTORY_DROP_TEST_DSN-gated scratch DB harness
// from history_drop_test.go (setupScratchDB / seedState / stateRow).

// Regression: a retry must resume from the persisted LastIndexed high-water
// mark, not the startup-era historyLastIndex the retry loop used to reuse.
func TestResumeIndex(t *testing.T) {
	tests := []struct {
		name             string
		historyLastIndex uint64
		lastIndexed      State
		want             uint64
	}{
		{
			name:             "no persisted progress resumes after history tip",
			historyLastIndex: 1000,
			lastIndexed:      State{}, // unset
			want:             1001,
		},
		{
			name:             "retry resumes after persisted progress, not startup tip",
			historyLastIndex: 1000,
			lastIndexed:      State{Index: 5000},
			want:             5001,
		},
		{
			name:             "state level with history tip resumes at next block",
			historyLastIndex: 1000,
			lastIndexed:      State{Index: 1000},
			want:             1001,
		},
		{
			name:             "stale state below history tip never rewinds",
			historyLastIndex: 1000,
			lastIndexed:      State{Index: 800},
			want:             1001,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ResumeIndex(tc.historyLastIndex, tc.lastIndexed))
		})
	}
}

// Same resume logic against a real states table; skips without a test DSN.
func TestContinuousStartIndex(t *testing.T) {
	dsn := os.Getenv(testDSNEnv)
	if dsn == "" {
		t.Skipf("%s not set", testDSNEnv)
	}

	t.Run("resumes from persisted LastIndexed, not startup tip", func(t *testing.T) {
		db := setupScratchDB(t, dsn)
		seedState(t, db, LastIndexed, 5000)

		got, err := ContinuousStartIndex(db, 1000)
		require.NoError(t, err)
		require.Equal(t, uint64(5001), got)
	})

	t.Run("cold start with no state resumes after history tip", func(t *testing.T) {
		db := setupScratchDB(t, dsn)

		got, err := ContinuousStartIndex(db, 1000)
		require.NoError(t, err)
		require.Equal(t, uint64(1001), got)
	})
}

func TestLowerStateFloor(t *testing.T) {
	dsn := os.Getenv(testDSNEnv)
	if dsn == "" {
		t.Skipf("%s not set", testDSNEnv)
	}
	db := setupScratchDB(t, dsn)

	steps := []struct {
		desc  string
		index uint64
		want  uint64
	}{
		{"creates the row", 1000, 1000},
		{"never raises", 2000, 1000},
		{"lowers toward a re-indexed start", 600, 600},
		{"equal value is a no-op", 600, 600},
		{"still never raises", 900, 600},
	}
	for _, s := range steps {
		require.NoError(t, LowerStateFloor(db, BlockFloor, s.index, s.index), s.desc)
		require.Equal(t, s.want, stateRow(t, db, BlockFloor).Index, s.desc)
	}
}

func TestWriteCoverageStates(t *testing.T) {
	dsn := os.Getenv(testDSNEnv)
	if dsn == "" {
		t.Skipf("%s not set", testDSNEnv)
	}
	db := setupScratchDB(t, dsn)

	// Existing coverage [1000, 2000]; history_epochs is raised so catchup
	// re-indexes the first lower batch [500, 599]. The floor lowers and
	// LastIndexed regresses together.
	seedState(t, db, BlockFloor, 1000)
	seedState(t, db, LastIndexed, 2000)
	require.NoError(t, WriteCoverageStates(db, 599, 599, 500, 500))
	require.Equal(t, uint64(500), stateRow(t, db, BlockFloor).Index, "floor lowers to the re-indexed batch start")
	require.Equal(t, uint64(599), stateRow(t, db, LastIndexed).Index, "LastIndexed regresses with the floor, never lags above it")

	// A later batch advances LastIndexed; the floor must not be raised.
	require.NoError(t, WriteCoverageStates(db, 3000, 3000, 700, 700))
	require.Equal(t, uint64(500), stateRow(t, db, BlockFloor).Index, "floor is never raised")
	require.Equal(t, uint64(3000), stateRow(t, db, LastIndexed).Index, "LastIndexed advances")
}

// A crash between the two writes must roll back entirely: it must never leave a
// lowered floor paired with a stale, higher LastIndexed, which the FSP resume
// guard would read as contiguous and skip the unfilled blocks between them.
func TestCoverageStatePairRollsBack(t *testing.T) {
	dsn := os.Getenv(testDSNEnv)
	if dsn == "" {
		t.Skipf("%s not set", testDSNEnv)
	}
	db := setupScratchDB(t, dsn)

	seedState(t, db, BlockFloor, 1000)
	seedState(t, db, LastIndexed, 2000)

	boom := errors.New("boom")
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := UpdateState(tx, LastIndexed, 599, 599); err != nil {
			return err
		}
		return boom // fail before the floor is lowered
	})
	require.ErrorIs(t, err, boom)
	require.Equal(t, uint64(2000), stateRow(t, db, LastIndexed).Index, "partial state write must roll back")
	require.Equal(t, uint64(1000), stateRow(t, db, BlockFloor).Index, "floor untouched")
}
