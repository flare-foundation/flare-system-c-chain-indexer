package database

import (
	"context"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/logger"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"gorm.io/gorm"
)

func DropHistory(db *gorm.DB, intervalSeconds, checkInterval int, client *ethclient.Client) error {
	var deleteStart int
	for {
		var databaseTx *gorm.DB
		lastTx := &Transaction{}
		firstTx := &Transaction{}

		lastBlockTime, _, err := GetBlockTimestamp(nil, client)
		if err != nil {
			logger.Error("Failed to get the latest time: %s", err)
			goto sleep
		}

		deleteStart = lastBlockTime - intervalSeconds

		err = db.Where("timestamp < ?", deleteStart).Order("block_number desc").First(lastTx).Error
		if err != nil {
			if err.Error() != "record not found" {
				logger.Error("Failed to check historic data in the DB: %s", err)
			}
			goto sleep
		}

		databaseTx = db.Begin()
		defer func() {
			if r := recover(); r != nil {
				databaseTx.Rollback()
			}
		}()

		// delete in reverse to not break foreign keys
		for i := len(entities) - 1; i >= 1; i-- {
			entity := entities[i]
			err = db.Where("timestamp < ?", deleteStart).Delete(&entity).Error
			if err != nil {
				databaseTx.Rollback()
				logger.Error("Failed to delete historic data in the DB: %s", err)
				goto sleep
			}
		}

		err = db.Where("timestamp >= ?", deleteStart).Order("block_number").First(firstTx).Error
		if err != nil {
			databaseTx.Rollback()
			logger.Error("Failed to get first transaction in the DB: %s", err)
			goto sleep
		}
		err = States.Update(db, FirstDatabaseIndexState, int(firstTx.BlockNumber), int(firstTx.Timestamp))
		if err != nil {
			databaseTx.Rollback()
			logger.Error("Failed to update state in the DB: %s", err)
			goto sleep
		}

		err = databaseTx.Commit().Error
		if err != nil {
			logger.Error("Failed to delete the data the DB: %s", err)
			goto sleep
		}
		logger.Info("Deleted blocks up to index %d", lastTx.BlockNumber)

	sleep:
		time.Sleep(time.Duration(checkInterval) * time.Second)
	}
}

func GetMinBlockWithHistoryDrop(firstIndex, intervalSeconds int, client *ethclient.Client) (int, error) {
	firstTime, _, err := GetBlockTimestamp(big.NewInt(int64(firstIndex)), client)
	if err != nil {
		return 0, fmt.Errorf("GetMinBlockWithHistoryDrop: %w", err)
	}

	var lastTime, endIndex int
	lastTime, endIndex, err = GetBlockTimestamp(nil, client)

	if err != nil {
		return 0, fmt.Errorf("GetMinBlockWithHistoryDrop: %w", err)
	}

	if lastTime-firstTime < intervalSeconds {
		return firstIndex, nil
	}

	for endIndex-firstIndex > 1 {
		newIndex := (firstIndex + endIndex) / 2

		newTime, _, err := GetBlockTimestamp(big.NewInt(int64(newIndex)), client)
		if err != nil {
			return 0, fmt.Errorf("GetMinBlockWithHistoryDrop: %w", err)
		}
		if lastTime-newTime < intervalSeconds {
			endIndex = newIndex
		} else {
			firstIndex = newIndex
		}
	}

	return firstIndex, nil
}

func GetBlockTimestamp(index *big.Int, client *ethclient.Client) (int, int, error) {
	var block *types.Block
	var err error
	for j := 0; j < config.ReqRepeats; j++ {
		ctx, cancelFunc := context.WithTimeout(context.Background(), time.Duration(1000)*time.Millisecond)
		block, err = client.BlockByNumber(ctx, index)
		cancelFunc()
		if err == nil {
			break
		}
	}
	if err != nil {
		return 0, 0, fmt.Errorf("GetBlockTimestamp: %w", err)
	}

	return int(block.Time()), int(block.Number().Int64()), nil
}
