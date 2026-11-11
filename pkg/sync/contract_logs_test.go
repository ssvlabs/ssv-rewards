package sync

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var testContract = common.HexToAddress("0xDD9BC35aE942eF0cFa76930954a156B3fF30a4E1")

// cappedFilterer mimics an RPC node that refuses eth_getLogs responses with
// more than maxLogs entries, the way Nethermind does with MaxLogsPerResponse.
type cappedFilterer struct {
	logs    []types.Log
	maxLogs int
	err     error // when set, returned for every query
	queries [][2]uint64
}

func (f *cappedFilterer) FilterLogs(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	from, to := q.FromBlock.Uint64(), q.ToBlock.Uint64()
	f.queries = append(f.queries, [2]uint64{from, to})
	if len(q.Addresses) != 1 || q.Addresses[0] != testContract {
		return nil, fmt.Errorf("unexpected address filter: %v", q.Addresses)
	}
	if f.err != nil {
		return nil, f.err
	}
	var out []types.Log
	for _, l := range f.logs {
		if l.BlockNumber >= from && l.BlockNumber <= to {
			out = append(out, l)
		}
	}
	if len(out) > f.maxLogs {
		return nil, rpc.HTTPError{
			StatusCode: 503,
			Status:     "503 Service Unavailable",
			Body: []byte(fmt.Sprintf(
				`{"jsonrpc":"2.0","error":{"code":-32005,"message":"Too many logs requested. Max logs per response is %d."},"id":1}`,
				f.maxLogs,
			)),
		}
	}
	return out, nil
}

// makeLogs creates count logs in each of the given blocks.
func makeLogs(countPerBlock int, blocks ...uint64) []types.Log {
	var logs []types.Log
	for _, b := range blocks {
		for i := 0; i < countPerBlock; i++ {
			logs = append(logs, types.Log{
				Address:     testContract,
				BlockNumber: b,
				Index:       uint(i),
				TxHash:      common.BigToHash(common.Big1),
			})
		}
	}
	return logs
}

type collected struct {
	blocks []uint64
	logs   []types.Log
}

func (c *collected) emit(bl executionclient.BlockLogs) error {
	c.blocks = append(c.blocks, bl.BlockNumber)
	c.logs = append(c.logs, bl.Logs...)
	return nil
}

func TestFetchContractLogs_SplitsWindowWhenProviderCapsLogs(t *testing.T) {
	// 10 blocks × 3 logs = 30 logs in [100,109], but the node returns at most 5.
	var burst []uint64
	for b := uint64(100); b <= 109; b++ {
		burst = append(burst, b)
	}
	all := makeLogs(3, burst...)
	f := &cappedFilterer{logs: all, maxLogs: 5}

	var c collected
	err := fetchContractLogs(context.Background(), zap.NewNop(), f, testContract, 0, 199, 100, c.emit)
	require.NoError(t, err)

	require.Equal(t, all, c.logs, "every log must be emitted exactly once, in order")
	for i := 1; i < len(c.blocks); i++ {
		require.Greater(t, c.blocks[i], c.blocks[i-1], "emitted blocks must be strictly increasing")
	}
	require.Equal(t, uint64(199), c.blocks[len(c.blocks)-1], "must report progress up to the last block")
}

func TestFetchContractLogs_GrowsWindowBackAfterBurst(t *testing.T) {
	f := &cappedFilterer{logs: makeLogs(4, 10, 11), maxLogs: 5}

	var c collected
	err := fetchContractLogs(context.Background(), zap.NewNop(), f, testContract, 0, 999, 100, c.emit)
	require.NoError(t, err)

	var widestAfterBurst uint64
	for _, q := range f.queries {
		if q[0] > 11 && q[1]-q[0]+1 > widestAfterBurst {
			widestAfterBurst = q[1] - q[0] + 1
		}
	}
	require.Equal(t, uint64(100), widestAfterBurst, "window should return to the max batch size after the burst")
	require.Less(t, len(f.queries), 40, "should not keep crawling with tiny windows")
	require.Len(t, c.logs, 8)
}

func TestFetchContractLogs_NoSplitWhenUnderLimit(t *testing.T) {
	f := &cappedFilterer{logs: makeLogs(2, 5, 150), maxLogs: 100}

	var c collected
	err := fetchContractLogs(context.Background(), zap.NewNop(), f, testContract, 0, 249, 100, c.emit)
	require.NoError(t, err)

	require.Equal(t, [][2]uint64{{0, 99}, {100, 199}, {200, 249}}, f.queries)
	require.Equal(t, []uint64{5, 150, 249}, c.blocks)
}

func TestFetchContractLogs_FailsWhenSingleBlockExceedsLimit(t *testing.T) {
	f := &cappedFilterer{logs: makeLogs(6, 42), maxLogs: 5}

	var c collected
	err := fetchContractLogs(context.Background(), zap.NewNop(), f, testContract, 0, 99, 100, c.emit)
	require.Error(t, err)
	require.Contains(t, err.Error(), "block 42")
	require.Contains(t, err.Error(), "Too many logs")
	require.Empty(t, c.logs, "nothing from the failing window may be emitted")
}

func TestFetchContractLogs_DoesNotSplitOnOtherErrors(t *testing.T) {
	boom := errors.New("connection refused")
	f := &cappedFilterer{err: boom, maxLogs: 5}

	var c collected
	err := fetchContractLogs(context.Background(), zap.NewNop(), f, testContract, 0, 99, 100, c.emit)
	require.ErrorIs(t, err, boom)
	require.Len(t, f.queries, 1, "unrelated errors must not trigger retries")
}

func TestFetchContractLogs_StopsOnEmitError(t *testing.T) {
	f := &cappedFilterer{logs: makeLogs(1, 1, 150), maxLogs: 100}
	boom := errors.New("db down")

	err := fetchContractLogs(context.Background(), zap.NewNop(), f, testContract, 0, 199, 100,
		func(executionclient.BlockLogs) error { return boom })
	require.ErrorIs(t, err, boom)
	require.Len(t, f.queries, 1)
}

func TestIsTooManyLogsError(t *testing.T) {
	for _, msg := range []string{
		`503 Service Unavailable: {"error":{"code":-32005,"message":"Too many logs requested. Max logs per response is 20000."}}`,
		"query returned more than 10000 results",
		"Log response size exceeded. You can make eth_getLogs requests with up to a 2K block range",
	} {
		require.True(t, isTooManyLogsError(errors.New(msg)), msg)
	}
	for _, msg := range []string{
		"connection refused",
		`{"code":-32005,"message":"daily request count exceeded, request rate limited"}`,
	} {
		require.False(t, isTooManyLogsError(errors.New(msg)), msg)
	}
}
