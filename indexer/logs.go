package indexer

import (
	"context"
	"encoding/hex"
	"flare-ftso-indexer/database"
	"math/big"
	"strconv"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
)

type LogsBatch struct {
	Logs []types.Log
	sync.Mutex
}

func NewLogsBatch() *LogsBatch {
	transactionBatch := LogsBatch{}
	transactionBatch.Logs = make([]types.Log, 0)

	return &transactionBatch
}

func (ci *BlockIndexer) requestLogs(
	ctx context.Context, logsBatch *LogsBatch, logInfo [2]string, start, stop, last_chain_block int,
) error {
	for i := start; i < stop && i <= last_chain_block; i += ci.params.LogRange {
		toBlock := min(i+ci.params.LogRange-1, last_chain_block)
		var addresses []common.Address
		if logInfo[0] != "undefined" {
			addresses = []common.Address{
				common.HexToAddress(strings.ToLower(logInfo[0]))}
		}
		var topic [][]common.Hash
		if logInfo[1] != "undefined" {
			topic = [][]common.Hash{{common.HexToHash(strings.ToLower(logInfo[1]))}}
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

		logsBatch.Mutex.Lock()
		logsBatch.Logs = append(logsBatch.Logs, logs...)
		logsBatch.Mutex.Unlock()
	}

	return nil
}

func (ci *BlockIndexer) processLogs(logsBatch *LogsBatch, blockBatch *BlockBatch,
	firstBlockNum int, data *DatabaseStructData) error {
	for _, log := range logsBatch.Logs {
		topics := make([]string, 4)
		for j := 0; j < 4; j++ {
			if len(log.Topics) > j {
				topics[j] = log.Topics[j].Hex()[2:]
			} else {
				topics[j] = "NULL"
			}
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
			Timestamp:       blockBatch.Blocks[log.BlockNumber-uint64(firstBlockNum)].Time(),
		}
		// check if the log was not obtained from transactions already
		if check := data.LogHashIndexCheck[dbLog.TransactionHash+strconv.Itoa(int(dbLog.LogIndex))]; !check {
			data.Logs = append(data.Logs, dbLog)
		}
	}

	return nil
}
