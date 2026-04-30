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
	LastChainIndexState             string = "last_chain_block"
	LastDatabaseIndexState          string = "last_database_block"
	FirstDatabaseIndexState         string = "first_database_block"
	FirstDatabaseFSPEventIndexState string = "first_database_fsp_event_block"
)

var (
	stateNames = []string{
		FirstDatabaseIndexState,
		FirstDatabaseFSPEventIndexState,
		LastDatabaseIndexState,
		LastChainIndexState,
	}

	// States captures the state of the DB giving guaranties which
	// blocks were indexed. The global variable is used/modified by
	// the indexer as well as the history drop functionality.
	globalStates = NewStates()
)

func IsSet(state *State) bool {
	return state != nil && state.Index != 0
}

type DBStates struct {
	States map[string]*State
	mu     sync.RWMutex
}

func NewStates() *DBStates {
	return &DBStates{States: make(map[string]*State)}
}

func (s *DBStates) replaceAll(newStates map[string]*State) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, state := range newStates {
		s.States[name] = state
	}
}

// Update persists the state values to the DB and refreshes the in-memory
// cache. It MUST NOT be called inside a gorm.DB.Transaction block: the cache
// mutation is not subject to gorm rollback, so an outer rollback would leave
// the cache ahead of the persisted row.
func (s *DBStates) Update(db *gorm.DB, name string, newIndex, blockTimestamp uint64) error {
	s.mu.RLock()
	var id uint64
	if existing := s.States[name]; existing != nil {
		id = existing.ID
	}
	s.mu.RUnlock()

	state := &State{
		BaseEntity:     BaseEntity{ID: id},
		Name:           name,
		Index:          newIndex,
		BlockTimestamp: blockTimestamp,
		Updated:        time.Now(),
	}
	if err := db.Save(state).Error; err != nil {
		return err
	}

	s.mu.Lock()
	s.States[name] = state
	s.mu.Unlock()
	return nil
}

func (s *DBStates) UpdateAtStart(
	db *gorm.DB, startIndex, startBlockTimestamp, lastChainIndex, lastBlockTimestamp uint64,
) error {
	s.mu.RLock()
	_, firstDatabaseIndexSet := s.States[FirstDatabaseIndexState]
	s.mu.RUnlock()

	// Set the first database index state only if it does not exist yet
	if !firstDatabaseIndexSet {
		if err := s.Update(db, FirstDatabaseIndexState, startIndex, startBlockTimestamp); err != nil {
			return errors.Wrap(err, "states.Update(FirstDatabaseIndexState)")
		}
	}

	// Set the state for the current latest chain index
	if err := s.Update(db, LastChainIndexState, lastChainIndex, lastBlockTimestamp); err != nil {
		return errors.Wrap(err, "states.Update(LastChainIndexState)")
	}

	return nil
}

func LoadDBStates(ctx context.Context, db *gorm.DB) (*DBStates, error) {
	newStates, err := getDBStates(ctx, db)
	if err != nil {
		return nil, err
	}

	globalStates.replaceAll(newStates)
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
