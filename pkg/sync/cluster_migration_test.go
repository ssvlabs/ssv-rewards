package sync

import (
	"math/big"
	"sort"
	"testing"

	"github.com/bloxapp/ssv/eth/eventparser"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
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

// Test fixtures for reduceClusterMembers.
//
// The owner under test owns two clusters (A and B) with disjoint operator sets;
// the targetClusterID below points at cluster A. Cluster B's events should
// always be skipped by the reducer.
var (
	rcmOwner = common.HexToAddress("0x38e3df07e1ff499393fcd918c661ca8e7b00f53c")
	rcmOpsA  = []uint64{2222, 2224, 2226, 2227}
	rcmOpsB  = []uint64{2222, 2223, 2224, 2225}

	pkV1 = "ab" + "00" + "11"   // any non-empty distinct strings; reducer doesn't care about format
	pkV2 = "cd" + "22" + "33"
	pkV3 = "ef" + "44" + "55"
)

func rcmTargetClusterID(t *testing.T) []byte {
	t.Helper()
	cid, err := ssvtypes.ComputeClusterIDHash(rcmOwner.Bytes(), append([]uint64{}, rcmOpsA...))
	require.NoError(t, err)
	return cid
}

func add(pubkey string, ops []uint64) clusterMemberEvent {
	return clusterMemberEvent{PublicKey: pubkey, EventName: eventparser.ValidatorAdded, OperatorIds: ops}
}

func rem(pubkey string, ops []uint64) clusterMemberEvent {
	return clusterMemberEvent{PublicKey: pubkey, EventName: eventparser.ValidatorRemoved, OperatorIds: ops}
}

func TestReduceClusterMembers(t *testing.T) {
	target := rcmTargetClusterID(t)

	tests := []struct {
		name     string
		events   []clusterMemberEvent
		expected []string
	}{
		{
			name:     "empty input → empty members",
			events:   nil,
			expected: []string{},
		},
		{
			name: "add before mig → in members",
			events: []clusterMemberEvent{
				add(pkV1, rcmOpsA),
			},
			expected: []string{pkV1},
		},
		{
			name: "add then remove before mig → empty members",
			events: []clusterMemberEvent{
				add(pkV1, rcmOpsA),
				rem(pkV1, rcmOpsA),
			},
			expected: []string{},
		},
		{
			name: "add, remove, re-add to same cluster → in members",
			events: []clusterMemberEvent{
				add(pkV1, rcmOpsA),
				rem(pkV1, rcmOpsA),
				add(pkV1, rcmOpsA),
			},
			expected: []string{pkV1},
		},
		{
			name: "duplicate adds (same pubkey, same cluster) → idempotent, single membership",
			events: []clusterMemberEvent{
				add(pkV1, rcmOpsA),
				add(pkV1, rcmOpsA),
			},
			expected: []string{pkV1},
		},
		{
			name: "events for a different cluster (B) are ignored",
			events: []clusterMemberEvent{
				add(pkV2, rcmOpsB),
				rem(pkV2, rcmOpsB),
				add(pkV3, rcmOpsB),
			},
			expected: []string{},
		},
		{
			name: "interleaved A and B events — only A counted",
			events: []clusterMemberEvent{
				add(pkV1, rcmOpsA), // counted, V1 in A
				add(pkV2, rcmOpsB), // skipped
				add(pkV3, rcmOpsA), // counted, V3 in A
				rem(pkV2, rcmOpsB), // skipped
			},
			expected: []string{pkV1, pkV3},
		},
		{
			name: "operator IDs in non-sorted order still match (ComputeClusterIDHash sorts)",
			events: []clusterMemberEvent{
				add(pkV1, []uint64{rcmOpsA[3], rcmOpsA[0], rcmOpsA[2], rcmOpsA[1]}),
			},
			expected: []string{pkV1},
		},
		{
			name: "remove before any add (chain shouldn't emit this, but be defensive) → empty",
			events: []clusterMemberEvent{
				rem(pkV1, rcmOpsA),
			},
			expected: []string{},
		},
		{
			name: "mix: V1 added in A, V2 added then removed in A, V3 added in B (ignored)",
			events: []clusterMemberEvent{
				add(pkV1, rcmOpsA),
				add(pkV2, rcmOpsA),
				rem(pkV2, rcmOpsA),
				add(pkV3, rcmOpsB),
			},
			expected: []string{pkV1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := reduceClusterMembers(rcmOwner, target, tt.events)
			require.NoError(t, err)

			// Map iteration is nondeterministic; sort both sides for comparison.
			sort.Strings(got)
			expected := append([]string{}, tt.expected...)
			sort.Strings(expected)
			require.Equal(t, expected, got)
		})
	}
}

// TestReduceClusterMembers_OperatorIdsSliceNotMutated confirms the reducer
// doesn't mutate the caller's OperatorIds slice. Important because
// ssvtypes.ComputeClusterIDHash sorts in place; we pass a defensive copy.
func TestReduceClusterMembers_OperatorIdsSliceNotMutated(t *testing.T) {
	target := rcmTargetClusterID(t)

	// Pass a deliberately unsorted slice. Capture what we pass; assert it
	// remains unchanged after the reducer runs.
	unsorted := []uint64{rcmOpsA[3], rcmOpsA[0], rcmOpsA[2], rcmOpsA[1]}
	original := append([]uint64{}, unsorted...)

	_, err := reduceClusterMembers(rcmOwner, target, []clusterMemberEvent{
		add(pkV1, unsorted),
	})
	require.NoError(t, err)
	require.Equal(t, original, unsorted, "caller's OperatorIds slice was mutated")
}
