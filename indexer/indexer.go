package indexer

import (
	"context"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/logger"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type BlockIndexer struct {
	db           *gorm.DB
	params       config.IndexerConfig
	transactions map[string]map[string][2]bool
	client       *ethclient.Client
}

func CreateBlockIndexer(cfg *config.Config, db *gorm.DB, ethClient *ethclient.Client) *BlockIndexer {
	return &BlockIndexer{
		db:           db,
		params:       updateParams(cfg.Indexer),
		transactions: makeTransactions(cfg.Indexer.CollectTransactions),
		client:       ethClient,
	}
}

func updateParams(params config.IndexerConfig) config.IndexerConfig {
	if params.StopIndex == 0 {
		params.StopIndex = int(^uint(0) >> 1)
	}

	if params.TimeoutMillis == 0 {
		params.TimeoutMillis = config.TimeoutMillisDefault
	}

	params.BatchSize -= params.BatchSize % params.NumParallelReq

	if params.LogRange == 0 {
		params.LogRange = 1
	}

	if params.BatchSize == 0 {
		params.BatchSize = 1
	}

	if params.NumParallelReq == 0 {
		params.NumParallelReq = 1
	}

	return params
}

func makeTransactions(txInfo []config.TransactionInfo) map[string]map[string][2]bool {
	transactions := make(map[string]map[string][2]bool)

	for i := range txInfo {
		transaction := &txInfo[i]
		contractAddress := transaction.ContractAddress

		if _, ok := transactions[contractAddress]; !ok {
			transactions[contractAddress] = make(map[string][2]bool)
		}

		transactions[contractAddress][transaction.FuncSig] = [2]bool{
			transaction.Status, transaction.CollectEvents,
		}
	}

	return transactions
}

func (ci *BlockIndexer) SetStartIndex(newIndex int) {
	ci.params.StartIndex = newIndex
}

func (ci *BlockIndexer) IndexHistory() error {
	states, err := database.GetDBStates(ci.db)
	if err != nil {
		return errors.Wrap(err, "database.GetDBStates")
	}

	ixRange, err := ci.getIndexRange(states)
	if err != nil {
		return err
	}

	logger.Info("Starting to index blocks from %d to %d", ixRange.start, ixRange.end)

	for i := ixRange.start; i <= ixRange.end; i = i + ci.params.BatchSize {
		if err := ci.indexBatch(states, i, ixRange); err != nil {
			return err
		}

		// in the second to last run of the loop update lastIndex to get the blocks
		// that were produced during the run of the algorithm
		if ci.shouldUpdateLastIndex(ixRange, i) {
			ixRange, err = ci.updateLastIndexHistory(states, ixRange)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (ci *BlockIndexer) indexBatch(states *database.DBStates, batchIx int, ixRange *indexRange) error {
	lastBlockNumInRound := min(batchIx+ci.params.BatchSize-1, ixRange.end)

	blockBatch, err := ci.obtainBlocksBatch(batchIx, ixRange, lastBlockNumInRound)
	if err != nil {
		return err
	}

	transactionsBatch := ci.processBlocksBatch(blockBatch)

	if err := ci.processTransactionsBatch(transactionsBatch); err != nil {
		return err
	}

	logsBatch, err := ci.obtainLogsBatch(batchIx, lastBlockNumInRound)
	if err != nil {
		return err
	}

	lastForDBTimestamp := int(blockBatch.Blocks[min(ci.params.BatchSize-1, ixRange.end-batchIx)].Time())
	return ci.processAndSave(
		blockBatch,
		transactionsBatch,
		logsBatch,
		states,
		batchIx,
		lastBlockNumInRound,
		lastForDBTimestamp,
	)
}

func (ci *BlockIndexer) obtainBlocksBatch(batchIx int, ixRange *indexRange, lastBlockNumInRound int) (*BlockBatch, error) {
	startTime := time.Now()
	oneRunnerReqNum := ci.params.BatchSize / ci.params.NumParallelReq
	blockBatch := NewBlockBatch(ci.params.BatchSize)

	eg, _ := errgroup.WithContext(context.Background())

	for i := 0; i < ci.params.NumParallelReq; i++ {
		start := batchIx + oneRunnerReqNum*i
		stop := batchIx + oneRunnerReqNum*(i+1)
		listIndex := oneRunnerReqNum * i

		eg.Go(func() error {
			return ci.requestBlocks(
				blockBatch,
				start,
				stop,
				listIndex,
				ixRange.end,
			)
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	logger.Info(
		"Successfully obtained blocks %d to %d in %d milliseconds",
		batchIx, lastBlockNumInRound, time.Since(startTime).Milliseconds(),
	)

	return blockBatch, nil
}

func (ci *BlockIndexer) processBlocksBatch(blockBatch *BlockBatch) *TransactionsBatch {
	startTime := time.Now()
	transactionsBatch := NewTransactionsBatch()

	ci.processBlocks(blockBatch, transactionsBatch, 0, ci.params.BatchSize)
	logger.Info(
		"Successfully extracted %d transactions in %d milliseconds",
		len(transactionsBatch.Transactions), time.Since(startTime).Milliseconds(),
	)

	return transactionsBatch
}

func (ci *BlockIndexer) processTransactionsBatch(transactionsBatch *TransactionsBatch) error {
	startTime := time.Now()
	oneRunnerReqNum := (len(transactionsBatch.Transactions) / ci.params.NumParallelReq) + 1

	eg, _ := errgroup.WithContext(context.Background())

	for i := 0; i < ci.params.NumParallelReq; i++ {
		start := oneRunnerReqNum * i
		stop := min(oneRunnerReqNum*(i+1), len(transactionsBatch.Transactions))

		eg.Go(func() error {
			return ci.getTransactionsReceipt(transactionsBatch, start, stop)
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	logger.Info(
		"Checked receipts of %d transactions in %d milliseconds",
		countReceipts(transactionsBatch), time.Since(startTime).Milliseconds(),
	)

	return nil
}

func (ci *BlockIndexer) obtainLogsBatch(batchIx, lastBlockNumInRound int) (*LogsBatch, error) {
	logsBatch := NewLogsBatch()
	startTime := time.Now()
	numRequests := (ci.params.BatchSize / ci.params.LogRange)
	perRunner := (numRequests / ci.params.NumParallelReq)

	for _, logInfo := range ci.params.CollectLogs {
		eg, _ := errgroup.WithContext(context.Background())

		for i := 0; i < ci.params.NumParallelReq; i++ {
			start := batchIx + perRunner*ci.params.LogRange*i
			stop := batchIx + perRunner*ci.params.LogRange*(i+1)

			eg.Go(func() error {
				return ci.requestLogs(
					logsBatch,
					logInfo,
					start,
					stop,
					lastBlockNumInRound,
				)
			})
		}

		if err := eg.Wait(); err != nil {
			return nil, err
		}
	}

	logger.Info(
		"Obtained %d logs by request in %d milliseconds",
		len(logsBatch.Logs), time.Since(startTime).Milliseconds(),
	)

	return logsBatch, nil
}

type indexRange struct {
	start int
	end   int
}

func (ci *BlockIndexer) getIndexRange(states *database.DBStates) (*indexRange, error) {
	lastChainIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex()
	if err != nil {
		return nil, errors.Wrap(err, "ci.fetchLastBlockIndex")
	}

	startTimestamp, err := ci.fetchBlockTimestamp(ci.params.StartIndex)
	if err != nil {
		return nil, errors.Wrap(err, "ci.fetchBlockTimestamp")
	}

	startIndex, lastIndex, err := states.UpdateAtStart(ci.db, ci.params.StartIndex,
		startTimestamp, lastChainIndex, lastChainTimestamp, ci.params.StopIndex)
	if err != nil {
		return nil, errors.Wrap(err, "states.UpdateAtStart")
	}

	return &indexRange{start: startIndex, end: lastIndex}, nil
}

func (ci *BlockIndexer) updateLastIndexContinuous(
	states *database.DBStates, ixRange *indexRange,
) (*indexRange, error) {
	lastIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex()
	if err != nil {
		return nil, errors.Wrap(err, "ci.fetchLastBlockIndex")
	}

	err = states.Update(ci.db, database.LastChainIndexState, lastIndex, lastChainTimestamp)
	if err != nil {
		return nil, errors.Wrap(err, "states.Update")
	}

	return &indexRange{start: ixRange.start, end: lastIndex}, nil
}

func (ci *BlockIndexer) processAndSave(
	blockBatch *BlockBatch,
	transactionsBatch *TransactionsBatch,
	logsBatch *LogsBatch,
	states *database.DBStates,
	firstBlockNum, lastDBIndex, lastDBTimestamp int,
) error {
	startTime := time.Now()
	data, err := ci.processTransactions(transactionsBatch)
	if err != nil {
		return errors.Wrap(err, "ci.processTransactions")
	}

	numLogsFromReceipts := len(data.Logs)
	err = ci.processLogs(logsBatch, blockBatch, firstBlockNum, data)
	if err != nil {
		return errors.Wrap(err, "ci.processLogs")
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
	err = ci.saveData(data, states, lastDBIndex, lastDBTimestamp)
	if err != nil {
		return errors.Wrap(err, "ci.saveData")
	}

	logger.Info(
		"Saved %d transactions and %d logs in the DB in %d milliseconds",
		len(data.Transactions), len(data.Logs),
		time.Since(startTime).Milliseconds(),
	)

	return nil
}

func (ci *BlockIndexer) shouldUpdateLastIndex(ixRange *indexRange, batchIx int) bool {
	return batchIx+ci.params.BatchSize <= ixRange.end && batchIx+2*ci.params.BatchSize > ixRange.end
}

func (ci *BlockIndexer) updateLastIndexHistory(states *database.DBStates, ixRange *indexRange) (*indexRange, error) {
	lastChainIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex()
	if err != nil {
		return nil, errors.Wrap(err, "ci.fetchLastBlockIndex")
	}

	err = states.Update(ci.db, database.LastChainIndexState, lastChainIndex, lastChainTimestamp)
	if err != nil {
		return nil, errors.Wrap(err, "states.Update")
	}

	if lastChainIndex > ixRange.end && ci.params.StopIndex > ixRange.end {
		ixRange.end = min(lastChainIndex, ci.params.StopIndex)
		logger.Info("Updating the last block to %d", ixRange.end)
	}

	return ixRange, nil
}

func (ci *BlockIndexer) IndexContinuous() error {
	states, err := database.GetDBStates(ci.db)
	if err != nil {
		return errors.Wrap(err, "database.GetDBStates")
	}

	ixRange, err := ci.getIndexRange(states)
	if err != nil {
		return errors.Wrap(err, "ci.getIndexRange")
	}

	logger.Info("Continuously indexing blocks from %d", ixRange.start)

	// Request blocks one by one
	blockBatch := NewBlockBatch(1)
	for i := ixRange.start; i <= ci.params.StopIndex; i++ {
		if i > ixRange.end {
			logger.Debug("Up to date, last block %d", states.States[database.LastChainIndexState].Index)
			time.Sleep(time.Millisecond * time.Duration(ci.params.NewBlockCheckMillis))

			ixRange, err = ci.updateLastIndexContinuous(states, ixRange)
			if err != nil {
				return err
			}

			continue
		}

		err = ci.indexContinuousIteration(states, ixRange, i, blockBatch)
		if err != nil {
			return err
		}
	}

	logger.Debug("Stopping the indexer at block %d", states.States[database.LastDatabaseIndexState].Index)

	return nil
}

func (ci *BlockIndexer) indexContinuousIteration(
	states *database.DBStates, ixRange *indexRange, index int, blockBatch *BlockBatch,
) error {
	err := ci.requestBlocks(blockBatch, index, index+1, 0, ixRange.end)
	if err != nil {
		return errors.Wrap(err, "ci.requestBlocks")
	}

	transactionsBatch := NewTransactionsBatch()
	ci.processBlocks(blockBatch, transactionsBatch, 0, 1)

	err = ci.getTransactionsReceipt(transactionsBatch, 0, len(transactionsBatch.Transactions))
	if err != nil {
		return errors.Wrap(err, "ci.getTransactionsReceipt")
	}

	logsBatch := NewLogsBatch()
	for _, logInfo := range ci.params.CollectLogs {
		err = ci.requestLogs(logsBatch, logInfo, index, index+1, index)
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
	err = ci.saveData(data, states, index, indexTimestamp)
	if err != nil {
		return fmt.Errorf("IndexContinuous: %w", err)
	}

	if index%1000 == 0 {
		logger.Info("Indexer at block %d", index)
	}

	return nil
}
