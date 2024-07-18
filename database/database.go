package database

import (
	"context"
	"flare-ftso-indexer/config"
	"fmt"
	"sync/atomic"

	"github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
	gormMysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	tcp                      = "tcp"
	HistoryDropIntervalCheck = 60 * 30 // every 30 min
	DBTransactionBatchesSize = 1000
)

var (
	// List entities to auto-migrate
	entities = []interface{}{
		State{},
		Block{},
		Transaction{},
		Log{},
	}
	TransactionId atomic.Uint64
)

func ConnectAndInitialize(ctx context.Context, cfg *config.DBConfig) (*gorm.DB, error) {
	db, err := connect(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("ConnectAndInitialize: Connect: %w", err)
	}

	if cfg.DropTableAtStart {
		err = db.Migrator().DropTable(entities...)
		if err != nil {
			return nil, err
		}
	}

	// Initialize - auto migrate
	err = db.AutoMigrate(entities...)
	if err != nil {
		return nil, errors.Wrap(err, "ConnectAndInitialize: AutoMigrate")
	}

	// If the state info is not in the DB, create it
	_, err = UpdateDBStates(ctx, db)
	if err != nil {
		for _, name := range stateNames {
			s := &State{Name: name}
			s.updateIndex(0, 0)
			err = db.Create(s).Error
			if err != nil {
				return nil, errors.Wrap(err, "ConnectAndInitialize: Create")
			}
		}
	}

	if err := storeTransactionID(db); err != nil {
		return nil, err
	}

	return db, nil
}

func storeTransactionID(db *gorm.DB) (err error) {
	maxIndexTx := new(Transaction)
	err = db.Last(maxIndexTx).Error
	if err == nil {
		TransactionId.Store(maxIndexTx.ID + 1)
		return nil
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		TransactionId.Store(1)
		return nil
	}

	return errors.Wrap(err, "Failed to obtain ID data from DB")
}

func connect(ctx context.Context, cfg *config.DBConfig) (*gorm.DB, error) {
	// Connect to the database
	dbConfig := mysql.Config{
		User:                 cfg.Username,
		Passwd:               cfg.Password,
		Net:                  tcp,
		Addr:                 fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		DBName:               cfg.Database,
		AllowNativePasswords: true,
		ParseTime:            true,
	}

	gormLogLevel := getGormLogLevel(cfg)
	gormConfig := gorm.Config{
		Logger:          gormlogger.Default.LogMode(gormLogLevel),
		CreateBatchSize: DBTransactionBatchesSize,
	}

	db, err := gorm.Open(gormMysql.Open(dbConfig.FormatDSN()), &gormConfig)
	if err != nil {
		return nil, err
	}

	return db.WithContext(ctx), nil
}

func getGormLogLevel(cfg *config.DBConfig) gormlogger.LogLevel {
	if cfg.LogQueries {
		return gormlogger.Info
	}

	return gormlogger.Silent
}
