package database

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

const (
	LastChainIndexState     string = "last_chain_block"
	LastDatabaseIndexState  string = "last_database_block"
	FirstDatabaseIndexState string = "first_database_block"
)

var (
	stateNames = []string{
		FirstDatabaseIndexState,
		LastDatabaseIndexState,
		LastChainIndexState,
	}

	// States captures the state of the DB giving guaranties which
	// blocks were indexed. The global variable is used/modified by
	// the indexer as well as the history drop functionality.
	globalStates = NewStates()
)

type State struct {
	BaseEntity
	Name           string `gorm:"type:varchar(50);index"`
	Index          uint64
	BlockTimestamp uint64
	Updated        time.Time
}

func (s *State) updateIndex(newIndex, blockTimestamp uint64) {
	s.Index = newIndex
	s.Updated = time.Now()
	s.BlockTimestamp = blockTimestamp
}

type DBStates struct {
	States map[string]*State
	mu     sync.RWMutex
}

func NewStates() *DBStates {
	return &DBStates{States: make(map[string]*State)}
}

func (s *DBStates) updateStates(newStates map[string]*State) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, state := range newStates {
		s.States[name] = state
	}
}

func (s *DBStates) updateIndex(name string, newIndex, blockTimestamp uint64) {
	s.mu.Lock()
	state := s.States[name]
	if state == nil {
		state = &State{Name: name}
		s.States[name] = state
	}

	state.updateIndex(newIndex, blockTimestamp)
	s.mu.Unlock()
}

func (s *DBStates) updateDB(db *gorm.DB, name string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return db.Save(s.States[name]).Error
}

func (s *DBStates) Update(db *gorm.DB, name string, newIndex, blockTimestamp uint64) error {
	s.updateIndex(name, newIndex, blockTimestamp)
	return s.updateDB(db, name)
}

func (s *DBStates) UpdateAtStart(
	db *gorm.DB, startIndex, startBlockTimestamp, lastChainIndex, lastBlockTimestamp uint64,
) error {
	s.mu.RLock()
	_, firstDatabaseIndexSet := s.States[FirstDatabaseIndexState]
	s.mu.RUnlock()

	// Set the first database index state only if it does not exist yet
	if !firstDatabaseIndexSet {
		err := s.Update(db, FirstDatabaseIndexState, startIndex, startBlockTimestamp)
		if err != nil {
			return errors.Wrap(err, "states.Update(FirstDatabaseIndexState)")
		}
	}

	// Set the state for the current latest chain index
	err := s.Update(db, LastChainIndexState, lastChainIndex, lastBlockTimestamp)
	if err != nil {
		return errors.Wrap(err, "states.Update(LastChainIndexState)")
	}

	return nil
}

func UpdateDBStates(ctx context.Context, db *gorm.DB) (*DBStates, error) {
	newStates, err := getDBStates(ctx, db)
	if err != nil {
		return nil, err
	}

	globalStates.updateStates(newStates)
	return globalStates, nil
}

func getDBStates(ctx context.Context, db *gorm.DB) (map[string]*State, error) {
	newStates := make(map[string]*State)
	var mu sync.Mutex
	eg, ctx := errgroup.WithContext(ctx)

	for i := range stateNames {
		name := stateNames[i]

		eg.Go(func() error {
			state := new(State)
			err := db.WithContext(ctx).Where(&State{Name: name}).First(state).Error
			if err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return errors.Wrap(err, "db.Where")
				}

				return nil
			}

			mu.Lock()
			newStates[name] = state
			mu.Unlock()

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return newStates, nil
}
