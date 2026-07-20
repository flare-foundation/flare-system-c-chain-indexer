package core

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/contracts"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/database"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/diagnostics"

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
	if err := validateCollectLogs(cfg.Indexer.CollectLogs); err != nil {
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

	if params.LogRange == 0 {
		params.LogRange = 1
	}

	if params.BatchSize == 0 {
		params.BatchSize = 1
	}

	if params.RpcConcurrency <= 0 {
		params.RpcConcurrency = 1
	}

	if params.NewBlockCheckMillis <= 0 {
		params.NewBlockCheckMillis = 100
	}

	return params
}

func buildTransactionPolicies(txInfo []config.TransactionInfo) (map[common.Address]map[functionSignature]transactionsPolicy, error) {
	transactions := make(map[common.Address]map[functionSignature]transactionsPolicy)

	for i := range txInfo {
		transaction := &txInfo[i]
		contractAddress, err := parseTransactionAddress(transaction.ContractAddress)
		if err != nil {
			return nil, fmt.Errorf("parsing address %s: %w", transaction.ContractAddress, err)
		}

		if _, ok := transactions[contractAddress]; !ok {
			transactions[contractAddress] = make(map[functionSignature]transactionsPolicy)
		}

		funcSig, err := parseFuncSig(transaction.FuncSig)
		if err != nil {
			return nil, fmt.Errorf("parsing func sig %s: %w", transaction.FuncSig, err)
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

func parseTransactionAddress(address string) (common.Address, error) {
	if address == undefined {
		return undefinedAddress, nil
	}

	if !common.IsHexAddress(address) {
		return common.Address{}, errors.New("not an address")
	}

	if !strings.HasPrefix(address, "0x") {
		address = fmt.Sprintf("0x%s", address)
	}

	return common.HexToAddress(address), nil
}

func (ci *Engine) IndexHistory(ctx context.Context, startIndex uint64) (uint64, error) {
	ixRange, err := ci.getIndexRange(ctx, startIndex)
	if err != nil {
		return 0, err
	}

	logger.Infof("Starting history indexing: from=%d, to=%d", ixRange.start, ixRange.end)

	for i := ixRange.start; i <= ixRange.end; i = i + ci.params.BatchSize {
		if err := ci.indexBatch(ctx, i, ixRange); err != nil {
			return 0, errors.Wrapf(err, "indexBatch: from=%d, to=%d", i, min(i+ci.params.BatchSize-1, ixRange.end))
		}

		// in the second to last run of the loop update lastIndex to get the blocks
		// that were produced during the run of the algorithm
		if ci.shouldUpdateLastIndex(ixRange, i) {
			ixRange, err = ci.updateLastIndexHistory(ctx, ixRange)
			if err != nil {
				return 0, err
			}
		}
	}

	return ixRange.end, nil
}

func (ci *Engine) indexBatch(
	ctx context.Context, batchIx uint64, ixRange *indexRange,
) error {
	batchStart := time.Now()
	lastBlockNumInRound := min(batchIx+ci.params.BatchSize-1, ixRange.end)

	// Blocks (and the receipts derived from them) and logs are independent RPC
	// streams: log queries need only the block range, not the fetched bodies.
	// Fetch both concurrently and join before processing, so the handful of
	// eth_getLogs requests complete in the shadow of the much heavier block
	// fetch. A shared errgroup ctx means a failure on either side cancels the
	// other's in-flight requests.
	var (
		bBatch    *blockBatch
		txBatch   *transactionsBatch
		logsBatch *logsBatch
	)

	eg, egCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		var err error
		bBatch, err = ci.obtainBlocksBatch(egCtx, batchIx, lastBlockNumInRound)
		if err != nil {
			return err
		}

		txBatch = ci.processBlocksBatch(bBatch)

		return ci.processTransactionsBatch(egCtx, txBatch)
	})

	eg.Go(func() error {
		var err error
		logsBatch, err = ci.obtainLogsBatch(egCtx, batchIx, lastBlockNumInRound)
		return err
	})

	if err := eg.Wait(); err != nil {
		return err
	}

	lastForDBTimestamp := bBatch.blocks[min(ci.params.BatchSize-1, ixRange.end-batchIx)].Time()
	return ci.processAndSave(
		bBatch,
		txBatch,
		logsBatch,
		batchIx,
		lastBlockNumInRound,
		lastForDBTimestamp,
		batchStart,
	)
}

func (ci *Engine) obtainBlocksBatch(ctx context.Context, firstBlockNumber uint64, lastBlockNumInRound uint64) (*blockBatch, error) {
	startTime := time.Now()

	batchSize := lastBlockNumInRound + 1 - firstBlockNumber
	bBatch := newBlockBatch(batchSize)

	// SetLimit bounds goroutine fan-out so we don't spawn one goroutine per
	// block up front; the real RPC concurrency cap is enforced globally in
	// chain.Client.
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(ci.params.RpcConcurrency)

	for i := uint64(0); i < batchSize; i++ {
		num := firstBlockNumber + i
		eg.Go(func() error {
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

	// Fetch receipts concurrently, one goroutine per transaction (no straggler
	// slices). SetLimit bounds goroutine fan-out, not RPC concurrency — the real
	// cap is enforced globally in chain.Client.
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(ci.params.RpcConcurrency)

	for i := 0; i < len(txBatch.transactions); i++ {
		eg.Go(func() error {
			return ci.fetchReceiptAt(ctx, txBatch, i)
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

	// Fetch logs sequentially, one filter at a time. requestLogs walks
	// [batchIx, lastBlockNumInRound] stepping by LogRange, so LogRange is simply
	// the max number of blocks per eth_getLogs request.
	for _, logInfo := range ci.params.CollectLogs {
		if err := ci.requestLogs(
			ctx,
			lgBatch,
			logInfo,
			batchIx,
			lastBlockNumInRound+1,
			lastBlockNumInRound,
		); err != nil {
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
	ctx context.Context, startIndex uint64,
) (*indexRange, error) {
	lastChainIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "ci.fetchLastBlockIndex")
	}

	if err := database.UpdateState(ci.db, database.ChainTip, lastChainIndex, lastChainTimestamp); err != nil {
		return nil, errors.Wrap(err, "database.UpdateState(LastChainIndexState)")
	}

	lastIndex := min(lastChainIndex, ci.params.StopIndex)

	return &indexRange{start: startIndex, end: lastIndex}, nil
}

func (ci *Engine) updateLastIndexContinuous(
	ctx context.Context, ixRange *indexRange,
) (*indexRange, error) {
	lastIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "ci.fetchLastBlockIndex")
	}

	if err := database.UpdateState(ci.db, database.ChainTip, lastIndex, lastChainTimestamp); err != nil {
		return nil, errors.Wrap(err, "database.UpdateState")
	}

	return &indexRange{start: ixRange.start, end: lastIndex}, nil
}

func (ci *Engine) processAndSave(
	bBatch *blockBatch,
	txBatch *transactionsBatch,
	lgBatch *logsBatch,
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
	if err := ci.saveData(data, lastDBIndex, lastDBTimestamp); err != nil {
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
	ctx context.Context, ixRange *indexRange,
) (*indexRange, error) {
	lastChainIndex, lastChainTimestamp, err := ci.fetchLastBlockIndex(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "ci.fetchLastBlockIndex")
	}

	if err := database.UpdateState(ci.db, database.ChainTip, lastChainIndex, lastChainTimestamp); err != nil {
		return nil, errors.Wrap(err, "database.UpdateState")
	}

	if lastChainIndex > ixRange.end && ci.params.StopIndex > ixRange.end {
		ixRange.end = min(lastChainIndex, ci.params.StopIndex)
		logger.Infof("Extending history range: last_block=%d", ixRange.end)
	}

	return ixRange, nil
}

func (ci *Engine) IndexContinuous(ctx context.Context, startIndex uint64) error {
	ixRange, err := ci.getIndexRange(ctx, startIndex)
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

			ixRange, err = ci.updateLastIndexContinuous(ctx, ixRange)
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

		err = ci.indexContinuousIteration(ctx, blockNum)
		if err != nil {
			return err
		}

		lastProcessedBlockTime = [2]time.Time{time.Now(), time.Now()}
		blockNum++
	}

	logger.Debugf("Stopping continuous indexing: block=%d", blockNum)

	return nil
}

func (ci *Engine) indexContinuousIteration(ctx context.Context, index uint64) error {
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
	if err := ci.saveData(data, index, indexTimestamp); err != nil {
		return errors.Wrapf(err, "saveData: block=%d", index)
	}

	if index%1000 == 0 {
		logger.Infof("Continuous progress: block=%d", index)
	}

	return nil
}
