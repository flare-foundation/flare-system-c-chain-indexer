package indexer

import (
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer/abi"
	"flare-ftso-indexer/logger"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/exp/slices"
	"gorm.io/gorm"
)

type BlockIndexer struct {
	db           *gorm.DB
	params       config.IndexerConfig
	epochParams  config.EpochConfig
	transactions map[string]map[string][2]bool
	optTables    map[string]bool
	client       *ethclient.Client
}

func CreateBlockIndexer(cfg *config.Config, db *gorm.DB) (*BlockIndexer, error) {
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

	blockIndexer.epochParams = cfg.Epochs

	blockIndexer.transactions = make(map[string]map[string][2]bool)
	for _, transaction := range cfg.Indexer.Collect {
		contactAddress := transaction[0].(string)
		funcSig := transaction[1].(string)
		status := transaction[2].(bool)
		collectEvent := transaction[3].(bool)
		if _, ok := blockIndexer.transactions[contactAddress]; !ok {
			blockIndexer.transactions[contactAddress] = map[string][2]bool{}
		}
		blockIndexer.transactions[contactAddress][funcSig] = [2]bool{status, collectEvent}
	}

	blockIndexer.optTables = make(map[string]bool)
	if cfg.DB.OptTables != "" {
		methods := strings.Split(cfg.DB.OptTables, ",")
		for _, method := range methods {
			if slices.Contains(abi.FtsoMethods, method) == false {
				logger.Error("Unrecognized optional table name: %s", method)
				continue
			}
			blockIndexer.optTables[method] = true
		}
	}

	var err error
	blockIndexer.client, err = ethclient.Dial(cfg.Chain.NodeURL)
	if err != nil {
		return nil, fmt.Errorf("CreateBlockIndexer: Dial: %w", err)
	}

	return &blockIndexer, nil
}

func (ci *BlockIndexer) IndexHistory() error {
	// Get start and end block number
	States, err := database.GetDBStates(ci.db)
	if err != nil {
		return fmt.Errorf("IndexHistory: %w", err)
	}
	lastChainIndex, err := ci.fetchLastBlockIndex()
	if err != nil {
		return fmt.Errorf("IndexHistory: %w", err)
	}
	startIndex, lastIndex := ci.getIndexes(States, lastChainIndex)
	err = States.UpdateAtStart(ci.db, startIndex, lastChainIndex)
	if err != nil {
		return fmt.Errorf("IndexHistory: %w", err)
	}
	logger.Info("Starting to index blocks from %d to %d", startIndex, lastIndex)

	// Split block requests in batches
	blockBatch := NewBlockBatch(ci.params.BatchSize)
	blockErrChan := make(chan error, ci.params.NumParallelReq)
	databaseErrChan := make(chan error, 1)
	databaseErrChan <- nil
	for j := startIndex; j <= lastIndex; j = j + ci.params.BatchSize {
		// Split batched block requests among goroutines
		startTime := time.Now()
		oneRunnerReqNum := ci.params.BatchSize / ci.params.NumParallelReq
		for i := 0; i < ci.params.NumParallelReq; i++ {
			start := j + oneRunnerReqNum*i
			stop := j + oneRunnerReqNum*(i+1)
			go ci.requestBlocks(blockBatch, start, stop, oneRunnerReqNum*i,
				lastIndex, blockErrChan)
		}
		for i := 0; i < ci.params.NumParallelReq; i++ {
			err := <-blockErrChan
			if err != nil {
				return fmt.Errorf("IndexHistory: %w", err)
			}
		}
		logger.Info(
			"Successfully obtained blocks %d to %d in %d milliseconds",
			j, min(j+ci.params.BatchSize-1, lastIndex), time.Since(startTime).Milliseconds(),
		)

		// Make sure that the data from the previous batch was saved to the database,
		// before processing new transactions
		err = <-databaseErrChan
		if err != nil {
			return fmt.Errorf("IndexHistory: %w", err)
		}

		// Process blocks
		startTime = time.Now()
		batchTransactions := NewTransactionsBatch()
		go ci.processBlocks(blockBatch, batchTransactions, 0, ci.params.BatchSize, blockErrChan)
		err = <-blockErrChan
		if err != nil {
			return fmt.Errorf("IndexHistory: %w", err)
		}
		logger.Info(
			"Successfully extracted %d transactions in %d milliseconds",
			len(batchTransactions.Transactions), time.Since(startTime).Milliseconds(),
		)

		// Process transactions with goroutines
		startTime = time.Now()
		oneRunnerReqNum = (len(batchTransactions.Transactions) / ci.params.NumParallelReq) + 1
		for i := 0; i < ci.params.NumParallelReq; i++ {
			start := oneRunnerReqNum * i
			stop := min(oneRunnerReqNum*(i+1), len(batchTransactions.Transactions))
			go ci.getTransactionsReceipt(batchTransactions,
				start, stop, blockErrChan)
		}
		for i := 0; i < ci.params.NumParallelReq; i++ {
			err := <-blockErrChan
			if err != nil {
				return fmt.Errorf("IndexHistory: %w", err)
			}
		}
		logger.Info(
			"Checked receipts of %d transactions in %d milliseconds",
			countReceipts(batchTransactions), time.Since(startTime).Milliseconds(),
		)

		// process and save transactions on an independent goroutine
		go ci.processAndSave(batchTransactions, States, min(j+ci.params.BatchSize, lastIndex+1), databaseErrChan)

		// in the second to last run of the loop update lastIndex to get the blocks
		// that were produced during the run of the algorithm
		if j+ci.params.BatchSize <= lastIndex && j+2*ci.params.BatchSize > lastIndex {
			lastChainIndex, err := ci.fetchLastBlockIndex()
			if err != nil {
				return fmt.Errorf("IndexHistory: %w", err)
			}
			States.Update(ci.db, database.LastChainIndexState, lastChainIndex)
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

func (ci *BlockIndexer) processAndSave(batchTransactions *TransactionsBatch,
	states *database.DBStates, newIndex int, errChan chan error) {
	startTime := time.Now()
	transactionData, err := ci.processTransactions(batchTransactions)
	if err != nil {
		errChan <- fmt.Errorf("processAndSave: %w", err)
		return
	}
	logger.Info(
		"Processed %d transactions, %d logs, and extracted %d commits, %d reveals, "+
			"%d signatures, %d finalizations, and %d reward offers in %d milliseconds",
		len(batchTransactions.Transactions), len(transactionData.Logs),
		len(transactionData.Commits), len(transactionData.Reveals),
		len(transactionData.Signatures), len(transactionData.Finalizations),
		len(transactionData.RewardOffers), time.Since(startTime).Milliseconds(),
	)

	// Put transactions in the database
	startTime = time.Now()
	errChan2 := make(chan error, 1)
	ci.saveData(transactionData, states, newIndex, errChan2)
	err = <-errChan2
	if err != nil {
		errChan <- fmt.Errorf("processAndSave: %w", err)
		return
	}
	logger.Info(
		"Saved %d transactions in DB in %d milliseconds",
		len(transactionData.Transactions),
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
	lastChainIndex, err := ci.fetchLastBlockIndex()
	if err != nil {
		return fmt.Errorf("IndexContinuous: %w", err)
	}
	index, lastIndex := ci.getIndexes(states, lastChainIndex)
	err = states.UpdateAtStart(ci.db, index, lastChainIndex)
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
			logger.Debug("Stopping the indexer at block %d", states.States[database.NextDatabaseIndexState].Index-1)
			break
		}
		if index > lastIndex {
			logger.Debug("Up to date, last block %d", states.States[database.LastChainIndexState].Index)
			time.Sleep(time.Millisecond * time.Duration(ci.params.NewBlockCheckMillis))
			lastIndex, err = ci.fetchLastBlockIndex()
			if err != nil {
				return fmt.Errorf("IndexContinuous: %w", err)
			}
			states.Update(ci.db, database.LastChainIndexState, lastIndex)
			continue
		}
		ci.requestBlocks(blockBatch, index, index+1, 0, lastIndex, errChan)
		err := <-errChan
		if err != nil {
			return fmt.Errorf("IndexContinuous: %w", err)
		}
		batchTransactions := NewTransactionsBatch()
		ci.processBlocks(blockBatch, batchTransactions, 0, 1, errChan)
		err = <-errChan
		if err != nil {
			return fmt.Errorf("IndexContinuous: %w", err)
		}

		ci.getTransactionsReceipt(batchTransactions, 0, len(batchTransactions.Transactions),
			errChan)
		err = <-errChan
		if err != nil {
			return fmt.Errorf("IndexContinuous: %w", err)
		}

		transactionData, err := ci.processTransactions(batchTransactions)
		if err != nil {
			return fmt.Errorf("IndexContinuous: %w", err)
		}

		index += 1
		ci.saveData(transactionData, states, index, errChan)
		err = <-errChan
		if err != nil {
			return fmt.Errorf("IndexContinuous: %w", err)
		}

		if index%1000 == 0 {
			logger.Info("Indexer at block %d", index)
		}
	}

	return nil
}

func (ci *BlockIndexer) getIndexes(states *database.DBStates, lastIndex int) (int, int) {
	var startIndex int
	if ci.params.StartIndex < int(states.States[database.FirstDatabaseIndexState].Index) {
		startIndex = ci.params.StartIndex
	} else {
		startIndex = max(int(states.States[database.NextDatabaseIndexState].Index), ci.params.StartIndex)
	}
	lastIndex = min(ci.params.StopIndex, lastIndex)

	return startIndex, lastIndex
}
