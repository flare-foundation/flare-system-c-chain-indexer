package database

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Exercises dropHistoryBelow against a real MySQL instance. Set
// HISTORY_DROP_TEST_DSN to enable, e.g. "root:root@tcp(127.0.0.1:3306)/";
// a scratch database history_drop_test is created and dropped per scenario.
const testDSNEnv = "HISTORY_DROP_TEST_DSN"

func TestDropHistoryFloors(t *testing.T) {
	dsn := os.Getenv(testDSNEnv)
	if dsn == "" {
		t.Skipf("%s not set", testDSNEnv)
	}

	// FSP mode: log-only backfill region (logs at 1000, 1500, 1900) below the
	// full region (blocks 2000..2009, one log each); floors as fsp.IndexStartup
	// leaves them. Full mode: full region only, no FSP floor row.
	// Timestamps equal block numbers for readability.
	tests := []struct {
		name      string
		fspMode   bool
		boundary  uint64
		wantFirst uint64
		wantFsp   uint64
	}{
		{"fsp: boundary below floor leaves floors alone", true, 500, 2000, 1000},
		{"fsp: boundary in log-only region advances floor to oldest log", true, 1600, 2000, 1900},
		{"fsp: boundary past log-only region converges floors", true, 2005, 2005, 2005},
		{"full: floors track first block together", false, 2003, 2003, 2003},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := setupScratchDB(t, dsn)

			if tc.fspMode {
				for _, n := range []uint64{1000, 1500, 1900} {
					createLog(t, db, n)
				}
			}
			for n := uint64(2000); n < 2010; n++ {
				require.NoError(t, db.Create(&Block{Number: n, Timestamp: n, Hash: fmt.Sprintf("block-%d", n)}).Error)
				createLog(t, db, n)
			}
			seedState(t, db, FirstDatabaseIndexState, 2000)
			seedState(t, db, LastDatabaseIndexState, 2009)
			if tc.fspMode {
				seedState(t, db, FirstDatabaseFSPEventIndexState, 1000)
			}
			require.NoError(t, dropHistoryBelow(context.Background(), db, tc.boundary))

			require.Equal(t, tc.wantFirst, stateRow(t, db, FirstDatabaseIndexState).BlockTimestamp)
			require.Equal(t, tc.wantFsp, stateRow(t, db, FirstDatabaseFSPEventIndexState).BlockTimestamp)

			var below int64
			require.NoError(t, db.Model(&Log{}).Where("timestamp < ?", tc.boundary).Count(&below).Error)
			require.Zero(t, below, "logs below the boundary must be deleted")
		})
	}
}

func setupScratchDB(t *testing.T, dsn string) *gorm.DB {
	admin, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Discard})
	require.NoError(t, err)
	require.NoError(t, admin.Exec("DROP DATABASE IF EXISTS history_drop_test").Error)
	require.NoError(t, admin.Exec("CREATE DATABASE history_drop_test").Error)
	t.Cleanup(func() {
		require.NoError(t, admin.Exec("DROP DATABASE IF EXISTS history_drop_test").Error)
	})

	db, err := gorm.Open(mysql.Open(dsn+"history_drop_test?parseTime=true"), &gorm.Config{Logger: gormlogger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(entities...))
	return db
}

func createLog(t *testing.T, db *gorm.DB, n uint64) {
	require.NoError(t, db.Create(&Log{
		BlockNumber: n, Timestamp: n, TransactionHash: fmt.Sprintf("log-%d", n),
	}).Error)
}

func seedState(t *testing.T, db *gorm.DB, name string, at uint64) {
	require.NoError(t, db.Create(&State{Name: name, Index: at, BlockTimestamp: at, Updated: time.Now()}).Error)
}

func stateRow(t *testing.T, db *gorm.DB, name string) State {
	var s State
	require.NoError(t, db.Where(&State{Name: name}).First(&s).Error)
	return s
}
