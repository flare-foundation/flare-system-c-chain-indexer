package database

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression: a retry must resume from the latest persisted block (high-water
// mark), not the startup-era historyLastIndex the retry loop used to reuse.
func TestResumeIndex(t *testing.T) {
	tests := []struct {
		name             string
		historyLastIndex uint64
		latestIndexedBlk uint64
		want             uint64
	}{
		{"no persisted progress resumes after history tip", 1000, 0, 1001},
		{"retry resumes after persisted progress, not startup tip", 1000, 5000, 5001},
		{"progress level with history tip resumes at next block", 1000, 1000, 1001},
		{"stale progress below history tip never rewinds", 1000, 800, 1001},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ResumeIndex(tc.historyLastIndex, tc.latestIndexedBlk))
		})
	}
}
