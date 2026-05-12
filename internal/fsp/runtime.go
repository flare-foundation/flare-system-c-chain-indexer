package fsp

import (
	"context"
	"math/big"
	"time"

	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/boff"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/chain"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/config"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/contracts"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/core"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/database"
	"github.com/flare-foundation/flare-system-c-chain-indexer/internal/ready"

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
	chainID *big.Int,
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

	historyDropSeconds := historyDropHeuristicSeconds(chainID, cfg.Indexer.HistoryEpochs)
	logger.Infof(
		"Using FSP history drop: chain_id=%s, history_epochs=%d, derived_retention_seconds=%d, derived_retention_days=%.2f",
		chainID,
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

// historyDropHeuristicSeconds derives the retention window used by history drop
// from the number of FSP reward epochs that must remain indexable. It keeps four extra
// reward epochs worth of history to cover the metadata event windows required by the
// oldest indexed reward epoch + extra buffer for potentially extended epochs.
func historyDropHeuristicSeconds(chainId *big.Int, historyEpochs uint64) uint64 {
	return (historyEpochs + 4) * rewardEpochSecondsFor(chainId)
}

// rewardEpochSecondsByChain is the reward-epoch duration per network.
// Mainnets run 3.5d epochs; testnets run 6h.
var rewardEpochSecondsByChain = map[chain.ChainID]uint64{
	chain.ChainIDFlare:    uint64((84 * time.Hour) / time.Second),
	chain.ChainIDSongbird: uint64((84 * time.Hour) / time.Second),
	chain.ChainIDCoston:   uint64((6 * time.Hour) / time.Second),
	chain.ChainIDCoston2:  uint64((6 * time.Hour) / time.Second),
}

func rewardEpochSecondsFor(chainID *big.Int) uint64 {
	chainIDInt := chain.ChainIDFromBigInt(chainID)
	if v, ok := rewardEpochSecondsByChain[chainIDInt]; ok {
		return v
	}
	return rewardEpochSecondsByChain[chain.ChainIDFlare]
}
