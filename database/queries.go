package database

import (
	"gorm.io/gorm"
)

func FetchState(db *gorm.DB, name string) (*State, error) {
	var currentState State
	err := db.Where(&State{Name: name}).First(&currentState).Error
	return &currentState, err
}

func CreateState(db *gorm.DB, s *State) error {
	return db.Create(s).Error
}

func UpdateState(db *gorm.DB, s *State) error {
	return db.Save(s).Error
}
