package indexer

import (
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer/abi"
	"flare-ftso-indexer/logger"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/exp/slices"
	"gorm.io/gorm"
)

type BlockIndexer struct {
	db          *gorm.DB
	params      config.IndexerConfig
	epochParams config.EpochConfig
	optTables   map[string]bool
	client      *ethclient.Client
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
	blockIndexer.epochParams = cfg.Epochs

	blockIndexer.optTables = make(map[string]bool)
	if cfg.DB.OptTables != "" {
		methods := strings.Split(cfg.DB.OptTables, ",")
		for _, method := range methods {
			if slices.Contains(abi.FtsoMethods, method) == false {
				logger.Error("Unrecognized optional table name %s", method)
				continue
			}
			blockIndexer.optTables[method] = true
		}
	}

	var err error
	blockIndexer.client, err = ethclient.Dial(cfg.Chain.NodeURL)
	if err != nil {
		return nil, err
	}

	return &blockIndexer, nil
}

func (ci *BlockIndexer) IndexHistory() error {
	// Get start and end block number
	state, err := ci.dbState()
	if err != nil {
		return err
	}
	lastChainIndex, err := ci.fetchLastBlockIndex()
	if err != nil {
		return err
	}
	startIndex, lastIndex, err := ci.getIndexes(state, lastChainIndex)
	if err != nil {
		return err
	}
	state.UpdateAtStart(startIndex, lastChainIndex)

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
			countReceipts(batchTransactions), time.Since(startTime).Milliseconds(),
		)

		state.UpdateNextIndex(min(j+ci.params.BatchSize, lastIndex) + 1)
		// process and save transactions on an independent goroutine
		go ci.processAndSave(batchTransactions, state, databaseErrChan)
	}

	err = <-databaseErrChan
	if err != nil {
		return err
	}

	return nil
}

func (ci *BlockIndexer) processAndSave(batchTransactions *TransactionsBatch,
	currentState *database.State, errChan chan error) {
	startTime := time.Now()
	transactionData, err := ci.processTransactions(batchTransactions)
	if err != nil {
		errChan <- err
		return
	}
	logger.Info(
		"Processed %d transactions, and extracted %d commits, %d reveals, %d signatures,"+
			" %d finalizations, and %d reward offers in %d milliseconds",
		len(batchTransactions.Transactions), len(transactionData.Commits),
		len(transactionData.Reveals), len(transactionData.Signatures),
		len(transactionData.Finalizations), len(transactionData.RewardOffers),
		time.Since(startTime).Milliseconds(),
	)

	// Put transactions in the database
	startTime = time.Now()
	errChan2 := make(chan error, 1)
	ci.saveData(transactionData, errChan2)
	err = <-errChan2
	if err != nil {
		errChan <- err
		return
	}
	logger.Info(
		"Saved %d transactions in DB in %d milliseconds",
		len(transactionData.Transactions),
		time.Since(startTime).Milliseconds(),
	)

	err = database.UpdateState(ci.db, currentState)
	if err != nil {
		errChan <- err
	}

	errChan <- nil
}

func (ci *BlockIndexer) IndexContinuous() error {
	// Get start and end block number
	currentState, err := ci.dbState()
	if err != nil {
		return err
	}
	lastChainIndex, err := ci.fetchLastBlockIndex()
	if err != nil {
		return err
	}
	index, lastIndex, err := ci.getIndexes(currentState, lastChainIndex)
	if err != nil {
		return err
	}
	currentState.UpdateAtStart(index, lastChainIndex)
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
			lastIndex, err = ci.fetchLastBlockIndex()
			if err != nil {
				return err
			}
			currentState.UpdateLastIndex(lastIndex)
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
		currentState.UpdateNextIndex(index)
		ci.saveData(transactionData, errChan)
		err = <-errChan
		if err != nil {
			return err
		}
		err = database.UpdateState(ci.db, currentState)
		if err != nil {
			return err
		}
	}

	return nil
}

func (ci *BlockIndexer) getIndexes(state *database.State, lastIndex int) (int, int, error) {
	startIndex := max(int(state.NextDBIndex), ci.params.StartIndex)
	lastIndex = min(ci.params.StopIndex, lastIndex)

	return startIndex, lastIndex, nil
}
