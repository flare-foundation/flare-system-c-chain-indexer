package core

import (
	"context"
	"encoding/hex"
	"flare-ftso-indexer/internal/chain"
	"flare-ftso-indexer/internal/config"
	"flare-ftso-indexer/internal/contracts"
	"flare-ftso-indexer/internal/database"
	"flare-ftso-indexer/internal/diagnostics"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
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

type Engine struct {
	db               *gorm.DB
	params           config.IndexerConfig
	transactions     map[common.Address]map[functionSignature]transactionsPolicy
	client           *chain.Client
	contractResolver *contracts.ContractResolver
}

type transactionsPolicy struct {
	status        bool
	collectEvents bool
}

type functionSignature [4]byte

func NewEngine(
	cfg *config.Config,
	db *gorm.DB,
	client *chain.Client,
	contractResolver *contracts.ContractResolver,
) (*Engine, error) {
	txs, err := buildTransactionPolicies(cfg.Indexer.CollectTransactions)
	if err != nil {
		return nil, err
	}
	if contractResolver == nil {
		return nil, errors.New("contract resolver is required")
	}

	params := applyIndexerDefaults(cfg.Indexer)
	diagnostics.LogIndexerPolicy(params)

	return &Engine{
		db:               db,
		params:           params,
		transactions:     txs,
		client:           client,
		contractResolver: contractResolver,
	}, nil
}

func applyIndexerDefaults(params config.IndexerConfig) config.IndexerConfig {
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

	if params.NewBlockCheckMillis == 0 {
		params.NewBlockCheckMillis = 100
	}

	return params
}

func buildTransactionPolicies(txInfo []config.TransactionInfo) (map[common.Address]map[functionSignature]transactionsPolicy, error) {
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

func (ci *Engine) IndexHistory(ctx context.Context, startIndex uint64) (uint64, error) {
	states, err := database.LoadDBStates(ctx, ci.db)
	if err != nil {
		return 0, errors.Wrap(err, "database.LoadDBStates")
	}

	ixRange, err := ci.getIndexRange(ctx, states, startIndex)
	if err != nil {
		return 0, err
	}

	logger.Infof("Starting history indexing: from=%d, to=%d", ixRange.start, ixRange.end)

	for i := ixRange.start; i <= ixRange.end; i = i + ci.params.BatchSize {
		if err := ci.indexBatch(ctx, states, i, ixRange); err != nil {
			return 0, errors.Wrapf(err, "indexBatch: from=%d, to=%d", i, min(i+ci.params.BatchSize-1, ixRange.end))
		}

		// in the second to last run of the loop update lastIndex to get the blocks
		// that were produced during the run of the algorithm
		if ci.shouldUpdateLastIndex(ixRange, i) {
			ixRange, err = ci.updateLastIndexHistory(ctx, states, ixRange)
			if err != nil {
				return 0, err
			}
		}
	}

	return ixRange.end, nil
}

func (ci *Engine) indexBatch(
	ctx context.Context, states *database.DBStates, batchIx uint64, ixRange *indexRange,
) error {
	batchStart := time.Now()
	lastBlockNumInRound := min(batchIx+ci.params.BatchSize-1, ixRange.end)

	bBatch, err := ci.obtainBlocksBatch(ctx, batchIx, lastBlockNumInRound)
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
		batchStart,
	)
}

func (ci *Engine) obtainBlocksBatch(ctx context.Context, firstBlockNumber uint64, lastBlockNumInRound uint64) (*blockBatch, error) {
	startTime := time.Now()

	// Use a semaphore to limit concurrent requests.
	sem := make(chan struct{}, ci.params.NumParallelReq)

	batchSize := lastBlockNumInRound + 1 - firstBlockNumber
	bBatch := newBlockBatch(batchSize)

	eg, ctx := errgroup.WithContext(ctx)

	for i := uint64(0); i < batchSize; i++ {
		num := firstBlockNumber + i
		eg.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()

			block, err := ci.fetchBlock(ctx, &num)
			if err != nil {
				return err
			}

			// Locking is unnecessary since each goroutine writes to a different
			// location in the blocks array - there is no possibility of a
			// collision.
			bBatch.blocks[num-firstBlockNumber] = block

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	logger.Debugf(
		"Fetched blocks: from=%d, to=%d, duration_ms=%d",
		firstBlockNumber, lastBlockNumInRound, time.Since(startTime).Milliseconds(),
	)

	return bBatch, nil
}

func (ci *Engine) processBlocksBatch(bBatch *blockBatch) *transactionsBatch {
	startTime := time.Now()
	txBatch := new(transactionsBatch)

	ci.processBlocks(bBatch, txBatch)
	logger.Debugf(
		"Extracted transactions: count=%d, duration_ms=%d",
		len(txBatch.transactions), time.Since(startTime).Milliseconds(),
	)

	return txBatch
}

func (ci *Engine) processTransactionsBatch(
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

	logger.Debugf(
		"Checked receipts: count=%d, duration_ms=%d",
		countReceipts(txBatch), time.Since(startTime).Milliseconds(),
	)

	return nil
}

func (ci *Engine) obtainLogsBatch(
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

	logger.Debugf(
		"Fetched logs: count=%d, duration_ms=%d",
		len(lgBatch.logs), time.Since(startTime).Milliseconds(),
	)

	return lgBatch, nil
}

type indexRange struct {
	start uint64
	end   uint64
}

func (ci *Engine) getIndexRange(
	ctx context.Context, states *database.DBStates, startIndex uint64,
) (*indexRange, error) {
	lastChainIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "ci.fetchLastBlockIndex")
	}

	startTimestamp, err := ci.fetchBlockTimestamp(ctx, startIndex)
	if err != nil {
		return nil, errors.Wrap(err, "ci.fetchBlockTimestamp")
	}

	if err := states.UpdateAtStart(ci.db, startIndex, startTimestamp, lastChainIndex, lastChainTimestamp); err != nil {
		return nil, errors.Wrap(err, "states.UpdateAtStart")
	}

	lastIndex := min(lastChainIndex, ci.params.StopIndex)

	return &indexRange{start: startIndex, end: lastIndex}, nil
}

func (ci *Engine) updateLastIndexContinuous(
	ctx context.Context, states *database.DBStates, ixRange *indexRange,
) (*indexRange, error) {
	lastIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "ci.fetchLastBlockIndex")
	}

	if err := states.Update(ci.db, database.LastChainIndexState, lastIndex, lastChainTimestamp); err != nil {
		return nil, errors.Wrap(err, "states.Update")
	}

	return &indexRange{start: ixRange.start, end: lastIndex}, nil
}

func (ci *Engine) processAndSave(
	bBatch *blockBatch,
	txBatch *transactionsBatch,
	lgBatch *logsBatch,
	states *database.DBStates,
	firstBlockNum, lastDBIndex, lastDBTimestamp uint64,
	batchStart time.Time,
) error {
	data := newDatabaseStructData()
	data.Blocks = ci.convertBlocksToDB(bBatch)

	if err := ci.processTransactions(txBatch, data); err != nil {
		return errors.Wrap(err, "ci.processTransactions")
	}

	numLogsFromReceipts := len(data.Logs)
	if err := ci.processLogs(lgBatch, bBatch, firstBlockNum, data); err != nil {
		return errors.Wrap(err, "ci.processLogs")
	}

	saveStart := time.Now()
	if err := ci.saveData(data, states, lastDBIndex, lastDBTimestamp); err != nil {
		return errors.Wrap(err, "ci.saveData")
	}

	logger.Debugf(
		"Saved batch: transactions=%d, logs=%d, duration_ms=%d",
		len(data.Transactions), len(data.Logs),
		time.Since(saveStart).Milliseconds(),
	)

	logger.Infof(
		"Processed batch: from=%d, to=%d, blocks=%d, transactions=%d, receipt_logs=%d, filter_logs=%d, duration_ms=%d",
		firstBlockNum,
		lastDBIndex,
		len(data.Blocks),
		len(txBatch.transactions),
		numLogsFromReceipts,
		len(data.Logs)-numLogsFromReceipts,
		time.Since(batchStart).Milliseconds(),
	)

	return nil
}

func (ci *Engine) shouldUpdateLastIndex(ixRange *indexRange, batchIx uint64) bool {
	return batchIx+ci.params.BatchSize <= ixRange.end && batchIx+2*ci.params.BatchSize > ixRange.end
}

func (ci *Engine) updateLastIndexHistory(
	ctx context.Context, states *database.DBStates, ixRange *indexRange,
) (*indexRange, error) {
	lastChainIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "ci.fetchLastBlockIndex")
	}

	if err := states.Update(ci.db, database.LastChainIndexState, lastChainIndex, lastChainTimestamp); err != nil {
		return nil, errors.Wrap(err, "states.Update")
	}

	if lastChainIndex > ixRange.end && ci.params.StopIndex > ixRange.end {
		ixRange.end = min(lastChainIndex, ci.params.StopIndex)
		logger.Infof("Extending history range: last_block=%d", ixRange.end)
	}

	return ixRange, nil
}

func (ci *Engine) IndexContinuous(ctx context.Context, startIndex uint64) error {
	states, err := database.LoadDBStates(ctx, ci.db)
	if err != nil {
		return errors.Wrap(err, "database.LoadDBStates")
	}

	ixRange, err := ci.getIndexRange(ctx, states, startIndex)
	if err != nil {
		return errors.Wrap(err, "ci.getIndexRange")
	}

	logger.Infof("Starting continuous indexing: from=%d", ixRange.start)

	// Request blocks one by one
	blockNum := ixRange.start
	lastProcessedBlockTime := [2]time.Time{time.Now(), time.Now()}
	for blockNum <= ci.params.StopIndex {
		if blockNum > ixRange.end {
			time.Sleep(time.Millisecond * time.Duration(ci.params.NewBlockCheckMillis))

			ixRange, err = ci.updateLastIndexContinuous(ctx, states, ixRange)
			if err != nil {
				return err
			}

			elapsed := time.Since(lastProcessedBlockTime[0]).Seconds()
			delay := ci.params.NoNewBlocksDelayWarning
			if delay != 0 && elapsed > delay {
				logger.Warnf("No new blocks: elapsed_seconds=%.2f", time.Since(lastProcessedBlockTime[1]).Seconds())
				lastProcessedBlockTime[0] = time.Now()
			}

			continue
		}

		err = ci.indexContinuousIteration(ctx, states, blockNum)
		if err != nil {
			return err
		}

		lastProcessedBlockTime = [2]time.Time{time.Now(), time.Now()}
		blockNum++
	}

	logger.Debugf("Stopping continuous indexing: block=%d", blockNum)

	return nil
}

func (ci *Engine) indexContinuousIteration(ctx context.Context, states *database.DBStates, index uint64) error {
	startTime := time.Now()
	block, err := ci.fetchBlock(ctx, &index)
	if err != nil {
		return errors.Wrapf(err, "fetchBlock: block=%d", index)
	}

	bBatch := &blockBatch{blocks: []*chain.Block{block}}

	txBatch := new(transactionsBatch)
	ci.processBlocks(bBatch, txBatch)

	err = ci.getTransactionsReceipt(ctx, txBatch, 0, len(txBatch.transactions))
	if err != nil {
		return errors.Wrapf(err, "getTransactionsReceipt: block=%d", index)
	}

	logsBatch := new(logsBatch)
	for _, logInfo := range ci.params.CollectLogs {
		err = ci.requestLogs(ctx, logsBatch, logInfo, index, index+1, index)
		if err != nil {
			return errors.Wrapf(err, "requestLogs: block=%d", index)
		}
	}

	data := newDatabaseStructData()
	data.Blocks = ci.convertBlocksToDB(bBatch)

	if err := ci.processTransactions(txBatch, data); err != nil {
		return errors.Wrapf(err, "processTransactions: block=%d", index)
	}

	err = ci.processLogs(logsBatch, bBatch, index, data)
	if err != nil {
		return errors.Wrapf(err, "processLogs: block=%d", index)
	}

	indexTimestamp := bBatch.blocks[0].Time()
	if err := ci.saveData(data, states, index, indexTimestamp); err != nil {
		return errors.Wrapf(err, "saveData: block=%d", index)
	}

	logger.Debugf(
		"Indexed block: block=%d, transactions=%d, logs=%d, duration_ms=%d",
		index, len(data.Transactions), len(data.Logs), time.Since(startTime).Milliseconds(),
	)

	if index%1000 == 0 {
		logger.Infof("Continuous progress: block=%d", index)
	}

	return nil
}
