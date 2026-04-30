package fsp

import (
	"context"
	"flare-ftso-indexer/internal/boff"
	"flare-ftso-indexer/internal/chain"
	"flare-ftso-indexer/internal/config"
	"flare-ftso-indexer/internal/contracts"
	"flare-ftso-indexer/internal/core"
	"flare-ftso-indexer/internal/database"
	"flare-ftso-indexer/internal/ready"
	"time"

	"github.com/flare-foundation/go-flare-common/pkg/logger"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

func RunIndexer(
	ctx context.Context,
	cfg *config.Config,
	db *gorm.DB,
	ethClient *chain.Client,
	resolver *contracts.ContractResolver,
) error {
	logger.Infof(
		"Starting indexer in FSP mode: history_epochs=%d, fsp_tx_lookback_seconds=%d, collect_transactions=%d, collect_logs=%d",
		cfg.Indexer.HistoryEpochs,
		cfg.Indexer.FspTxLookbackSeconds,
		len(cfg.Indexer.CollectTransactions),
		len(cfg.Indexer.CollectLogs),
	)

	cIndexer, err := core.NewEngine(cfg, db, ethClient, resolver)
	if err != nil {
		return err
	}

	ready.SetSynced(false)

	historyLastIndex, err := boff.Retry(
		ctx,
		func() (uint64, error) {
			return IndexStartup(ctx, cIndexer)
		},
		"IndexFspStartup",
	)
	if err != nil {
		return errors.Wrap(err, "FSP startup backfill fatal error")
	}

	historyDropSeconds := historyDropHeuristicSeconds(cfg.Indexer.HistoryEpochs)
	logger.Infof(
		"Using FSP history drop: history_epochs=%d, derived retention=%ds (%.2f days)",
		cfg.Indexer.HistoryEpochs,
		historyDropSeconds,
		float64(historyDropSeconds)/(24*60*60),
	)
	if cfg.DB.HistoryDrop != nil {
		logger.Warnf(
			"db.history_drop=%d is ignored in FSP mode; retention is derived from history_epochs",
			*cfg.DB.HistoryDrop,
		)
	}
	go database.DropHistory(
		ctx,
		db,
		historyDropSeconds,
		database.HistoryDropIntervalCheck,
		ethClient,
		0,
	)

	ready.SetSynced(true)

	err = boff.RetryNoReturn(
		ctx,
		func() error {
			return cIndexer.IndexContinuous(ctx, historyLastIndex+1)
		},
		"IndexContinuous",
	)
	if err != nil {
		return errors.Wrap(err, "FSP Index continuous fatal error")
	}

	logger.Infof("Finished FSP indexing")

	return nil
}

func historyDropHeuristicSeconds(historyEpochs uint64) uint64 {
	// Over-estimation heuristic: history_epochs * 3.5 days + 14 days.
	const (
		baseRetentionSeconds     = uint64((14 * 24 * time.Hour) / time.Second)
		retentionPerEpochSeconds = uint64((84 * time.Hour) / time.Second)
	)

	return baseRetentionSeconds + historyEpochs*retentionPerEpochSeconds
}
