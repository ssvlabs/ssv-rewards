package sync

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv-rewards/pkg/rewards"
)

func TestIsPostStakingUpgrade(t *testing.T) {
	upgrade := &rewards.StakingUpgrade{Block: 100, LogIndex: 5}

	tests := []struct {
		name     string
		log      *types.Log
		upgrade  *rewards.StakingUpgrade
		expected bool
	}{
		{
			name:     "nil upgrade",
			log:      &types.Log{BlockNumber: 200, Index: 10},
			upgrade:  nil,
			expected: false,
		},
		{
			name:     "before upgrade block",
			log:      &types.Log{BlockNumber: 99, Index: 10},
			upgrade:  upgrade,
			expected: false,
		},
		{
			name:     "same block, before log_index",
			log:      &types.Log{BlockNumber: 100, Index: 4},
			upgrade:  upgrade,
			expected: false,
		},
		{
			name:     "same block, at log_index",
			log:      &types.Log{BlockNumber: 100, Index: 5},
			upgrade:  upgrade,
			expected: true,
		},
		{
			name:     "same block, after log_index",
			log:      &types.Log{BlockNumber: 100, Index: 6},
			upgrade:  upgrade,
			expected: true,
		},
		{
			name:     "after upgrade block",
			log:      &types.Log{BlockNumber: 101, Index: 0},
			upgrade:  upgrade,
			expected: true,
		},
		{
			name:     "after upgrade block, log_index irrelevant",
			log:      &types.Log{BlockNumber: 200, Index: 0},
			upgrade:  upgrade,
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, isPostStakingUpgrade(tt.log, tt.upgrade))
		})
	}
}

func TestClusterMigratedToETHTopic(t *testing.T) {
	// Hardcoded digest guards against accidental changes to the event signature in eth_migration.go.
	expected := common.HexToHash("0x6dc71e7231318932a8a7f99bac76f94ffe24cac147843e0c285ce4cf290c803a")
	require.Equal(t, expected, rewards.ClusterMigratedToETHTopic)
}

func TestClusterMigratedToETHABIDecode(t *testing.T) {
	// Encode known values using the same ABI definition.
	eventABI := clusterMigratedToETHABI

	operatorIds := []uint64{1, 2, 3, 4}
	ethDeposited := big.NewInt(1e18)
	ssvRefunded := big.NewInt(2e18)
	effectiveBalance := uint32(32)

	type clusterTuple struct {
		ValidatorCount  uint32   `json:"validatorCount"`
		NetworkFeeIndex uint64   `json:"networkFeeIndex"`
		Index           uint64   `json:"index"`
		Active          bool     `json:"active"`
		Balance         *big.Int `json:"balance"`
	}
	cluster := clusterTuple{
		ValidatorCount:  7,
		NetworkFeeIndex: 100,
		Index:           200,
		Active:          true,
		Balance:         big.NewInt(5e18),
	}

	// Pack only the non-indexed inputs (owner is indexed → in Topics).
	data, err := eventABI.Inputs.NonIndexed().Pack(
		operatorIds,
		ethDeposited,
		ssvRefunded,
		effectiveBalance,
		cluster,
	)
	require.NoError(t, err)

	// Build a synthetic log.
	owner := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	log := &types.Log{
		Topics: []common.Hash{
			rewards.ClusterMigratedToETHTopic,
			common.BytesToHash(owner.Bytes()),
		},
		Data: data,
	}

	// Decode non-indexed data (same as production code).
	decoded, err := eventABI.Inputs.Unpack(log.Data)
	require.NoError(t, err)
	require.Len(t, decoded, 5)

	// Verify operatorIds.
	gotOps, ok := decoded[0].([]uint64)
	require.True(t, ok, "operatorIds type: %T", decoded[0])
	require.Equal(t, operatorIds, gotOps)

	// Verify ethDeposited.
	gotEthDeposited, ok := decoded[1].(*big.Int)
	require.True(t, ok, "ethDeposited type: %T", decoded[1])
	require.Equal(t, ethDeposited, gotEthDeposited)

	// Verify ssvRefunded.
	gotSsvRefunded, ok := decoded[2].(*big.Int)
	require.True(t, ok, "ssvRefunded type: %T", decoded[2])
	require.Equal(t, ssvRefunded, gotSsvRefunded)

	// Verify effectiveBalance.
	gotEffectiveBalance, ok := decoded[3].(uint32)
	require.True(t, ok, "effectiveBalance type: %T", decoded[3])
	require.Equal(t, effectiveBalance, gotEffectiveBalance)

	// Verify cluster tuple via the same struct assertion used in production.
	clusterData := decoded[4]
	clusterStruct, ok := clusterData.(struct {
		ValidatorCount  uint32   `json:"validatorCount"`
		NetworkFeeIndex uint64   `json:"networkFeeIndex"`
		Index           uint64   `json:"index"`
		Active          bool     `json:"active"`
		Balance         *big.Int `json:"balance"`
	})
	require.True(t, ok, "cluster tuple type: %T", clusterData)
	require.Equal(t, uint32(7), clusterStruct.ValidatorCount)
	require.Equal(t, uint64(100), clusterStruct.NetworkFeeIndex)
	require.Equal(t, uint64(200), clusterStruct.Index)
	require.True(t, clusterStruct.Active)
	require.Equal(t, big.NewInt(5e18), clusterStruct.Balance)

	// Verify owner from Topics[1].
	gotOwner := common.BytesToAddress(log.Topics[1].Bytes())
	require.Equal(t, owner, gotOwner)
}

func TestClusterMigratedToETHABIParsing(t *testing.T) {
	// Verify the ABI JSON parses without panic and has the expected structure.
	event := clusterMigratedToETHABI

	require.Equal(t, "ClusterMigratedToETH", event.Name)
	require.Len(t, event.Inputs, 6)

	// First input: owner (indexed).
	require.Equal(t, "owner", event.Inputs[0].Name)
	require.True(t, event.Inputs[0].Indexed)
	require.Equal(t, abi.AddressTy, event.Inputs[0].Type.T)

	// Second input: operatorIds (not indexed, uint64[]).
	require.Equal(t, "operatorIds", event.Inputs[1].Name)
	require.False(t, event.Inputs[1].Indexed)
	require.Equal(t, abi.SliceTy, event.Inputs[1].Type.T)

	// Last input: cluster tuple (not indexed).
	require.Equal(t, "cluster", event.Inputs[5].Name)
	require.False(t, event.Inputs[5].Indexed)
	require.Equal(t, abi.TupleTy, event.Inputs[5].Type.T)
}
