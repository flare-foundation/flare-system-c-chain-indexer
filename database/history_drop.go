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

func DropHistory(db *gorm.DB, intervalSeconds, checkInterval int, nodeURL string) error {
	var client *ethclient.Client
	var err error
	for {
		client, err = ethclient.Dial(nodeURL)
		if err != nil {
			logger.Error("Failed to dial node: %s", err)
			time.Sleep(time.Duration(checkInterval) * time.Second)
		}
		break
	}

	var deleteStart int
	for {
		var databaseTx *gorm.DB
		lastTx := &FtsoTransaction{}

		lastBlockTime, _, err := GetBlockTimestamp(nil, client)
		if err != nil {
			logger.Error("Failed to get the latest time: %s", err)
			goto sleep
		}

		deleteStart = lastBlockTime - intervalSeconds

		err = db.Where("timestamp < ?", deleteStart).Order("block_id desc").First(lastTx).Error
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
		for _, entity := range entities[1:] {
			err = db.Where("timestamp < ?", deleteStart).Delete(&entity).Error
			if err != nil {
				databaseTx.Rollback()
				logger.Error("Failed to delete historic data in the DB: %s", err)
				goto sleep
			}
		}

		err = States.Update(db, FirstDatabaseIndexState, int(lastTx.BlockId)+1)
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
		logger.Info("Deleted blocks up to index %d", lastTx.BlockId)

	sleep:
		time.Sleep(time.Duration(checkInterval) * time.Second)
	}
}

func GetMinBlockWithHistoryDrop(firstIndex, intervalSeconds int, nodeURL string) (int, error) {
	client, err := ethclient.Dial(nodeURL)
	if err != nil {
		return 0, fmt.Errorf("GetMinBlockWithHistoryDrop: %w", err)
	}
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
