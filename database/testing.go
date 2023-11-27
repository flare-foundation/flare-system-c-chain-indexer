package database

import (
	"flare-ftso-indexer/config"

	"gorm.io/gorm"
)

const (
	MysqlTestUser     string = "indexeruser"
	MysqlTestPassword string = "indexeruser"
	MysqlTestHost     string = "localhost"
	MysqlTestPort     int    = 3307
)

func ConnectAndInitializeTestDB(cfg *config.DBConfig, dropTables bool) (*gorm.DB, error) {
	db, err := Connect(cfg)
	if err != nil {
		return nil, err
	}

	if dropTables {
		err = db.Migrator().DropTable(entities...)
		if err != nil {
			return nil, err
		}
	}

	// Initialize - auto migrate
	err = db.AutoMigrate(entities...)
	if err != nil {
		return nil, err
	}

	if dropTables {
		for _, name := range StateNames {
			s := &State{Name: name}
			s.UpdateIndex(0, 0)
			err = db.Create(s).Error
			if err != nil {
				return nil, err
			}
		}

	}
	return db, nil
}
