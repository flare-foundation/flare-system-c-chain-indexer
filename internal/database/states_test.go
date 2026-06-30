package database

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

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
