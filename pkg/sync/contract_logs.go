package sync

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// maxLogBatchSize is the widest block window requested per eth_getLogs call.
// It matches executionclient.DefaultHistoricalLogsBatchSize, which this sync
// used before adaptive splitting was added.
const maxLogBatchSize = executionclient.DefaultHistoricalLogsBatchSize

type logFilterer interface {
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
}

// fetchContractLogs fetches the contract's logs in [fromBlock, toBlock] and
// passes them to emit grouped by block, in ascending block order.
//
// When the RPC node refuses a window because it holds too many logs, the
// window is halved and retried; after each successful window it doubles again,
// up to maxBatchSize. The set of logs emitted does not depend on the window
// sizes used, only on the requested range.
//
// As in executionclient.FetchLogs, a window without logs emits an empty
// BlockLogs for its last block so callers can record progress.
func fetchContractLogs(
	ctx context.Context,
	logger *zap.Logger,
	client logFilterer,
	contract common.Address,
	fromBlock, toBlock, maxBatchSize uint64,
	emit func(executionclient.BlockLogs) error,
) error {
	if toBlock < fromBlock {
		return fmt.Errorf("from block (%d) cannot be greater than to block (%d)", fromBlock, toBlock)
	}
	if maxBatchSize == 0 {
		return fmt.Errorf("max batch size must be positive")
	}

	batch := maxBatchSize
	cursor := fromBlock
	for cursor <= toBlock {
		if err := ctx.Err(); err != nil {
			return err
		}

		end := toBlock
		if toBlock-cursor >= batch {
			end = cursor + batch - 1
		}

		logs, err := client.FilterLogs(ctx, ethereum.FilterQuery{
			Addresses: []common.Address{contract},
			FromBlock: new(big.Int).SetUint64(cursor),
			ToBlock:   new(big.Int).SetUint64(end),
		})
		if err != nil {
			if !isTooManyLogsError(err) {
				return fmt.Errorf("failed to fetch logs for blocks %d-%d: %w", cursor, end, err)
			}
			if end == cursor {
				return fmt.Errorf("block %d alone exceeds the RPC node's log limit: %w", cursor, err)
			}
			batch = (end - cursor + 1) / 2
			logger.Warn("RPC node log limit hit, splitting block window",
				zap.Uint64("from", cursor),
				zap.Uint64("to", end),
				zap.Uint64("new_batch_size", batch),
			)
			continue
		}

		valid := make([]types.Log, 0, len(logs))
		for _, l := range logs {
			if l.Removed {
				// Shouldn't happen for finalized blocks; skipped like executionclient.FetchLogs does.
				logger.Warn("log is removed",
					zap.String("block_hash", l.BlockHash.Hex()),
					zap.String("tx_hash", l.TxHash.Hex()),
					zap.Uint("log_index", l.Index),
				)
				continue
			}
			valid = append(valid, l)
		}
		if len(valid) == 0 {
			if err := emit(executionclient.BlockLogs{BlockNumber: end}); err != nil {
				return err
			}
		} else {
			for _, bl := range executionclient.PackLogs(valid) {
				if err := emit(bl); err != nil {
					return err
				}
			}
		}

		cursor = end + 1
		if batch < maxBatchSize {
			batch = min(batch*2, maxBatchSize)
		}
	}
	return nil
}

// tooManyLogsMessages are substrings (lowercase) of errors that providers
// return when an eth_getLogs response would be too large. Deliberately narrow:
// rate-limit errors share JSON-RPC code -32005 but must not trigger a split.
var tooManyLogsMessages = []string{
	"too many logs",              // Nethermind: "Too many logs requested. Max logs per response is N."
	"query returned more than",   // Geth/Infura: "query returned more than 10000 results"
	"log response size exceeded", // Alchemy
}

func isTooManyLogsError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, s := range tooManyLogsMessages {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
