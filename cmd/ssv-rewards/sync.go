package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/auto"
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv-rewards/pkg/beacon"
	"github.com/bloxapp/ssv-rewards/pkg/models"
	"github.com/bloxapp/ssv-rewards/pkg/rewards"
	"github.com/bloxapp/ssv-rewards/pkg/sync"
	"github.com/bloxapp/ssv-rewards/pkg/sync/performance"
	"github.com/bloxapp/ssv-rewards/pkg/sync/performance/beaconcha"
	"github.com/bloxapp/ssv-rewards/pkg/sync/performance/e2m"
)

type SyncCmd struct {
	DataDir                    string  `env:"DATA_DIR"                       default:"./data"               help:"Path to the data directory."`
	ExecutionEndpoint          string  `env:"EXECUTION_ENDPOINT"                                            help:"RPC endpoint to an Ethereum execution node."                                        required:""`
	ConsensusEndpoint          string  `env:"CONSENSUS_ENDPOINT"                                            help:"HTTP endpoint to an Ethereum Beacon node API."                                      required:""`
	SSVAPIEndpoint             string  `env:"SSV_API_ENDPOINT"                                              help:"HTTP endpoint to an SSV API."                                                       required:""`
	E2MEndpoint                string  `env:"E2M_ENDPOINT"                                                  help:"HTTP endpoint to an ethereum2-monitor API."                                         required:"" xor:"monitoring-endpoint" name:"e2m-endpoint"`
	BeaconchaEndpoint          string  `env:"BEACONCHA_ENDPOINT"             default:"https://beaconcha.in" help:"HTTP endpoint to a beaconcha.in API."                                               required:"" xor:"monitoring-endpoint"`
	BeaconchaAPIKey            string  `env:"BEACONCHA_API_KEY"                                             help:"API key for beaconcha.in API."`
	BeaconchaRequestsPerMinute float64 `env:"BEACONCHA_REQUESTS_PER_MINUTE"  default:"20"                   help:"Maximum number of requests per minute to beaconcha.in API."`
	HighestExecutionBlock      uint64  `env:"HIGHEST_EXECUTION_BLOCK"                                       help:"Execution block number to end syncing at. Defaults to the highest finalized block."`
	Fresh                      bool    `env:"FRESH"                                                         help:"Delete all data and start from scratch."`
	FreshSSV                   bool    `env:"FRESH_SSV"                                                     help:"Delete all SSV data and start from scratch."`
}

func (c *SyncCmd) Run(
	logger *zap.Logger,
	db *sql.DB,
	network networkconfig.NetworkConfig,
	plan *rewards.Plan,
) error {
	ctx := context.Background()

	dataDir := filepath.Join(c.DataDir, network.Name)
	logger.Info(
		"Starting ssv-rewards",
		zap.String("network", network.Name),
		zap.String("data_dir", dataDir),
	)

	// Start from scratch, if requested.
	if c.Fresh {
		// Drop schema.
		if _, err := db.ExecContext(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
			return fmt.Errorf("failed to drop schema: %w", err)
		}
		logger.Info("Dropped PostgreSQL schema")
	}

	// Create tables if they don't exist.
	schemaSQL, err := os.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("failed to read schema.sql: %w", err)
	}
	if _, err := db.ExecContext(ctx, string(schemaSQL)); err != nil {
		return fmt.Errorf("failed to execute schema.sql: %w", err)
	}
	logger.Info("Applied PostgreSQL schema")

	if c.FreshSSV || c.Fresh {
		// Empty the dataDir.
		if err := os.RemoveAll(dataDir); err != nil {
			return fmt.Errorf("failed to remove data dir: %w", err)
		}

		// Truncate SSV-related tables.
		truncate := `
			TRUNCATE TABLE validators CASCADE;
			TRUNCATE TABLE validator_events CASCADE;
			TRUNCATE TABLE validator_performances CASCADE;
		`
		if _, err := db.ExecContext(ctx, truncate); err != nil {
			return fmt.Errorf("failed to truncate validator_events: %w", err)
		}

		// Unset recorded errors in the contract_events table.
		if _, err := db.ExecContext(ctx, "UPDATE contract_events SET error = NULL"); err != nil {
			return fmt.Errorf("failed to unset contract_events.error: %w", err)
		}
	}

	// Open SSV DB.
	ssvDB, err := kv.New(logger, basedb.Options{
		Ctx:  ctx,
		Path: filepath.Join(dataDir, "ssv-node-storage"),
	})
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}
	nodeStorage, err := storage.NewNodeStorage(logger, ssvDB)
	if err != nil {
		return fmt.Errorf("failed to create node storage: %w", err)
	}

	// Connect to execution node.
	el, err := executionclient.New(
		ctx,
		c.ExecutionEndpoint,
		common.HexToAddress(network.RegistryContractAddr),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to execution node: %w", err)
	}

	eventFilterer, err := el.Filterer()
	if err != nil {
		return fmt.Errorf("failed to create event filterer: %w", err)
	}
	eventParser := eventparser.New(eventFilterer)

	logger.Info("Connected to execution node", zap.String("endpoint", c.ExecutionEndpoint))

	// Connect to consensus node.
	cl, err := auto.New(
		ctx,
		auto.WithAddress(c.ConsensusEndpoint),
		auto.WithLogLevel(zerolog.ErrorLevel),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to consensus node: %w", err)
	}
	genesisTime, err := cl.(eth2client.GenesisTimeProvider).GenesisTime(ctx)
	if err != nil {
		return fmt.Errorf("failed to get genesis time: %w", err)
	}
	spec := beacon.Spec{
		Network:        network.Beacon.GetNetwork().String(),
		GenesisTime:    genesisTime.UTC(),
		SlotsPerEpoch:  32,
		SlotDuration:   12 * time.Second,
		FarFutureEpoch: math.MaxUint64,
	}
	logger.Info("Connected to consensus node", zap.String("endpoint", c.ConsensusEndpoint))

	// Derive fromBlock and toBlock.
	fromBlock := network.RegistrySyncOffset.Uint64()
	toBlock := c.HighestExecutionBlock

	finalizedBlock, err := el.RPC().
		BlockByNumber(ctx, new(big.Int).SetInt64(rpc.FinalizedBlockNumber.Int64()))
	if err != nil {
		return fmt.Errorf("failed to get current block number: %w", err)
	}
	if toBlock == 0 {
		toBlock = finalizedBlock.Number().Uint64()
	} else if toBlock > finalizedBlock.Number().Uint64() {
		return fmt.Errorf("--to-block is not yet finalized")
	}

	// Create or verify the state of the database.
	state, err := models.States().One(ctx, db)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to get state: %w", err)
		}
		state = &models.State{
			NetworkName:        network.Name,
			LowestBlockNumber:  int(fromBlock),
			HighestBlockNumber: int(toBlock),
		}
		if err := state.Insert(ctx, db, boil.Infer()); err != nil {
			return fmt.Errorf("failed to insert state: %w", err)
		}
	} else {
		if state.NetworkName != network.Name {
			return fmt.Errorf("database is already synced with %s, want %s", state.NetworkName, network.Name)
		}
		if state.LowestBlockNumber != int(fromBlock) {
			return fmt.Errorf("database is already synced from block %d, want %d", state.LowestBlockNumber, fromBlock)
		}
		fromBlock = uint64(state.HighestBlockNumber) + 1
	}

	// Sync contract events.
	if fromBlock <= toBlock {
		err = sync.SyncContractEvents(
			ctx,
			logger,
			spec,
			eventParser,
			el,
			db,
			fromBlock,
			toBlock,
		)
		if err != nil {
			return fmt.Errorf("failed to sync contract events: %w", err)
		}
	}

	// Sync validator events.
	err = sync.SyncValidatorEvents(
		ctx,
		logger,
		network,
		eventParser,
		nodeStorage,
		db,
		cl,
	)
	if err != nil {
		return fmt.Errorf("failed to sync validator events: %w", err)
	}

	// Sync validator performance.
	var performanceProvider performance.Provider
	switch {
	case c.E2MEndpoint != "":
		performanceProvider = e2m.New(c.E2MEndpoint)
	case c.BeaconchaEndpoint != "":
		performanceProvider, err = beaconcha.New(
			c.BeaconchaEndpoint,
			c.BeaconchaAPIKey,
			float64(c.BeaconchaRequestsPerMinute)*0.666, // Safety margin.
			filepath.Join(dataDir, ".cache", "beaconcha"),
		)
		if err != nil {
			return fmt.Errorf("failed to create beaconcha client: %w", err)
		}
	default:
		return fmt.Errorf("either e2m-endpoint or beaconcha-endpoint must be provided")
	}

	ssvCacheDir := filepath.Join(dataDir, ".cache", "ssv")
	if err := os.MkdirAll(ssvCacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create SSV cache directory: %w", err)
	}
	err = sync.SyncValidatorPerformance(
		ctx,
		logger,
		spec,
		el.RPC(),
		db,
		c.SSVAPIEndpoint,
		performanceProvider,
		plan.Rounds[0].Period.FirstDay(),
		plan.Rounds[len(plan.Rounds)-1].Period.LastDay(),
		toBlock,
		filepath.Join(dataDir, ".cache", "ssv"),
	)
	if err != nil {
		return fmt.Errorf("failed to sync validator performance: %w", err)
	}

	return nil
}
