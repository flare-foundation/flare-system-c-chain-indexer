package indexer

import (
	"context"
	"encoding/hex"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
)

type LogsBatch struct {
	Logs []types.Log
	mu   sync.RWMutex
}

func (ci *BlockIndexer) requestLogs(
	ctx context.Context, logsBatch *LogsBatch, logInfo config.LogInfo, start, stop, last_chain_block int,
) error {
	for i := start; i < stop && i <= last_chain_block; i += ci.params.LogRange {
		toBlock := min(i+ci.params.LogRange-1, last_chain_block)

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
			FromBlock: big.NewInt(int64(i)),
			ToBlock:   big.NewInt(int64(toBlock)),
			Addresses: addresses,
			Topics:    topic,
		}

		logs, err := ci.client.FilterLogs(ctx, query)
		if err != nil {
			return errors.Wrap(err, "client.FilterLogs")
		}

		logsBatch.mu.Lock()
		logsBatch.Logs = append(logsBatch.Logs, logs...)
		logsBatch.mu.Unlock()
	}

	return nil
}

func (ci *BlockIndexer) processLogs(
	logsBatch *LogsBatch, blockBatch *BlockBatch, firstBlockNum int, data *DatabaseStructData,
) error {
	logsBatch.mu.RLock()
	defer logsBatch.mu.RUnlock()

	for i := range logsBatch.Logs {
		log := &logsBatch.Logs[i]

		var topics [numTopics]string
		for j := 0; j < numTopics; j++ {
			if len(log.Topics) > j {
				topics[j] = log.Topics[j].Hex()[2:]
			} else {
				topics[j] = nullTopic
			}
		}

		block := blockBatch.Blocks[log.BlockNumber-uint64(firstBlockNum)]
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
