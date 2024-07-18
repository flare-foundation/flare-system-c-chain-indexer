package indexer

import (
	"context"
	"encoding/hex"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/logger"
	"fmt"
	"strings"
	"time"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

const (
	nullTopic = "NULL"
	numTopics = 4
	undefined = "undefined"
)

var (
	undefinedAddress = common.Address{}
	undefinedFuncSig = functionSignature{}
)

type BlockIndexer struct {
	db           *gorm.DB
	params       config.IndexerConfig
	transactions map[common.Address]map[functionSignature]transactionsPolicy
	client       ethclient.Client
}

type transactionsPolicy struct {
	status        bool
	collectEvents bool
}

type functionSignature [4]byte

func CreateBlockIndexer(cfg *config.Config, db *gorm.DB, ethClient ethclient.Client) (*BlockIndexer, error) {
	txs, err := makeTransactions(cfg.Indexer.CollectTransactions)
	if err != nil {
		return nil, err
	}

	return &BlockIndexer{
		db:           db,
		params:       updateParams(cfg.Indexer),
		transactions: txs,
		client:       ethClient,
	}, nil
}

func updateParams(params config.IndexerConfig) config.IndexerConfig {
	if params.StopIndex == 0 {
		params.StopIndex = ^uint64(0)
	}

	params.BatchSize -= params.BatchSize % uint64(params.NumParallelReq)

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

func makeTransactions(txInfo []config.TransactionInfo) (map[common.Address]map[functionSignature]transactionsPolicy, error) {
	transactions := make(map[common.Address]map[functionSignature]transactionsPolicy)

	for i := range txInfo {
		transaction := &txInfo[i]
		contractAddress := parseTransactionAddress(transaction.ContractAddress)

		if _, ok := transactions[contractAddress]; !ok {
			transactions[contractAddress] = make(map[functionSignature]transactionsPolicy)
		}

		funcSig, err := parseFuncSig(transaction.FuncSig)
		if err != nil {
			return nil, err
		}

		transactions[contractAddress][funcSig] = transactionsPolicy{
			status:        transaction.Status,
			collectEvents: transaction.CollectEvents,
		}
	}

	return transactions, nil
}

func parseFuncSig(funcSig string) (functionSignature, error) {
	if funcSig == undefined {
		return undefinedFuncSig, nil
	}

	funcSig = strings.TrimPrefix(funcSig, "0x")

	bs, err := hex.DecodeString(funcSig)
	if err != nil {
		return functionSignature{}, err
	}

	if len(bs) != 4 {
		return functionSignature{}, errors.New("invalid length function signature")
	}

	var funcSigBytes functionSignature
	copy(funcSigBytes[:], bs)

	return funcSigBytes, nil
}

func parseTransactionAddress(address string) common.Address {
	if address == undefined {
		return undefinedAddress
	}

	if !strings.HasPrefix(address, "0x") {
		address = fmt.Sprintf("0x%s", address)
	}

	return common.HexToAddress(address)
}

func (ci *BlockIndexer) IndexHistory(ctx context.Context) error {
	states, err := database.UpdateDBStates(ctx, ci.db)
	if err != nil {
		return errors.Wrap(err, "database.UpdateDBStates")
	}

	ixRange, err := ci.getIndexRange(ctx, states)
	if err != nil {
		return err
	}

	logger.Info("Starting to index blocks from %d to %d", ixRange.start, ixRange.end)

	for i := ixRange.start; i <= ixRange.end; i = i + ci.params.BatchSize {
		if err := ci.indexBatch(ctx, states, i, ixRange); err != nil {
			return err
		}

		// in the second to last run of the loop update lastIndex to get the blocks
		// that were produced during the run of the algorithm
		if ci.shouldUpdateLastIndex(ixRange, i) {
			ixRange, err = ci.updateLastIndexHistory(ctx, states, ixRange)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (ci *BlockIndexer) indexBatch(
	ctx context.Context, states *database.DBStates, batchIx uint64, ixRange *indexRange,
) error {
	lastBlockNumInRound := min(batchIx+ci.params.BatchSize-1, ixRange.end)

	bBatch, err := ci.obtainBlocksBatch(ctx, batchIx, ixRange, lastBlockNumInRound)
	if err != nil {
		return err
	}

	txBatch := ci.processBlocksBatch(bBatch)

	if err := ci.processTransactionsBatch(ctx, txBatch); err != nil {
		return err
	}

	logsBatch, err := ci.obtainLogsBatch(ctx, batchIx, lastBlockNumInRound)
	if err != nil {
		return err
	}

	lastForDBTimestamp := bBatch.blocks[min(ci.params.BatchSize-1, ixRange.end-batchIx)].Time()
	return ci.processAndSave(
		bBatch,
		txBatch,
		logsBatch,
		states,
		batchIx,
		lastBlockNumInRound,
		lastForDBTimestamp,
	)
}

func (ci *BlockIndexer) obtainBlocksBatch(
	ctx context.Context, firstBlockNumber uint64, ixRange *indexRange, lastBlockNumInRound uint64,
) (*blockBatch, error) {
	startTime := time.Now()

	batchSize := lastBlockNumInRound + 1 - firstBlockNumber
	numParallelReq := max(uint64(ci.params.NumParallelReq), batchSize)

	oneRunnerReqNum := (batchSize + numParallelReq - 1) / numParallelReq
	bBatch := newBlockBatch(batchSize)

	eg, ctx := errgroup.WithContext(ctx)

	for i := uint64(0); i < numParallelReq; i++ {
		batchIxStart := oneRunnerReqNum * i
		batchIxStop := batchIxStart + oneRunnerReqNum

		blockNumberStart := firstBlockNumber + batchIxStart
		blockNumberStop := firstBlockNumber + batchIxStop
		if blockNumberStop > lastBlockNumInRound {
			blockNumberStop = lastBlockNumInRound + 1
		}

		eg.Go(func() error {
			blocks, err := ci.requestBlocks(
				ctx, blockNumberStart, blockNumberStop,
			)
			if err != nil {
				return err
			}

			if len(blocks) != int(batchIxStop-batchIxStart) {
				return errors.New("unexpected number of blocks returned")
			}

			copy(bBatch.blocks[batchIxStart:batchIxStop], blocks)

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	logger.Info(
		"Successfully obtained blocks %d to %d in %d milliseconds",
		firstBlockNumber, lastBlockNumInRound, time.Since(startTime).Milliseconds(),
	)

	return bBatch, nil
}

func (ci *BlockIndexer) processBlocksBatch(bBatch *blockBatch) *transactionsBatch {
	startTime := time.Now()
	txBatch := new(transactionsBatch)

	ci.processBlocks(bBatch, txBatch)
	logger.Info(
		"Successfully extracted %d transactions in %d milliseconds",
		len(txBatch.transactions), time.Since(startTime).Milliseconds(),
	)

	return txBatch
}

func (ci *BlockIndexer) processTransactionsBatch(
	ctx context.Context, txBatch *transactionsBatch,
) error {
	startTime := time.Now()
	oneRunnerReqNum := (len(txBatch.transactions) / ci.params.NumParallelReq) + 1

	eg, ctx := errgroup.WithContext(ctx)

	for i := 0; i < ci.params.NumParallelReq; i++ {
		start := oneRunnerReqNum * i
		stop := min(oneRunnerReqNum*(i+1), len(txBatch.transactions))

		eg.Go(func() error {
			return ci.getTransactionsReceipt(ctx, txBatch, start, stop)
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	logger.Info(
		"Checked receipts of %d transactions in %d milliseconds",
		countReceipts(txBatch), time.Since(startTime).Milliseconds(),
	)

	return nil
}

func (ci *BlockIndexer) obtainLogsBatch(
	ctx context.Context, batchIx, lastBlockNumInRound uint64,
) (*logsBatch, error) {
	lgBatch := new(logsBatch)
	startTime := time.Now()
	numRequests := ci.params.BatchSize / ci.params.LogRange
	perRunner := numRequests / uint64(ci.params.NumParallelReq)

	for _, logInfo := range ci.params.CollectLogs {
		eg, ctx := errgroup.WithContext(ctx)

		for i := 0; i < ci.params.NumParallelReq; i++ {
			start := batchIx + perRunner*ci.params.LogRange*uint64(i)
			stop := batchIx + perRunner*ci.params.LogRange*uint64(i+1)

			eg.Go(func() error {
				return ci.requestLogs(
					ctx,
					lgBatch,
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
		len(lgBatch.logs), time.Since(startTime).Milliseconds(),
	)

	return lgBatch, nil
}

type indexRange struct {
	start uint64
	end   uint64
}

func (ci *BlockIndexer) getIndexRange(
	ctx context.Context, states *database.DBStates,
) (*indexRange, error) {
	lastChainIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "ci.fetchLastBlockIndex")
	}

	startTimestamp, err := ci.fetchBlockTimestamp(ctx, ci.params.StartIndex)
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
	ctx context.Context, states *database.DBStates, ixRange *indexRange,
) (*indexRange, error) {
	lastIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex(ctx)
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
	bBatch *blockBatch,
	txBatch *transactionsBatch,
	lgBatch *logsBatch,
	states *database.DBStates,
	firstBlockNum, lastDBIndex, lastDBTimestamp uint64,
) error {
	startTime := time.Now()

	data := newDatabaseStructData()
	data.Blocks = ci.convertBlocksToDB(bBatch)

	if err := ci.processTransactions(txBatch, data); err != nil {
		return errors.Wrap(err, "ci.processTransactions")
	}

	numLogsFromReceipts := len(data.Logs)
	if err := ci.processLogs(lgBatch, bBatch, firstBlockNum, data); err != nil {
		return errors.Wrap(err, "ci.processLogs")
	}

	logger.Info(
		"Processed %d blocks with %d transactions and extracted %d logs from receipts and %d new logs from requests in %d milliseconds",
		len(data.Blocks),
		len(txBatch.transactions),
		numLogsFromReceipts,
		len(data.Logs)-numLogsFromReceipts,
		time.Since(startTime).Milliseconds(),
	)

	// Push transactions and logs in the database
	startTime = time.Now()
	err := ci.saveData(data, states, lastDBIndex, lastDBTimestamp)
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

func (ci *BlockIndexer) shouldUpdateLastIndex(ixRange *indexRange, batchIx uint64) bool {
	return batchIx+ci.params.BatchSize <= ixRange.end && batchIx+2*ci.params.BatchSize > ixRange.end
}

func (ci *BlockIndexer) updateLastIndexHistory(
	ctx context.Context, states *database.DBStates, ixRange *indexRange,
) (*indexRange, error) {
	lastChainIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex(ctx)
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

func (ci *BlockIndexer) IndexContinuous(ctx context.Context) error {
	states, err := database.UpdateDBStates(ctx, ci.db)
	if err != nil {
		return errors.Wrap(err, "database.UpdateDBStates")
	}

	ixRange, err := ci.getIndexRange(ctx, states)
	if err != nil {
		return errors.Wrap(err, "ci.getIndexRange")
	}

	logger.Info("Continuously indexing blocks from %d", ixRange.start)

	// Request blocks one by one
	blockNum := ixRange.start
	for blockNum <= ci.params.StopIndex {
		if blockNum > ixRange.end {
			logger.Debug("Up to date, last block %d", states.States[database.LastChainIndexState].Index)
			time.Sleep(time.Millisecond * time.Duration(ci.params.NewBlockCheckMillis))

			ixRange, err = ci.updateLastIndexContinuous(ctx, states, ixRange)
			if err != nil {
				return err
			}

			continue
		}

		err = ci.indexContinuousIteration(ctx, states, ixRange, blockNum)
		if err != nil {
			return err
		}

		blockNum++
	}

	logger.Debug("Stopping the indexer at block %d", states.States[database.LastDatabaseIndexState].Index)

	return nil
}

func (ci *BlockIndexer) indexContinuousIteration(
	ctx context.Context,
	states *database.DBStates,
	ixRange *indexRange,
	index uint64,
) error {
	block, err := ci.fetchBlock(ctx, &index)
	if err != nil {
		return errors.Wrap(err, "ci.fetchBlock")
	}

	bBatch := &blockBatch{blocks: []*types.Block{block}}

	txBatch := new(transactionsBatch)
	ci.processBlocks(bBatch, txBatch)

	err = ci.getTransactionsReceipt(ctx, txBatch, 0, len(txBatch.transactions))
	if err != nil {
		return errors.Wrap(err, "ci.getTransactionsReceipt")
	}

	logsBatch := new(logsBatch)
	for _, logInfo := range ci.params.CollectLogs {
		err = ci.requestLogs(ctx, logsBatch, logInfo, index, index+1, index)
		if err != nil {
			return fmt.Errorf("IndexContinuous: %w", err)
		}
	}

	data := newDatabaseStructData()
	data.Blocks = ci.convertBlocksToDB(bBatch)

	if err := ci.processTransactions(txBatch, data); err != nil {
		return fmt.Errorf("IndexContinuous: %w", err)
	}

	err = ci.processLogs(logsBatch, bBatch, index, data)
	if err != nil {
		return fmt.Errorf("IndexContinuous: %w", err)
	}

	indexTimestamp := bBatch.blocks[0].Time()
	err = ci.saveData(data, states, index, indexTimestamp)
	if err != nil {
		return fmt.Errorf("IndexContinuous: %w", err)
	}

	if index%1000 == 0 {
		logger.Info("Indexer at block %d", index)
	}

	return nil
}
