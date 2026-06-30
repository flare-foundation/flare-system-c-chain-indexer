package database

import (
	"time"

	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// StateName identifies a row in the states table. The string values are a
// cross-repo contract (consumers read them by name) — rename identifiers
// freely, never the values without a coordinated migration.
type StateName string

const (
	// ChainTip is the latest confirmed block observed on chain (an
	// observation, not a coverage claim).
	ChainTip StateName = "last_chain_block"
	// LastIndexed is the top of the fully indexed range.
	LastIndexed StateName = "last_database_block"
	// BlockFloor is the full-coverage floor: all blocks, transactions and
	// logs from this block on are indexed.
	BlockFloor StateName = "first_database_block"
	// LogFloor is the log-coverage floor: all collected logs from this block
	// on are present (FSP mode backfills logs deeper than blocks).
	LogFloor StateName = "first_database_log_block"
)

// States capture the state of the DB, giving guarantees about which blocks and
// logs are indexed. The DB rows are the single source of truth: there is no
// in-memory cache, so writes are safe inside transactions and out-of-band
// changes to the states table are picked up by the next reader. Readers
// receive State by value — a copy that cannot alias or mutate anything shared;
// missing rows yield the zero State.

func IsSet(state State) bool {
	return state.Index != 0
}

// UpdateState upserts the state row with the given name.
func UpdateState(db *gorm.DB, name StateName, index, blockTimestamp uint64) error {
	state := &State{
		Name:           string(name),
		Index:          index,
		BlockTimestamp: blockTimestamp,
		Updated:        time.Now(),
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"index", "block_timestamp", "updated"}),
	}).Create(state).Error
}

// GetState returns the state row with the given name by value, or the zero
// State (IsSet == false) if it does not exist.
func GetState(db *gorm.DB, name StateName) (State, error) {
	var state State
	err := db.Where(&State{Name: string(name)}).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	return state, nil
}

// GetStates returns the named state rows keyed by name; a missing row reads
// as the zero State (IsSet == false) when the map is indexed.
func GetStates(db *gorm.DB, names ...StateName) (map[StateName]State, error) {
	var rows []State
	if err := db.Where("name IN ?", names).Find(&rows).Error; err != nil {
		return nil, err
	}
	states := make(map[StateName]State, len(rows))
	for _, state := range rows {
		states[StateName(state.Name)] = state
	}
	return states, nil
}

// CreateStateIfMissing writes the state row only when it does not exist yet.
// The insert is atomic (ON CONFLICT DO NOTHING on the unique name), so it can
// never overwrite a concurrent update of an existing row.
func CreateStateIfMissing(db *gorm.DB, name StateName, index, blockTimestamp uint64) error {
	state := &State{
		Name:           string(name),
		Index:          index,
		BlockTimestamp: blockTimestamp,
		Updated:        time.Now(),
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoNothing: true,
	}).Create(state).Error
}

// ResumeIndex returns the block continuous indexing should resume from: one
// past the higher of historyLastIndex (the cold-start floor) and the persisted
// LastIndexed high-water mark.
func ResumeIndex(historyLastIndex uint64, lastIndexed State) uint64 {
	resumeAfter := historyLastIndex
	if IsSet(lastIndexed) && lastIndexed.Index > resumeAfter {
		resumeAfter = lastIndexed.Index
	}
	return resumeAfter + 1
}

// ContinuousStartIndex resolves ResumeIndex against the persisted LastIndexed
// state. Call it inside the retry loop so each attempt re-reads real progress.
func ContinuousStartIndex(db *gorm.DB, historyLastIndex uint64) (uint64, error) {
	lastIndexed, err := GetState(db, LastIndexed)
	if err != nil {
		return 0, errors.Wrap(err, "GetState(LastIndexed)")
	}
	return ResumeIndex(historyLastIndex, lastIndexed), nil
}
