package fsp

import (
	"context"
	"flare-ftso-indexer/boff"
	"flare-ftso-indexer/chain"
	"flare-ftso-indexer/config"
	"flare-ftso-indexer/contracts"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer/core"
	"flare-ftso-indexer/logger"
	"time"

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
	cIndexer, err := core.NewEngine(cfg, db, ethClient, resolver)
	if err != nil {
		return err
	}

	if err := setSyncedState(ctx, db, false); err != nil {
		return errors.Wrap(err, "set synced=false")
	}

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
	logger.Info(
		"Using FSP history drop: history_epochs=%d, derived retention days=%.2f",
		cfg.Indexer.HistoryEpochs,
		float64(historyDropSeconds)/(24*60*60),
	)
	go database.DropHistory(
		ctx,
		db,
		historyDropSeconds,
		database.HistoryDropIntervalCheck,
		ethClient,
		0,
	)

	if err := setSyncedState(ctx, db, true); err != nil {
		return errors.Wrap(err, "set synced=true")
	}

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

	logger.Info("Finished FSP indexing")

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

func setSyncedState(ctx context.Context, db *gorm.DB, synced bool) error {
	states, err := database.LoadDBStates(ctx, db)
	if err != nil {
		return errors.Wrap(err, "database.LoadDBStates")
	}

	index := uint64(0)
	if synced {
		index = 1
	}

	return states.Update(db, database.SyncedState, index, 0)
}
