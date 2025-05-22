package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/schollz/progressbar/v3"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv-rewards/pkg/beacon"
	"github.com/bloxapp/ssv-rewards/pkg/models"

	_ "github.com/lib/pq"
)

func SyncContractEvents(
	ctx context.Context,
	logger *zap.Logger,
	spec beacon.Spec,
	eventParser *eventparser.EventParser,
	el *executionclient.ExecutionClient,
	db *sql.DB,
	fromBlock, toBlock uint64,
) error {
	if toBlock < fromBlock {
		return fmt.Errorf(
			"from block (%d) cannot be greater than to block (%d)",
			fromBlock,
			toBlock,
		)
	}
	logger.Info("Fetching events",
		zap.Uint64("from", fromBlock),
		zap.Uint64("to", toBlock),
	)
	totalEvents := 0
	bar := progressbar.New(int(toBlock-fromBlock) + 1)
	bar.Describe("Fetching events")
	defer bar.Clear()
	logs, errs := el.FetchLogs(ctx, fromBlock, toBlock)
FetchEvents:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case blockLogs, ok := <-logs:
			if !ok {
				break FetchEvents
			}
			if err := insertContractEvents(ctx, logger, spec, db, eventParser, el, blockLogs.BlockNumber, blockLogs.Logs); err != nil {
				return fmt.Errorf("failed to insert contract events for block %d: %w", blockLogs.BlockNumber, err)
			}
			totalEvents += len(blockLogs.Logs)
			bar.Set(int(blockLogs.BlockNumber-fromBlock) + 1)
		}
	}
	if err := <-errs; err != nil {
		return fmt.Errorf("failed to fetch logs: %w", err)
	}
	bar.Clear()
	logger.Info("Fetched events",
		zap.Uint64("from", fromBlock),
		zap.Uint64("to", toBlock),
		zap.Int("count", totalEvents),
	)
	return nil
}

func insertContractEvents(
	ctx context.Context,
	logger *zap.Logger,
	spec beacon.Spec,
	db *sql.DB,
	eventParser *eventparser.EventParser,
	el *executionclient.ExecutionClient,
	blockNumber uint64,
	logs []types.Log,
) error {
	var blockTime time.Time
	if len(logs) > 0 {
		block, err := el.RPC().BlockByNumber(ctx, big.NewInt(int64(blockNumber)))
		if err != nil {
			return fmt.Errorf("failed to get block: %w", err)
		}
		blockTime = time.Unix(int64(block.Time()), 0).UTC()
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, log := range logs {
		abiEvent, ssvEvent, err := parseContractEvent(logger, eventParser, &log)
		if err != nil {
			logger.Error("failed to parse event", zap.String("tx_hash", log.TxHash.String()), zap.Error(err))
		}

		rawEventJSON, err := json.Marshal(log)
		if err != nil {
			return fmt.Errorf("failed to marshal raw log: %w", err)
		}
		ssvEventJSON, err := json.Marshal(ssvEvent)
		if err != nil {
			return fmt.Errorf("failed to marshal ssv event: %w", err)
		}

		dbEvent := &models.ContractEvent{
			BlockNumber:      int(log.BlockNumber),
			BlockHash:        log.BlockHash.String(),
			BlockTime:        blockTime,
			Slot:             int(spec.SlotAt(blockTime)),
			TransactionHash:  log.TxHash.String(),
			TransactionIndex: int(log.TxIndex),
			LogIndex:         int(log.Index),
			RawLog:           rawEventJSON,
			RawEvent:         ssvEventJSON,
		}
		if abiEvent != nil {
			dbEvent.EventName = abiEvent.Name
		}

		if err := dbEvent.Insert(ctx, tx, boil.Infer()); err != nil {
			return fmt.Errorf("failed to insert contract event: %w", err)
		}
	}

	_, err = models.States().UpdateAll(ctx, tx, models.M{"highest_block_number": int(blockNumber)})
	if err != nil {
		return fmt.Errorf("failed to update state: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func parseContractEvent(
	logger *zap.Logger,
	eventParser *eventparser.EventParser,
	log *types.Log,
) (*abi.Event, interface{}, error) {
	abiEvent, err := eventParser.EventByID(log.Topics[0])
	if err != nil {
		// TODO: this errors happens for some reason, does this also happen in SSV?
		return nil, nil, fmt.Errorf("failed to get event by id: %w", err)
	}
	ssvEvent, err := eventParser.ParseEvent(abiEvent, *log)
	if err != nil {
		if errors.Is(err, eventparser.ErrUnknownEvent) {
			return abiEvent, nil, nil
		}
		// TODO: does this also happen in SSV?
		return abiEvent, nil, fmt.Errorf("failed to parse event %s: %w", abiEvent.Name, err)
	}
	return abiEvent, ssvEvent, nil
}
