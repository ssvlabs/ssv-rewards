package sync

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/bloxapp/ssv/eth/eventparser"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv-rewards/pkg/models"
	"github.com/bloxapp/ssv-rewards/pkg/rewards"
)

// isPostStakingUpgrade returns true if the log is at or after the configured upgrade boundary.
func isPostStakingUpgrade(log *types.Log, upgrade *rewards.StakingUpgrade) bool {
	if upgrade == nil {
		return false
	}
	blockNum := int(log.BlockNumber)
	return blockNum > upgrade.Block ||
		(blockNum == upgrade.Block && int(log.Index) >= upgrade.LogIndex)
}

// clusterMigratedToETHJSON is the JSON ABI for the ClusterMigratedToETH event.
const clusterMigratedToETHJSON = `[{
	"anonymous": false,
	"inputs": [
		{"indexed": true,  "name": "owner",            "type": "address"},
		{"indexed": false, "name": "operatorIds",      "type": "uint64[]"},
		{"indexed": false, "name": "ethDeposited",     "type": "uint256"},
		{"indexed": false, "name": "ssvRefunded",      "type": "uint256"},
		{"indexed": false, "name": "effectiveBalance", "type": "uint32"},
		{"indexed": false, "name": "cluster",          "type": "tuple",
		 "components": [
			{"name": "validatorCount",  "type": "uint32"},
			{"name": "networkFeeIndex", "type": "uint64"},
			{"name": "index",           "type": "uint64"},
			{"name": "active",          "type": "bool"},
			{"name": "balance",         "type": "uint256"}
		]}
	],
	"name": "ClusterMigratedToETH",
	"type": "event"
}]`

var clusterMigratedToETHABI = func() abi.Event {
	parsed, err := abi.JSON(strings.NewReader(clusterMigratedToETHJSON))
	if err != nil {
		panic(fmt.Sprintf("parse ClusterMigratedToETH ABI: %v", err))
	}
	return parsed.Events["ClusterMigratedToETH"]
}()

// handleClusterMigration processes a ClusterMigratedToETH event by setting
// migration_day on all validators in the cluster and inserting validator_events
// records.
func handleClusterMigration(
	ctx context.Context,
	logger *zap.Logger,
	db *sql.DB,
	log *types.Log,
	contractEvent *models.ContractEvent,
	migratedClusters map[string]struct{},
) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("malformed ClusterMigratedToETH log: expected >= 2 topics, got %d", len(log.Topics))
	}
	owner := common.BytesToAddress(log.Topics[1].Bytes())

	decoded, err := clusterMigratedToETHABI.Inputs.Unpack(log.Data)
	if err != nil {
		return fmt.Errorf("decode ClusterMigratedToETH data: %w", err)
	}
	operatorIds := decoded[0].([]uint64)

	// validatorCount lives in the cluster tuple (5th non-indexed input, first field).
	clusterStruct, ok := decoded[4].(struct {
		ValidatorCount  uint32   `json:"validatorCount"`
		NetworkFeeIndex uint64   `json:"networkFeeIndex"`
		Index           uint64   `json:"index"`
		Active          bool     `json:"active"`
		Balance         *big.Int `json:"balance"`
	})
	if !ok {
		return fmt.Errorf("unexpected cluster tuple type: %T", decoded[4])
	}
	validatorCount := clusterStruct.ValidatorCount

	clusterID, err := ssvtypes.ComputeClusterIDHash(owner.Bytes(), operatorIds)
	if err != nil {
		return fmt.Errorf("compute cluster ID: %w", err)
	}
	clusterIDStr := hex.EncodeToString(clusterID)

	if _, exists := migratedClusters[clusterIDStr]; exists {
		logger.Warn("duplicate ClusterMigratedToETH in same sync run",
			zap.String("cluster_id", clusterIDStr),
			zap.Uint64("block_number", log.BlockNumber),
		)
		return nil
	}
	migratedClusters[clusterIDStr] = struct{}{}

	members, err := computeClusterMembersAt(
		ctx, db, owner, clusterID, int(log.BlockNumber), int(log.Index),
	)
	if err != nil {
		return fmt.Errorf("compute cluster members at migration position: %w", err)
	}

	if len(members) == 0 && validatorCount == 0 {
		logger.Warn("migrated cluster has no validators",
			zap.String("cluster_id", clusterIDStr),
			zap.String("owner", strings.ToLower(owner.Hex())),
		)
		return nil
	} else if len(members) != int(validatorCount) {
		logger.Warn("local member count differs from on-chain validatorCount",
			zap.String("cluster_id", clusterIDStr),
			zap.Int("local_members", len(members)),
			zap.Uint32("chain_validator_count", validatorCount))
	}

	migrationDay := contractEvent.BlockTime.UTC().Truncate(24 * time.Hour)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, pubKey := range members {
		// IS NULL guard preserves an earlier migration_day if the validator
		// already transitioned to the ETH tree via a prior cluster.
		_, err := models.Validators(
			models.ValidatorWhere.PublicKey.EQ(pubKey),
			models.ValidatorWhere.MigrationDay.IsNull(),
		).UpdateAll(ctx, tx, models.M{
			models.ValidatorColumns.MigrationDay: migrationDay,
		})
		if err != nil {
			return fmt.Errorf("set migration_day: %w", err)
		}

		validatorEvent := models.ValidatorEvent{
			ContractEventID: contractEvent.ID,
			Slot:            contractEvent.Slot,
			BlockNumber:     int(log.BlockNumber),
			BlockTime:       contractEvent.BlockTime,
			LogIndex:        int(log.Index),
			EventName:       rewards.ClusterMigratedToETHEvent,
			OwnerAddress:    strings.ToLower(owner.Hex()[2:]),
			PublicKey:       pubKey,
			Activated:       true,
		}
		if err := validatorEvent.Insert(ctx, tx, boil.Infer()); err != nil {
			return fmt.Errorf("insert validator event: %w", err)
		}
	}

	return tx.Commit()
}

// computeClusterMembersAt returns the public keys of validators in the cluster
// identified by targetClusterID at the chain position (migBlock, migLogIndex),
// derived from validator_events ordered by (block_number, log_index).
//
// A pubkey is a member if the latest of its ValidatorAdded/ValidatorRemoved
// events whose cluster_id matches targetClusterID and whose position is strictly
// before (migBlock, migLogIndex) is a ValidatorAdded.
func computeClusterMembersAt(
	ctx context.Context,
	db *sql.DB,
	owner common.Address,
	targetClusterID []byte,
	migBlock, migLogIndex int,
) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT ve.public_key, ve.event_name,
		       ce.raw_event::jsonb -> 'OperatorIds' AS op_ids
		FROM validator_events ve
		JOIN contract_events ce ON ce.id = ve.contract_event_id
		WHERE ve.owner_address = $1
		  AND ve.event_name IN ('ValidatorAdded', 'ValidatorRemoved')
		  AND (ve.block_number < $2
		       OR (ve.block_number = $2 AND ve.log_index < $3))
		ORDER BY ve.block_number, ve.log_index
	`,
		strings.ToLower(owner.Hex()[2:]),
		migBlock, migLogIndex,
	)
	if err != nil {
		return nil, fmt.Errorf("query cluster member events: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	events := make([]clusterMemberEvent, 0)
	for rows.Next() {
		var (
			pubKey, eventName string
			opIdsJSON         []byte
		)
		if err := rows.Scan(&pubKey, &eventName, &opIdsJSON); err != nil {
			return nil, fmt.Errorf("scan cluster member event: %w", err)
		}

		var ops []uint64
		if err := json.Unmarshal(opIdsJSON, &ops); err != nil {
			return nil, fmt.Errorf("unmarshal operator ids: %w", err)
		}

		events = append(events, clusterMemberEvent{
			PublicKey: pubKey,
			EventName: eventName,
			OperatorIds: ops,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster member events: %w", err)
	}

	return reduceClusterMembers(owner, targetClusterID, events)
}

// clusterMemberEvent is a decoded ValidatorAdded or ValidatorRemoved event,
// shaped for consumption by reduceClusterMembers without a DB or ABI dependency.
type clusterMemberEvent struct {
	PublicKey   string
	EventName   string
	OperatorIds []uint64
}

// reduceClusterMembers walks events in order and returns the public keys
// currently in targetClusterID: a pubkey is a member if its latest event
// whose computed cluster_id equals targetClusterID is a ValidatorAdded.
func reduceClusterMembers(
	owner common.Address,
	targetClusterID []byte,
	events []clusterMemberEvent,
) ([]string, error) {
	members := map[string]struct{}{}
	for _, ev := range events {
		// ComputeClusterIDHash sorts operatorIds in place; copy to avoid
		// mutating the caller's slice.
		ops := append([]uint64(nil), ev.OperatorIds...)
		cid, err := ssvtypes.ComputeClusterIDHash(owner.Bytes(), ops)
		if err != nil {
			return nil, fmt.Errorf("compute cluster id: %w", err)
		}
		if !bytes.Equal(cid, targetClusterID) {
			continue
		}

		switch ev.EventName {
		case eventparser.ValidatorAdded:
			members[ev.PublicKey] = struct{}{}
		case eventparser.ValidatorRemoved:
			delete(members, ev.PublicKey)
		}
	}

	out := make([]string, 0, len(members))
	for pk := range members {
		out = append(out, pk)
	}
	return out, nil
}
