package database

import (
	"flare-ftso-indexer/config"
	"fmt"

	"github.com/go-sql-driver/mysql"
	gormMysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	MysqlTestUser     string = "indexeruser"
	MysqlTestPassword string = "indexeruser"
	MysqlTestHost     string = "localhost"
	MysqlTestPort     int    = 3307
)

func ConnectTestDB(cfg *config.DBConfig) (*gorm.DB, error) {
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

func ConnectAndInitializeTestDB(cfg *config.DBConfig, dropTables bool, stateName string) (*gorm.DB, error) {
	db, err := ConnectTestDB(cfg)
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
		s := State{Name: stateName, NextDBIndex: 0, LastChainIndex: 0}
		s.UpdateTime()
		err := CreateState(db, &s)
		if err != nil {
			return nil, err
		}
	}
	return db, nil
}
