package database

import (
	"flare-ftso-indexer/logger"
	"time"

	"gorm.io/gorm"
)

func DropHistory(db *gorm.DB, intervalSeconds, checkInterval int) {
	for {
		var databaseTx *gorm.DB
		deleteStart := time.Now().Add(-time.Duration(intervalSeconds) * time.Second)

		lastTx := &FtsoTransaction{}
		err := db.Where("timestamp < ?", deleteStart.Unix()).Order("block_id desc").First(lastTx).Error
		if err != nil {
			if err.Error() != "record not found" {
				logger.Error("Failed to check historic data in the DB", err)
			}
			goto sleep
		}

		databaseTx = db.Begin()
		defer func() {
			if r := recover(); r != nil {
				databaseTx.Rollback()
			}
		}()
		for _, entity := range entities[1:] {
			err = db.Where("timestamp < ?", deleteStart.Unix()).Delete(&entity).Error
			if err != nil {
				databaseTx.Rollback()
				logger.Error("Failed to delete historic data in the DB", err)
				goto sleep
			}
		}

		err = States.Update(db, FirstDatabaseIndexStateName, int(lastTx.BlockId)+1)
		if err != nil {
			databaseTx.Rollback()
			logger.Error("Failed to update state in the DB", err)
			goto sleep
		}

		err = databaseTx.Commit().Error
		if err != nil {
			logger.Error("Failed to delete the data the DB", err)
			goto sleep
		}
		logger.Info("Deleted blocks up to index %d", lastTx.BlockId)

	sleep:
		time.Sleep(time.Duration(checkInterval) * time.Second)
	}
}
