package database

import (
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	LastChainIndexState     string = "last_chain_block"
	LastDatabaseIndexState  string = "last_database_block"
	FirstDatabaseIndexState string = "first_database_block"
)

var (
	StateNames = []string{
		FirstDatabaseIndexState,
		LastDatabaseIndexState,
		LastChainIndexState,
	}
	// States captures the state of the DB giving guaranties which
	// blocks were indexed. The global variable is used/modified by
	// the indexer as well as the history drop functionality.
	States = NewStates()
)

type State struct {
	BaseEntity
	Name           string `gorm:"type:varchar(50);index"`
	Index          uint64
	BlockTimestamp uint64
	Updated        time.Time
}

type DBStates struct {
	States map[string]*State
	sync.Mutex
}

func NewStates() *DBStates {
	states := &DBStates{}
	states.States = make(map[string]*State)

	return states
}

func (s *State) UpdateIndex(newIndex, blockTimestamp int) {
	s.Index = uint64(newIndex)
	s.Updated = time.Now()
	s.BlockTimestamp = uint64(blockTimestamp)
}

func GetDBStates(db *gorm.DB) (*DBStates, error) {
	States.Mutex.Lock()
	for _, name := range StateNames {
		var state State
		err := db.Where(&State{Name: name}).First(&state).Error
		if err != nil {
			States.Mutex.Unlock()
			return nil, fmt.Errorf("GetDBStates: %w", err)
		}
		States.States[name] = &state
	}
	States.Mutex.Unlock()

	return States, nil
}

func (states *DBStates) UpdateIndex(name string, newIndex, blockTimestamp int) {
	states.States[name].UpdateIndex(newIndex, blockTimestamp)
}

func (states *DBStates) UpdateDB(db *gorm.DB, name string) error {
	return db.Save(states.States[name]).Error
}

func (states *DBStates) Update(db *gorm.DB, name string, newIndex, blockTimestamp int) error {
	states.UpdateIndex(name, newIndex, blockTimestamp)
	err := states.UpdateDB(db, name)
	if err != nil {
		return fmt.Errorf("Update: %w", err)
	}

	return nil
}

func (states *DBStates) UpdateAtStart(db *gorm.DB, startIndex, startBlockTimestamp,
	lastChainIndex, lastBlockTimestamp, stopIndex int) (int, int, error) {
	var err error
	if startIndex >= int(states.States[FirstDatabaseIndexState].Index) && startIndex <= int(states.States[FirstDatabaseIndexState].Index)+1 {
		startIndex = int(states.States[LastDatabaseIndexState].Index + 1)
	} else {
		// if startIndex is set before existing data in the DB or a break among saved blocks
		// in the DB is created, then we change the guaranties about the starting block
		err = states.Update(db, FirstDatabaseIndexState, startIndex, startBlockTimestamp)
		if err != nil {
			return 0, 0, fmt.Errorf("UpdateAtStart: %w", err)
		}
	}

	err = states.Update(db, LastChainIndexState, lastChainIndex, lastBlockTimestamp)
	if err != nil {
		return 0, 0, fmt.Errorf("UpdateAtStart: %w", err)
	}

	lastIndex := min(stopIndex, lastChainIndex)

	return startIndex, lastIndex, nil
}
