package sync

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
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

// handleClusterMigration processes a ClusterMigratedToETH event by setting migration_day
// on all validators in the cluster and inserting validator_events records.
func handleClusterMigration(
	ctx context.Context,
	logger *zap.Logger,
	db *sql.DB,
	nodeStorage operatorstorage.Storage,
	log *types.Log,
	contractEvent *models.ContractEvent,
	migratedClusters map[string]struct{},
) error {
	if len(log.Topics) < 2 {
		return fmt.Errorf("malformed ClusterMigratedToETH log: expected >= 2 topics, got %d", len(log.Topics))
	}

	// Parse owner from Topics[1] (indexed address, padded to 32 bytes).
	owner := common.BytesToAddress(log.Topics[1].Bytes())

	// Decode non-indexed event data.
	decoded, err := clusterMigratedToETHABI.Inputs.Unpack(log.Data)
	if err != nil {
		return fmt.Errorf("decode ClusterMigratedToETH data: %w", err)
	}
	operatorIds := decoded[0].([]uint64)

	// Decode validatorCount from cluster tuple (6th event param, first field).
	clusterData := decoded[4]
	clusterStruct, ok := clusterData.(struct {
		ValidatorCount  uint32   `json:"validatorCount"`
		NetworkFeeIndex uint64   `json:"networkFeeIndex"`
		Index           uint64   `json:"index"`
		Active          bool     `json:"active"`
		Balance         *big.Int `json:"balance"`
	})
	if !ok {
		return fmt.Errorf("unexpected cluster tuple type: %T", clusterData)
	}
	validatorCount := clusterStruct.ValidatorCount

	// Compute cluster ID.
	clusterID, err := ssvtypes.ComputeClusterIDHash(owner.Bytes(), operatorIds)
	if err != nil {
		return fmt.Errorf("compute cluster ID: %w", err)
	}
	clusterIDStr := hex.EncodeToString(clusterID)

	// In-memory dedup: warn on duplicate within same sync run.
	if _, exists := migratedClusters[clusterIDStr]; exists {
		logger.Warn("duplicate ClusterMigratedToETH in same sync run (A4 invariant)",
			zap.String("cluster_id", clusterIDStr),
			zap.Uint64("block_number", log.BlockNumber),
		)
		return nil
	}
	migratedClusters[clusterIDStr] = struct{}{}

	// Look up affected validators.
	shares := nodeStorage.Shares().List(nil, registrystorage.ByClusterID(clusterID))

	if len(shares) == 0 && validatorCount == 0 {
		logger.Warn("migrated cluster has no validators",
			zap.String("cluster_id", clusterIDStr),
			zap.String("owner", strings.ToLower(owner.Hex())),
		)
	} else if len(shares) != int(validatorCount) {
		logger.Warn("share count differs from on-chain validatorCount",
			zap.String("cluster_id", clusterIDStr),
			zap.Int("local_shares", len(shares)),
			zap.Uint32("chain_validator_count", validatorCount))
	}

	migrationDay := contractEvent.BlockTime.UTC().Truncate(24 * time.Hour)

	// Wrap all DB writes in a single transaction.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, share := range shares {
		pubKey := hex.EncodeToString(share.ValidatorPubKey)

		// Set migration_day. IS NULL guard prevents overwriting on cross-run duplicates.
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
