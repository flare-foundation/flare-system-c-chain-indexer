package indexer

import (
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/logger"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

type BlockIndexer struct {
	db           *gorm.DB
	params       config.IndexerConfig
	transactions map[string]map[string][2]bool
	client       *ethclient.Client
}

func CreateBlockIndexer(cfg *config.Config, db *gorm.DB, ethClient *ethclient.Client) (*BlockIndexer, error) {
	blockIndexer := BlockIndexer{}
	blockIndexer.db = db
	blockIndexer.params = cfg.Indexer
	if blockIndexer.params.StopIndex == 0 {
		blockIndexer.params.StopIndex = int(^uint(0) >> 1)
	}
	if blockIndexer.params.TimeoutMillis == 0 {
		blockIndexer.params.TimeoutMillis = config.TimeoutMillisDefault
	}
	blockIndexer.params.BatchSize -= blockIndexer.params.BatchSize % blockIndexer.params.NumParallelReq

	blockIndexer.transactions = make(map[string]map[string][2]bool)
	for i := range cfg.Indexer.CollectTransactions {
		transaction := &cfg.Indexer.CollectTransactions[i]
		contractAddress := transaction.ContractAddress

		if _, ok := blockIndexer.transactions[contractAddress]; !ok {
			blockIndexer.transactions[contractAddress] = map[string][2]bool{}
		}

		blockIndexer.transactions[contractAddress][transaction.FuncSig] = [2]bool{
			transaction.Status, transaction.CollectEvents,
		}
	}

	blockIndexer.client = ethClient
	if blockIndexer.params.LogRange == 0 {
		blockIndexer.params.LogRange = 1
	}
	if blockIndexer.params.BatchSize == 0 {
		blockIndexer.params.BatchSize = 1
	}
	if blockIndexer.params.NumParallelReq == 0 {
		blockIndexer.params.NumParallelReq = 1
	}

	return &blockIndexer, nil
}

func (ci *BlockIndexer) SetStartIndex(newIndex int) {
	ci.params.StartIndex = newIndex
}

func (ci *BlockIndexer) IndexHistory() error {
	// Get start and end block number
	States, err := database.GetDBStates(ci.db)
	if err != nil {
		return fmt.Errorf("IndexHistory: %w", err)
	}
	lastChainIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex()
	if err != nil {
		return fmt.Errorf("IndexHistory: %w", err)
	}
	startTimestamp, err := ci.fetchBlockTimestamp(ci.params.StartIndex)
	if err != nil {
		return fmt.Errorf("IndexHistory: %w", err)
	}
	startIndex, lastIndex, err := States.UpdateAtStart(ci.db, ci.params.StartIndex,
		startTimestamp, lastChainIndex, lastChainTimestamp, ci.params.StopIndex)
	if err != nil {
		return fmt.Errorf("IndexHistory: %w", err)
	}
	logger.Info("Starting to index blocks from %d to %d", startIndex, lastIndex)

	// Split block requests in batches
	parallelErrChan := make(chan error, ci.params.NumParallelReq)
	databaseErrChan := make(chan error, 1)
	databaseErrChan <- nil
	for j := startIndex; j <= lastIndex; j = j + ci.params.BatchSize {
		// Split batched block requests among goroutines
		lastBlockNumInRound := min(j+ci.params.BatchSize-1, lastIndex)
		blockBatch := NewBlockBatch(ci.params.BatchSize)
		startTime := time.Now()
		oneRunnerReqNum := ci.params.BatchSize / ci.params.NumParallelReq
		for i := 0; i < ci.params.NumParallelReq; i++ {
			start := j + oneRunnerReqNum*i
			stop := j + oneRunnerReqNum*(i+1)
			go ci.requestBlocks(blockBatch, start, stop, oneRunnerReqNum*i,
				lastIndex, parallelErrChan)
		}
		for i := 0; i < ci.params.NumParallelReq; i++ {
			err := <-parallelErrChan
			if err != nil {
				return fmt.Errorf("IndexHistory: %w", err)
			}
		}
		logger.Info(
			"Successfully obtained blocks %d to %d in %d milliseconds",
			j, lastBlockNumInRound, time.Since(startTime).Milliseconds(),
		)

		// Process blocks
		startTime = time.Now()
		transactionsBatch := NewTransactionsBatch()
		go ci.processBlocks(blockBatch, transactionsBatch, 0, ci.params.BatchSize, parallelErrChan)
		err = <-parallelErrChan
		if err != nil {
			return fmt.Errorf("IndexHistory: %w", err)
		}
		logger.Info(
			"Successfully extracted %d transactions in %d milliseconds",
			len(transactionsBatch.Transactions), time.Since(startTime).Milliseconds(),
		)

		// Process transactions with goroutines
		startTime = time.Now()
		oneRunnerReqNum = (len(transactionsBatch.Transactions) / ci.params.NumParallelReq) + 1
		for i := 0; i < ci.params.NumParallelReq; i++ {
			start := oneRunnerReqNum * i
			stop := min(oneRunnerReqNum*(i+1), len(transactionsBatch.Transactions))
			go ci.getTransactionsReceipt(transactionsBatch,
				start, stop, parallelErrChan)
		}
		for i := 0; i < ci.params.NumParallelReq; i++ {
			err := <-parallelErrChan
			if err != nil {
				return fmt.Errorf("IndexHistory: %w", err)
			}
		}
		logger.Info(
			"Checked receipts of %d transactions in %d milliseconds",
			countReceipts(transactionsBatch), time.Since(startTime).Milliseconds(),
		)

		// Obtain and process logs with goroutines
		logsBatch := NewLogsBatch()
		startTime = time.Now()
		numRequests := (ci.params.BatchSize / ci.params.LogRange)
		perRunner := (numRequests / ci.params.NumParallelReq)
		for _, logInfo := range ci.params.CollectLogs {
			for i := 0; i < ci.params.NumParallelReq; i++ {
				start := j + perRunner*ci.params.LogRange*i
				stop := j + perRunner*ci.params.LogRange*(i+1)
				go ci.requestLogs(logsBatch, logInfo, start, stop,
					lastBlockNumInRound, parallelErrChan)
			}
			for i := 0; i < ci.params.NumParallelReq; i++ {
				err := <-parallelErrChan
				if err != nil {
					return fmt.Errorf("IndexHistory: %w", err)
				}
			}
		}
		logger.Info(
			"Obtained %d logs by request in %d milliseconds",
			len(logsBatch.Logs), time.Since(startTime).Milliseconds(),
		)

		// Make sure that the data from the previous batch was saved to the database,
		// before processing and saving new data
		err = <-databaseErrChan
		if err != nil {
			return fmt.Errorf("IndexHistory: %w", err)
		}

		// process and save transactions and logs on an independent goroutine
		lastForDBTimestamp := int(blockBatch.Blocks[min(ci.params.BatchSize-1, lastIndex-j)].Time())
		go ci.processAndSave(blockBatch, transactionsBatch, logsBatch, States, j, lastBlockNumInRound,
			lastForDBTimestamp, databaseErrChan)

		// in the second to last run of the loop update lastIndex to get the blocks
		// that were produced during the run of the algorithm
		if j+ci.params.BatchSize <= lastIndex && j+2*ci.params.BatchSize > lastIndex {
			lastChainIndex, lastChainTimestamp, err = ci.fetchLastBlockIndex()
			if err != nil {
				return fmt.Errorf("IndexHistory: %w", err)
			}

			err := States.Update(ci.db, database.LastChainIndexState, lastChainIndex, lastChainTimestamp)
			if err != nil {
				return errors.Wrap(err, "States.Update")
			}

			if lastChainIndex > lastIndex && ci.params.StopIndex > lastIndex {
				lastIndex = min(lastChainIndex, ci.params.StopIndex)
				logger.Info("Updating the last block to %d", lastIndex)
			}
		}
	}

	err = <-databaseErrChan
	if err != nil {
		return fmt.Errorf("IndexHistory: %w", err)
	}

	return nil
}

func (ci *BlockIndexer) processAndSave(blockBatch *BlockBatch, transactionsBatch *TransactionsBatch, logsBatch *LogsBatch,
	states *database.DBStates, firstBlockNum, lastDBIndex, lastDBTimestamp int, errChan chan error) {
	startTime := time.Now()
	data, err := ci.processTransactions(transactionsBatch)
	if err != nil {
		errChan <- fmt.Errorf("processAndSave: %w", err)
		return
	}
	numLogsFromReceipts := len(data.Logs)
	err = ci.processLogs(logsBatch, blockBatch, firstBlockNum, data)
	if err != nil {
		errChan <- fmt.Errorf("processAndSave: %w", err)
		return
	}
	logger.Info(
		"Processed %d transactions and extracted %d logs from receipts "+
			"and %d new logs from requests in %d milliseconds",
		len(transactionsBatch.Transactions), numLogsFromReceipts,
		len(data.Logs)-numLogsFromReceipts,
		time.Since(startTime).Milliseconds(),
	)

	// Push transactions and logs in the database
	startTime = time.Now()
	errChan2 := make(chan error, 1)
	ci.saveData(data, states, lastDBIndex, lastDBTimestamp, errChan2)
	err = <-errChan2
	if err != nil {
		errChan <- fmt.Errorf("processAndSave: %w", err)
		return
	}
	logger.Info(
		"Saved %d transactions and %d logs in the DB in %d milliseconds",
		len(data.Transactions), len(data.Logs),
		time.Since(startTime).Milliseconds(),
	)

	errChan <- nil
}

func (ci *BlockIndexer) IndexContinuous() error {
	// Get start and end block number
	states, err := database.GetDBStates(ci.db)
	if err != nil {
		return fmt.Errorf("IndexContinuous: %w", err)
	}
	lastChainIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex()
	if err != nil {
		return fmt.Errorf("IndexContinuous: %w", err)
	}
	startTimestamp, err := ci.fetchBlockTimestamp(ci.params.StartIndex)
	if err != nil {
		return fmt.Errorf("IndexHistory: %w", err)
	}
	index, lastIndex, err := states.UpdateAtStart(ci.db, ci.params.StartIndex,
		startTimestamp, lastChainIndex, lastChainTimestamp, ci.params.StopIndex)
	if err != nil {
		return fmt.Errorf("IndexContinuous: %w", err)
	}
	logger.Info("Continuously indexing blocks from %d", index)

	// Request blocks one by one
	blockBatch := NewBlockBatch(1)
	errChan := make(chan error, 1)
	for {
		// useful for tests
		if index > ci.params.StopIndex {
			logger.Debug("Stopping the indexer at block %d", states.States[database.LastDatabaseIndexState].Index)
			break
		}
		if index > lastIndex {
			logger.Debug("Up to date, last block %d", states.States[database.LastChainIndexState].Index)
			time.Sleep(time.Millisecond * time.Duration(ci.params.NewBlockCheckMillis))
			lastIndex, lastChainTimestamp, err = ci.fetchLastBlockIndex()
			if err != nil {
				return fmt.Errorf("IndexContinuous: %w", err)
			}

			err := states.Update(ci.db, database.LastChainIndexState, lastIndex, lastChainTimestamp)
			if err != nil {
				return errors.Wrap(err, "States.Update")
			}

			continue
		}
		ci.requestBlocks(blockBatch, index, index+1, 0, lastIndex, errChan)
		err := <-errChan
		if err != nil {
			return fmt.Errorf("IndexContinuous: %w", err)
		}
		transactionsBatch := NewTransactionsBatch()
		ci.processBlocks(blockBatch, transactionsBatch, 0, 1, errChan)
		err = <-errChan
		if err != nil {
			return fmt.Errorf("IndexContinuous: %w", err)
		}

		ci.getTransactionsReceipt(transactionsBatch, 0, len(transactionsBatch.Transactions),
			errChan)
		err = <-errChan
		if err != nil {
			return fmt.Errorf("IndexContinuous: %w", err)
		}

		logsBatch := NewLogsBatch()
		for _, logInfo := range ci.params.CollectLogs {
			ci.requestLogs(logsBatch, logInfo, index, index+1,
				index, errChan)
			err = <-errChan
			if err != nil {
				return fmt.Errorf("IndexContinuous: %w", err)
			}
		}

		data, err := ci.processTransactions(transactionsBatch)
		if err != nil {
			return fmt.Errorf("IndexContinuous: %w", err)
		}

		err = ci.processLogs(logsBatch, blockBatch, index, data)
		if err != nil {
			return fmt.Errorf("IndexContinuous: %w", err)
		}

		indexTimestamp := int(blockBatch.Blocks[0].Time())
		ci.saveData(data, states, index, indexTimestamp, errChan)
		err = <-errChan
		if err != nil {
			return fmt.Errorf("IndexContinuous: %w", err)
		}

		if index%1000 == 0 {
			logger.Info("Indexer at block %d", index)
		}
		index += 1
	}

	return nil
}
