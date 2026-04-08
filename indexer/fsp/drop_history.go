package fsp

import (
	"context"
	"flare-ftso-indexer/database"
	"flare-ftso-indexer/indexer/core"
	"flare-ftso-indexer/logger"
	"time"

	systemcontract "github.com/flare-foundation/go-flare-common/pkg/contracts/system"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

func DropHistory(ctx context.Context, ci *core.Engine, checkIntervalSeconds uint64) {
	for {
		logger.Info("starting FSP history drop iteration")
		startTime := time.Now()

		if err := dropHistoryIteration(ctx, ci); err != nil {
			logger.Error("FSP history drop error: %s", err)
		} else {
			logger.Info("finished FSP history drop iteration in %v", time.Since(startTime))
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(checkIntervalSeconds) * time.Second):
		}
	}
}

func dropHistoryIteration(ctx context.Context, ci *core.Engine) error {
	states, err := database.LoadDBStates(ctx, ci.DB())
	if err != nil {
		return errors.Wrap(err, "database.LoadDBStates")
	}

	latestConfirmedNumber, latestConfirmedTimestamp, err := ci.FetchLastBlockIndex(ctx)
	if err != nil {
		return errors.Wrap(err, "ci.FetchLastBlockIndex")
	}

	fsmAddress, err := ci.ContractResolver().ResolveByName(ctx, fspFsmContractName)
	if err != nil {
		return err
	}

	fsmCaller, err := systemcontract.NewFlareSystemsManagerCaller(fsmAddress, ci.Client())
	if err != nil {
		return errors.Wrap(err, "bind FlareSystemsManager caller")
	}

	plan, err := computeStartupPlan(ctx, ci, fsmCaller, latestConfirmedNumber, latestConfirmedTimestamp)
	if err != nil {
		return err
	}

	if plan.keepFromBlock == 0 {
		return ci.DB().Transaction(func(tx *gorm.DB) error {
			if err := states.Update(tx, database.FirstDatabaseIndexState, 0, 0); err != nil {
				return errors.Wrap(err, "update first database state")
			}
			if err := states.Update(tx, database.FirstFullIndexState, plan.fullIndexStartBlock, plan.fullIndexStartTimestamp); err != nil {
				return errors.Wrap(err, "update first full index state")
			}
			if err := states.Update(tx, database.LastDatabaseIndexState, latestConfirmedNumber, latestConfirmedTimestamp); err != nil {
				return errors.Wrap(err, "update last database state")
			}
			if err := states.Update(tx, database.LastChainIndexState, latestConfirmedNumber, latestConfirmedTimestamp); err != nil {
				return errors.Wrap(err, "update last chain state")
			}
			return nil
		})
	}

	dbTx := ci.DB().WithContext(ctx)
	if err := database.DeleteInBatches(dbTx, plan.keepFromTimestamp, database.Log{}); err != nil {
		return errors.Wrap(err, "drop old logs")
	}
	if err := database.DeleteInBatches(dbTx, plan.keepFromTimestamp, database.Transaction{}); err != nil {
		return errors.Wrap(err, "drop old transactions")
	}
	if err := database.DeleteInBatches(dbTx, plan.keepFromTimestamp, database.Block{}); err != nil {
		return errors.Wrap(err, "drop old blocks")
	}

	firstIndex := plan.keepFromBlock
	firstTimestamp := plan.keepFromTimestamp

	return ci.DB().Transaction(func(tx *gorm.DB) error {
		if err := states.Update(tx, database.FirstDatabaseIndexState, firstIndex, firstTimestamp); err != nil {
			return errors.Wrap(err, "update first database state")
		}
		if err := states.Update(tx, database.FirstFullIndexState, plan.fullIndexStartBlock, plan.fullIndexStartTimestamp); err != nil {
			return errors.Wrap(err, "update first full index state")
		}
		if err := states.Update(tx, database.LastDatabaseIndexState, latestConfirmedNumber, latestConfirmedTimestamp); err != nil {
			return errors.Wrap(err, "update last database state")
		}
		if err := states.Update(tx, database.LastChainIndexState, latestConfirmedNumber, latestConfirmedTimestamp); err != nil {
			return errors.Wrap(err, "update last chain state")
		}
		return nil
	})
}
