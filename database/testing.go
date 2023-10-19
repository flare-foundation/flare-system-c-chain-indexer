package database

import (
	"flare-ftso-indexer/config"
	"strings"

	logger2 "flare-ftso-indexer/logger"

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
	if cfg.OptTables != "" {
		optTables := strings.Split(cfg.OptTables, ",")
		for _, method := range optTables {
			entity, ok := MethodToInterface[method]
			if ok {
				entities = append(entities, entity)
			} else {
				logger2.Error("Unrecognized optional table name %s", method)
			}
		}
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
		s := &State{Name: TransactionsStateName,
			NextDBIndex:    0,
			LastChainIndex: 0,
			FirstDBIndex:   0}
		s.UpdateTime()
		err = db.Create(s).Error
		if err != nil {
			return nil, err
		}
	}
	return db, nil
}
