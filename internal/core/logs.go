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

// validateCollectLogs parses every collect_logs filter once at engine
// construction so a config typo fails startup rather than at fetch time.
func validateCollectLogs(logInfos []config.LogInfo) error {
	for _, logInfo := range logInfos {
		if _, err := parseLogAddresses(logInfo.ContractAddress); err != nil {
			return fmt.Errorf("collect_logs address %q: %w", logInfo.ContractAddress, err)
		}
		if _, err := parseLogTopics(logInfo.Topic); err != nil {
			return fmt.Errorf("collect_logs topic %q: %w", logInfo.Topic, err)
		}
	}
	return nil
}

// parseLogAddresses returns the address filter for a collect_logs entry: nil
// (no filter) for empty/"undefined", or the single parsed address.
func parseLogAddresses(contractAddress string) ([]common.Address, error) {
	contractAddress = strings.TrimSpace(contractAddress)
	if contractAddress == "" || strings.EqualFold(contractAddress, undefined) {
		return nil, nil
	}
	if !common.IsHexAddress(contractAddress) {
		return nil, fmt.Errorf("%s is not a valid address", contractAddress)
	}
	return []common.Address{common.HexToAddress(contractAddress)}, nil
}

// parseLogTopics returns the topic0 filter for a collect_logs entry: nil (no
// filter) for empty/"undefined", or the single parsed 32-byte topic.
func parseLogTopics(topic0 string) ([][]common.Hash, error) {
	topic0 = strings.TrimSpace(topic0)
	if topic0 == "" || strings.EqualFold(topic0, undefined) {
		return nil, nil
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(topic0), "0x"))
	if err != nil {
		return nil, fmt.Errorf("decoding topic0: %w", err)
	}
	if len(decoded) != common.HashLength {
		return nil, fmt.Errorf("topic0 %s does not have 32 bytes", topic0)
	}
	return [][]common.Hash{{common.BytesToHash(decoded)}}, nil
}

func (ci *Engine) fetchLogsChunk(
	ctx context.Context, logInfo config.LogInfo, fromBlock, toBlock uint64,
) ([]types.Log, error) {
	addresses, err := parseLogAddresses(logInfo.ContractAddress)
	if err != nil {
		return nil, err
	}

	topic, err := parseLogTopics(logInfo.Topic)
	if err != nil {
		return nil, err
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
			ctx, cancelFunc := context.WithTimeout(ctx, config.RPCTimeout)
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
