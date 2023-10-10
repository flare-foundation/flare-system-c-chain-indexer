package indexer

import (
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/logger"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"gorm.io/gorm"
)

const (
	StateName string = "ftso_indexer" // todo
)

type BlockIndexer struct {
	StateName string

	db     *gorm.DB
	params config.IndexerConfig
	epoch  config.EpochConfig

	client *ethclient.Client
}

func CreateBlockIndexer(cfg *config.Config, db *gorm.DB) (*BlockIndexer, error) {
	blockIndexer := BlockIndexer{}
	blockIndexer.StateName = StateName
	blockIndexer.db = db
	blockIndexer.params = cfg.Indexer
	if blockIndexer.params.StopIndex == 0 {
		blockIndexer.params.StopIndex = int(^uint(0) >> 1)
	}
	if blockIndexer.params.TimeoutMillis == 0 {
		blockIndexer.params.TimeoutMillis = config.TimeoutMillisDefault
	}
	blockIndexer.epoch = cfg.Epochs

	var err error
	blockIndexer.client, err = ethclient.Dial(cfg.Chain.NodeURL)
	if err != nil {
		return nil, err
	}

	return &blockIndexer, nil
}

func (ci *BlockIndexer) IndexHistory() error {
	// Get start and end block number
	currentState, startIndex, lastIndex, err := ci.state()
	if err != nil {
		return err
	}
	logger.Info("Starting to index blocks from %d to %d", startIndex, lastIndex)

	// Split block requests in batches
	blockBatch := NewBlockBatch(ci.params.BatchSize)
	blockErrChan := make(chan error, ci.params.NumParallelReq)
	databaseErrChan := make(chan error, 1)
	databaseErrChan <- nil
	for j := startIndex; j < lastIndex; j = j + ci.params.BatchSize {
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
				return err
			}
		}
		logger.Info(
			"Successfully obtained blocks %d to %d in %d milliseconds",
			j, min(j+ci.params.BatchSize, lastIndex), time.Since(startTime).Milliseconds(),
		)

		// Make sure that the data from the previous batch was saved to the database,
		// before processing new transactions
		err = <-databaseErrChan
		if err != nil {
			return err
		}

		// Process blocks
		startTime = time.Now()
		batchTransactions := NewTransactionsBatch()
		go ci.processBlocks(blockBatch, batchTransactions, 0, ci.params.BatchSize, blockErrChan)
		err = <-blockErrChan
		if err != nil {
			return err
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
				return err
			}
		}
		logger.Info(
			"Checked receipts of %d transactions in %d milliseconds",
			CountReceipts(batchTransactions), time.Since(startTime).Milliseconds(),
		)

		currentState.UpdateNextIndex(min(j+ci.params.BatchSize, lastIndex) + 1)
		// process and save transactions on an independent goroutine
		go ci.processAndSave(batchTransactions, currentState, databaseErrChan)
	}

	err = <-databaseErrChan
	if err != nil {
		return err
	}

	return nil
}

func (ci *BlockIndexer) processAndSave(batchTransactions *TransactionsBatch,
	currentState database.State, errChan chan error) {
	startTime := time.Now()
	transactionData, err := ci.processTransactions(batchTransactions)
	if err != nil {
		errChan <- err
		return
	}
	logger.Info(
		"Processed %d transactions in %d milliseconds",
		len(batchTransactions.Transactions), time.Since(startTime).Milliseconds(),
	)

	// Put transactions in the database
	startTime = time.Now()
	ci.saveTransactions(transactionData, currentState, errChan)
	logger.Info(
		"Saved %d transactions, %d commits, %d reveals, %d signatures,"+
			"%d finalizations, and %d reward offers to the DB in %d milliseconds",
		len(transactionData.Transactions), len(transactionData.Commits),
		len(transactionData.Reveals), len(transactionData.Signatures),
		len(transactionData.Finalizations), len(transactionData.RewardOffers),
		time.Since(startTime).Milliseconds(),
	)
}

func (ci *BlockIndexer) IndexContinuous() error {
	// Get start and end block number
	currentState, index, lastIndex, err := ci.state()
	if err != nil {
		return err
	}
	logger.Info("Starting to continuously index blocks from %d", index)

	// Request blocks one by one
	blockBatch := NewBlockBatch(1)
	errChan := make(chan error, 1)
	for {
		// useful for tests
		if index > ci.params.StopIndex {
			logger.Debug("Stopping the indexer at block %d", currentState.NextDBIndex-1)
			break
		}
		if index > lastIndex {
			logger.Debug("Up to date, last block %d", currentState.LastChainIndex)
			time.Sleep(time.Millisecond * time.Duration(ci.params.NewBlockCheckMillis))
			currentState, index, lastIndex, err = ci.state()
			if err != nil {
				return err
			}
			continue
		}
		ci.requestBlocks(blockBatch, index, index+1, 0, lastIndex, errChan)
		err := <-errChan
		if err != nil {
			return err
		}
		batchTransactions := NewTransactionsBatch()
		ci.processBlocks(blockBatch, batchTransactions, 0, 1, errChan)
		err = <-errChan
		if err != nil {
			return err
		}

		ci.getTransactionsReceipt(batchTransactions, 0, len(batchTransactions.Transactions),
			errChan)
		err = <-errChan
		if err != nil {
			return err
		}

		transactionData, err := ci.processTransactions(batchTransactions)
		if err != nil {
			return err
		}

		index += 1
		currentState.Update(index, lastIndex)
		ci.saveTransactions(transactionData, currentState, errChan)
		err = <-errChan
		if err != nil {
			return err
		}
	}

	return nil
}
