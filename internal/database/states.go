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
// changes to the states table are picked up by the next reader.

func IsSet(state *State) bool {
	return state != nil && state.Index != 0
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

// GetState returns the state row with the given name, or nil if it does not exist.
func GetState(db *gorm.DB, name string) (*State, error) {
	var state State
	err := db.Where(&State{Name: name}).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// GetStates returns the named state rows keyed by name; missing rows are
// simply absent from the map.
func GetStates(db *gorm.DB, names ...string) (map[string]*State, error) {
	var rows []*State
	if err := db.Where("name IN ?", names).Find(&rows).Error; err != nil {
		return nil, err
	}
	states := make(map[string]*State, len(rows))
	for _, state := range rows {
		states[state.Name] = state
	}
	return states, nil
}

// UpdateStatesAtStart records the indexing start: the first-database floor is
// written only when it does not exist yet (atomic insert-if-missing, so a
// concurrent history-drop raise can never be overwritten), the chain tip
// unconditionally.
func UpdateStatesAtStart(
	db *gorm.DB, startIndex, startBlockTimestamp, lastChainIndex, lastBlockTimestamp uint64,
) error {
	first := &State{
		Name:           FirstDatabaseIndexState,
		Index:          startIndex,
		BlockTimestamp: startBlockTimestamp,
		Updated:        time.Now(),
	}
	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoNothing: true,
	}).Create(first).Error
	if err != nil {
		return errors.Wrap(err, "create-if-missing FirstDatabaseIndexState")
	}

	if err := UpdateState(db, LastChainIndexState, lastChainIndex, lastBlockTimestamp); err != nil {
		return errors.Wrap(err, "UpdateState(LastChainIndexState)")
	}

	return nil
}
