package database

import (
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	LastChainIndexState     string = "last_chain_block"
	NextDatabaseIndexState  string = "next_database_block" // aka last_database_block + 1
	FirstDatabaseIndexState string = "first_database_block"
)

var (
	StateNames = []string{
		FirstDatabaseIndexState,
		NextDatabaseIndexState,
		LastChainIndexState,
	}
	// States captures the state of the DB giving guaranties which
	// blocks were indexed. The global variable is used/modified by
	// the indexer as well as the history drop functionality.
	States = NewStates()
)

type State struct {
	BaseEntity
	Name    string `gorm:"type:varchar(50);index"`
	Index   uint64
	Updated time.Time
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

func (s *State) UpdateIndex(newIndex int) {
	s.Index = uint64(newIndex)
	s.Updated = time.Now()
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

func (states *DBStates) UpdateIndex(name string, newIndex int) {
	states.States[name].UpdateIndex(newIndex)
}

func (states *DBStates) UpdateDB(db *gorm.DB, name string) error {
	return db.Save(states.States[name]).Error
}

func (states *DBStates) Update(db *gorm.DB, name string, newIndex int) error {
	states.UpdateIndex(name, newIndex)
	err := states.UpdateDB(db, name)
	if err != nil {
		return fmt.Errorf("Update: %w", err)
	}

	return nil
}

func (states *DBStates) UpdateAtStart(db *gorm.DB, startIndex, lastChainIndex int) error {
	var err error
	// if a break among saved blocks in the dataset is created,
	// then we change the guaranties about the starting block
	if int(states.States[NextDatabaseIndexState].Index) < startIndex {
		err = states.Update(db, FirstDatabaseIndexState, startIndex)
		if err != nil {
			return fmt.Errorf("UpdateAtStart: %w", err)
		}
	}
	err = states.Update(db, NextDatabaseIndexState, startIndex)
	if err != nil {
		return fmt.Errorf("UpdateAtStart: %w", err)
	}
	err = states.Update(db, LastChainIndexState, lastChainIndex)
	if err != nil {
		return fmt.Errorf("UpdateAtStart: %w", err)
	}

	return nil
}
