package database

import (
	"time"

	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	LastChainIndexState             string = "last_chain_block"
	LastDatabaseIndexState          string = "last_database_block"
	FirstDatabaseIndexState         string = "first_database_block"
	FirstDatabaseFSPEventIndexState string = "first_database_fsp_event_block"
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
func UpdateState(db *gorm.DB, name string, index, blockTimestamp uint64) error {
	state := &State{
		Name:           name,
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
func GetState(db *gorm.DB, name string) (State, error) {
	var state State
	err := db.Where(&State{Name: name}).First(&state).Error
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
func GetStates(db *gorm.DB, names ...string) (map[string]State, error) {
	var rows []State
	if err := db.Where("name IN ?", names).Find(&rows).Error; err != nil {
		return nil, err
	}
	states := make(map[string]State, len(rows))
	for _, state := range rows {
		states[state.Name] = state
	}
	return states, nil
}

// CreateStateIfMissing writes the state row only when it does not exist yet.
// The insert is atomic (ON CONFLICT DO NOTHING on the unique name), so it can
// never overwrite a concurrent update of an existing row.
func CreateStateIfMissing(db *gorm.DB, name string, index, blockTimestamp uint64) error {
	state := &State{
		Name:           name,
		Index:          index,
		BlockTimestamp: blockTimestamp,
		Updated:        time.Now(),
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoNothing: true,
	}).Create(state).Error
}
