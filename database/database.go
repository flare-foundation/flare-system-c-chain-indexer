package database

import (
	"flare-ftso-indexer/config"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
	gormMysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	TransactionsStateName string = "ftso_indexer"
)

var (
	// List entities to auto-migrate
	entities []interface{} = []interface{}{
		State{},
		FtsoTransaction{},
	}
)

func ConnectAndInitialize(cfg *config.DBConfig) (*gorm.DB, error) {
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
			}
		}
	}
	// Initialize - auto migrate
	err = db.AutoMigrate(entities...)
	if err != nil {
		return nil, err
	}
	// If the state info is not in the DB, create it
	_, err = FetchState(db, TransactionsStateName)
	if err != nil {
		s := State{Name: TransactionsStateName,
			NextDBIndex:    0,
			LastChainIndex: 0,
			FirstDBIndex:   0}
		s.UpdateTime()
		err = CreateState(db, &s)
		if err != nil {
			return nil, err
		}
	}

	return db, nil
}

func Connect(cfg *config.DBConfig) (*gorm.DB, error) {
	// Connect to the database
	dbConfig := mysql.Config{
		User:                 cfg.Username,
		Passwd:               cfg.Password,
		Net:                  "tcp",
		Addr:                 fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		DBName:               cfg.Database,
		AllowNativePasswords: true,
		ParseTime:            true,
	}

	var gormLogLevel logger.LogLevel
	if cfg.LogQueries {
		gormLogLevel = logger.Info
	} else {
		gormLogLevel = logger.Silent
	}
	gormConfig := gorm.Config{
		Logger: logger.Default.LogMode(gormLogLevel),
	}
	return gorm.Open(gormMysql.Open(dbConfig.FormatDSN()), &gormConfig)
}
