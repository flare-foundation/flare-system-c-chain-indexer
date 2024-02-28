package indexer

import (
	"context"
	"encoding/hex"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/logger"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
)

type logsBatch struct {
	logs []types.Log
	mu   sync.RWMutex
}

func (ci *BlockIndexer) requestLogs(
	ctx context.Context,
	lgBatch *logsBatch,
	logInfo config.LogInfo,
	start, stop, last_chain_block uint64,
) error {
	for i := start; i < stop && i <= last_chain_block; i += ci.params.LogRange {
		toBlock := min(i+ci.params.LogRange-1, last_chain_block)

		logs, err := ci.fetchLogsChunk(ctx, logInfo, i, toBlock)
		if err != nil {
			return err
		}

		lgBatch.mu.Lock()
		lgBatch.logs = append(lgBatch.logs, logs...)
		lgBatch.mu.Unlock()
	}

	return nil
}

func (ci *BlockIndexer) fetchLogsChunk(
	ctx context.Context, logInfo config.LogInfo, fromBlock, toBlock uint64,
) ([]types.Log, error) {
	var addresses []common.Address
	if logInfo.ContractAddress != undefined {
		addresses = []common.Address{
			common.HexToAddress(strings.ToLower(logInfo.ContractAddress)),
		}
	}

	var topic [][]common.Hash
	if logInfo.Topic != undefined {
		topic = [][]common.Hash{{common.HexToHash(strings.ToLower(logInfo.Topic))}}
	}

	query := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(toBlock),
		Addresses: addresses,
		Topics:    topic,
	}

	bOff := backoff.NewExponentialBackOff()
	bOff.MaxElapsedTime = config.BackoffMaxElapsedTime

	var logs []types.Log

	err := backoff.RetryNotify(
		func() error {
			ctx, cancelFunc := context.WithTimeout(ctx, config.DefaultTimeout)
			defer cancelFunc()

			var err error
			logs, err = ci.client.FilterLogs(ctx, query)
			return err
		},
		bOff,
		func(err error, d time.Duration) {
			logger.Debug("FilterLogs error: %s after %s", err, d)
		},
	)
	if err != nil {
		return nil, err
	}

	return logs, nil
}

func (ci *BlockIndexer) processLogs(
	lgBatch *logsBatch, bBatch *blockBatch, firstBlockNum uint64, data *databaseStructData,
) error {
	lgBatch.mu.RLock()
	defer lgBatch.mu.RUnlock()

	for i := range lgBatch.logs {
		log := &lgBatch.logs[i]

		var topics [numTopics]string
		for j := 0; j < numTopics; j++ {
			if len(log.Topics) > j {
				topics[j] = log.Topics[j].Hex()[2:]
			} else {
				topics[j] = nullTopic
			}
		}

		block := bBatch.blocks[log.BlockNumber-firstBlockNum]
		if blockNum := block.Number(); blockNum.Cmp(new(big.Int).SetUint64(log.BlockNumber)) != 0 {
			return errors.Errorf("block number mismatch: %s != %d", blockNum, log.BlockNumber)
		}

		dbLog := &database.Log{
			Address:         strings.ToLower(log.Address.Hex()[2:]),
			Data:            hex.EncodeToString(log.Data),
			Topic0:          topics[0],
			Topic1:          topics[1],
			Topic2:          topics[2],
			Topic3:          topics[3],
			TransactionHash: log.TxHash.Hex()[2:],
			LogIndex:        uint64(log.Index),
			Timestamp:       block.Time(),
			BlockNumber:     log.BlockNumber,
		}

		// check if the log was not obtained from transactions already
		key := fmt.Sprintf("%s%d", dbLog.TransactionHash, dbLog.LogIndex)
		if !data.LogHashIndexCheck[key] {
			data.Logs = append(data.Logs, dbLog)
		}
	}

	return nil
}
