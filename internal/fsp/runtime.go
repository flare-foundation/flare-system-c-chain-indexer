package fsp

import (
	"context"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/boff"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/contracts"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/core"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/database"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/ready"

	systemcontract "github.com/flare-foundation/go-flare-common/pkg/contracts/system"
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
		"Starting indexer in FSP mode: history_epochs=%d, collect_transactions=%d, collect_logs=%d",
		cfg.Indexer.HistoryEpochs,
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

	fsmAddress, err := cIndexer.ContractResolver().ResolveByName(ctx, fspFsmContractName)
	if err != nil {
		return errors.Wrap(err, "resolve FlareSystemsManager for history drop")
	}
	fsmCaller, err := systemcontract.NewFlareSystemsManagerCaller(fsmAddress, cIndexer.Client())
	if err != nil {
		return errors.Wrap(err, "bind FlareSystemsManager caller for history drop")
	}

	logger.Infof(
		"Using FSP history drop: history_epochs=%d, retention anchored on the oldest needed epoch's on-chain data",
		cfg.Indexer.HistoryEpochs,
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
		database.HistoryDropIntervalCheck,
		func(ctx context.Context) (uint64, error) {
			// Retried so a single transient RPC failure among the boundary's
			// contract reads does not skip a whole drop iteration.
			return boff.RetryWithMaxElapsed(ctx, func() (uint64, error) {
				return fspRetentionBoundary(ctx, fsmCaller, cfg.Indexer.HistoryEpochs)
			}, "fspRetentionBoundary")
		},
	)

	ready.SetSynced(true)

	err = boff.RetryNoReturn(
		ctx,
		func() error {
			// Re-read progress each attempt so a retry resumes from the
			// high-water mark, not the startup tip.
			startIndex, err := database.ContinuousStartIndex(db, historyLastIndex)
			if err != nil {
				return err
			}
			return cIndexer.IndexContinuous(ctx, startIndex)
		},
		"IndexContinuous",
	)
	if err != nil {
		return errors.Wrap(err, "FSP Index continuous fatal error")
	}

	logger.Infof("Finished FSP indexing")

	return nil
}
