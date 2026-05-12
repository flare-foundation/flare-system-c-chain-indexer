package core

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/boff"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/database"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/interfaces"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
)

type logsBatch struct {
	logs []types.Log
	mu   sync.RWMutex
}

func (ci *Engine) requestLogs(
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

func (ci *Engine) fetchLogsChunk(
	ctx context.Context, logInfo config.LogInfo, fromBlock, toBlock uint64,
) ([]types.Log, error) {
	var addresses []common.Address
	contractAddress := strings.TrimSpace(logInfo.ContractAddress)
	if contractAddress != "" && !strings.EqualFold(contractAddress, undefined) {
		addresses = []common.Address{
			common.HexToAddress(strings.ToLower(contractAddress)),
		}
	}

	var topic [][]common.Hash
	topic0 := strings.TrimSpace(logInfo.Topic)
	if topic0 != "" && !strings.EqualFold(topic0, undefined) {
		if !strings.HasPrefix(topic0, "0x") {
			topic0 = "0x" + topic0
		}
		topic = [][]common.Hash{{common.HexToHash(strings.ToLower(topic0))}}
	}

	query := interfaces.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(toBlock),
		Addresses: addresses,
		Topics:    topic,
	}

	return boff.RetryWithMaxElapsed(
		ctx,
		func() ([]types.Log, error) {
			ctx, cancelFunc := context.WithTimeout(ctx, config.Timeout)
			defer cancelFunc()

			return ci.client.FilterLogs(ctx, query)
		},
		"fetchLogsChunk",
	)
}

func (ci *Engine) processLogs(
	lgBatch *logsBatch, bBatch *blockBatch, firstBlockNum uint64, data *databaseStructData,
) error {
	lgBatch.mu.RLock()
	defer lgBatch.mu.RUnlock()

	for i := range lgBatch.logs {
		log := &lgBatch.logs[i]

		block := bBatch.blocks[log.BlockNumber-firstBlockNum]
		if blockNum := block.Number(); blockNum.Cmp(new(big.Int).SetUint64(log.BlockNumber)) != 0 {
			return errors.Errorf("block number mismatch: %s != %d", blockNum, log.BlockNumber)
		}

		dbLog := BuildDBLogFromRequestedLog(log, block.Time())

		// check if the log was not obtained from transactions already
		key := fmt.Sprintf("%s%d", dbLog.TransactionHash, dbLog.LogIndex)
		if !data.LogHashIndexCheck[key] {
			data.Logs = append(data.Logs, dbLog)
		}
	}

	return nil
}

func BuildDBLogFromRequestedLog(log *types.Log, timestamp uint64) *database.Log {
	topics := extractLogTopics(log)

	return &database.Log{
		Address:         strings.ToLower(log.Address.Hex()[2:]),
		Data:            hex.EncodeToString(log.Data),
		Topic0:          topics[0],
		Topic1:          topics[1],
		Topic2:          topics[2],
		Topic3:          topics[3],
		TransactionHash: strings.ToLower(log.TxHash.Hex()[2:]),
		LogIndex:        uint64(log.Index),
		Timestamp:       timestamp,
		BlockNumber:     log.BlockNumber,
	}
}

func extractLogTopics(log *types.Log) [numTopics]string {
	var topics [numTopics]string
	for j := 0; j < numTopics; j++ {
		if len(log.Topics) > j {
			topics[j] = log.Topics[j].Hex()[2:]
		} else {
			topics[j] = nullTopic
		}
	}
	return topics
}
