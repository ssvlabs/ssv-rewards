package sync

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bloxapp/ssv/networkconfig"
	registrystorage "github.com/bloxapp/ssv/registry/storage"
	"github.com/carlmjohnson/requests"
	"golang.org/x/exp/maps"

	eth2client "github.com/attestantio/go-eth2-client"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-rewards/pkg/models"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/eth/contract"
	"github.com/bloxapp/ssv/eth/eventhandler"
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/bloxapp/ssv/eth/executionclient"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/schollz/progressbar/v3"
	"github.com/sourcegraph/conc/pool"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
	"go.uber.org/zap"

	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

func SyncValidatorEvents(
	ctx context.Context,
	logger *zap.Logger,
	network networkconfig.NetworkConfig,
	eventParser *eventparser.EventParser,
	nodeStorage operatorstorage.Storage,
	db *sql.DB,
	ethClient *ethclient.Client,
	cl eth2client.Service,
) error {
	// Fetch events from the database, organize into a channel of BlockLogs
	// and process them using SSV's EventHandler.
	var lastProcessedBlockInt int
	lastProcessedBlock, found, err := nodeStorage.GetLastProcessedBlock(nil)
	if err != nil {
		return fmt.Errorf("failed to get last processed block: %w", err)
	}
	if !found {
		lastProcessedBlockInt = int(network.RegistrySyncOffset.Uint64())
	} else if lastProcessedBlock == nil {
		return fmt.Errorf("last processed block is nil")
	} else {
		lastProcessedBlockInt = int(lastProcessedBlock.Int64()) + 1
	}

	// Spawn event retriever.
	backgroundTasks := pool.New().WithContext(ctx).WithCancelOnError()
	events, err := models.ContractEvents(
		models.ContractEventWhere.BlockNumber.GTE(lastProcessedBlockInt),
		qm.OrderBy(
			"?, ?",
			models.ContractEventColumns.BlockNumber,
			models.ContractEventColumns.LogIndex,
		),
	).All(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to get events: %w", err)
	}

	bar := progressbar.New(len(events))
	bar.Describe("Processing events")
	defer bar.Clear()

	blockLogs := make(chan executionclient.BlockLogs)
	backgroundTasks.Go(func(ctx context.Context) error {
		defer close(blockLogs)
		logs := make([]types.Log, len(events))
		for i, event := range events {
			if err := json.Unmarshal(event.RawLog, &logs[i]); err != nil {
				logger.Error("failed to unmarshal event", zap.Error(err))
				return fmt.Errorf("failed to unmarshal event: %w", err)
			}
		}
		for _, v := range executionclient.PackLogs(logs) {
			select {
			case blockLogs <- v:
			case <-ctx.Done():
				logger.Error("context done", zap.Error(ctx.Err()))
				return ctx.Err()
			}
			bar.Add(len(v.Logs))
		}
		return nil
	})

	// Spawn handled event recorder.
	eventTraces := make(chan eventhandler.EventTrace, 1024)
	backgroundTasks.Go(func(ctx context.Context) (err error) {
		if err := recordHandledEvents(ctx, logger, db, nodeStorage, ethClient, eventTraces); err != nil {
			return fmt.Errorf("failed to record handled event: %w", err)
		}
		return nil
	})

	// Handle the events.
	eventHandlerOptions := []eventhandler.Option{
		eventhandler.WithFullNode(), // index complete validator set
	}
	if logger.Level() <= zap.DebugLevel {
		eventHandlerOptions = append(eventHandlerOptions, eventhandler.WithLogger(logger))
	}
	eventHandler, err := eventhandler.New(
		nodeStorage,
		eventParser,
		&noopTaskExecutor{},
		network.Domain,
		&blankOperatorData{},
		nil, // shareEncryptionKeyProvider
		nil, // keyManager
		qbftstorage.NewStores(),
		eventHandlerOptions...,
	)
	if err != nil {
		return fmt.Errorf("failed to create event handler: %w", err)
	}

	newLastProcessedBlock, err := eventHandler.HandleBlockEventsStream(
		blockLogs,
		false,
		eventTraces,
	)
	if err != nil {
		return fmt.Errorf("failed to handle block events stream: %w", err)
	}
	close(eventTraces)

	if err := backgroundTasks.Wait(); err != nil {
		return err
	}

	// Log stats about the SSV database.
	operators, err := nodeStorage.ListOperators(nil, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to list operators: %w", err)
	}
	totalValidators := 0
	liquidatedValidators := 0
	activeValidators := make(map[string]struct{})
	for _, share := range nodeStorage.Shares().List(nil) {
		totalValidators++
		if share.Liquidated {
			liquidatedValidators++
		} else {
			activeValidators[hex.EncodeToString(share.ValidatorPubKey)] = struct{}{}
		}
	}
	bar.Clear()
	logger.Info("Processed events",
		zap.Uint64("last_processed_block", newLastProcessedBlock),
		zap.Int("total_operators", len(operators)),
		zap.Int("total_validators", totalValidators),
		zap.Int("liquidated_validators", liquidatedValidators),
	)

	// Try to reconstruct active validator set from ValidatorEvents.
	validatorEvents, err := models.ValidatorEvents(
		qm.OrderBy(
			"?, ?",
			models.ValidatorEventColumns.BlockNumber,
			models.ValidatorEventColumns.LogIndex,
		),
	).All(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to get validator events: %w", err)
	}
	reconstructedValidators := make(map[string]struct{})
	for _, validatorEvent := range validatorEvents {
		if validatorEvent.Activated {
			reconstructedValidators[validatorEvent.PublicKey] = struct{}{}
		} else {
			delete(reconstructedValidators, validatorEvent.PublicKey)
		}
	}

	// Compare reconstructed validator set against the one in the database,
	// and fail if they don't match.
	missingValidators := make(map[string]struct{})
	for validator := range activeValidators {
		if _, ok := reconstructedValidators[validator]; !ok {
			missingValidators[validator] = struct{}{}
		}
	}
	extraValidators := make(map[string]struct{})
	for validator := range reconstructedValidators {
		if _, ok := activeValidators[validator]; !ok {
			extraValidators[validator] = struct{}{}
		}
	}
	if len(missingValidators) > 0 || len(extraValidators) > 0 {
		logger.Error("Validator set mismatch",
			zap.Strings("missing_validators", maps.Keys(missingValidators)),
			zap.Strings("extra_validators", maps.Keys(extraValidators)),
		)
		return fmt.Errorf("validator set mismatch")
	}

	// Insert validators.
	knownValidators := make(map[phase0.BLSPubKey]bool)
	for _, validatorEvent := range validatorEvents {
		pk, err := decodeValidatorPublicKey(validatorEvent.PublicKey)
		if err != nil {
			return fmt.Errorf("failed to decode validator public key: %w", err)
		}
		if validatorEvent.Activated {
			knownValidators[pk] = true
		} else {
			knownValidators[pk] = false
		}
	}
	beaconValidators, err := cl.(eth2client.ValidatorsProvider).ValidatorsByPubKey(
		ctx,
		"head",
		maps.Keys(knownValidators),
	)
	if err != nil {
		return fmt.Errorf("failed to get beacon validators: %w", err)
	}
	for pk, active := range knownValidators {
		var beaconValidator *v1.Validator
		for _, v := range beaconValidators {
			if v.Validator.PublicKey == pk {
				beaconValidator = v
				break
			}
		}
		validator := models.Validator{
			PublicKey: hex.EncodeToString(pk[:]),
			Active:    active,
		}
		if beaconValidator != nil {
			validator.Index = null.IntFrom(int(beaconValidator.Index))
			validator.BeaconStatus = null.StringFrom(beaconValidator.Status.String())
			validator.BeaconEffectiveBalance = null.Int64From(
				int64(beaconValidator.Validator.EffectiveBalance),
			)
			validator.BeaconActivationEligibilityEpoch = null.IntFrom(
				int(beaconValidator.Validator.ActivationEligibilityEpoch),
			)
			validator.BeaconActivationEpoch = null.IntFrom(
				int(beaconValidator.Validator.ActivationEpoch),
			)
			validator.BeaconExitEpoch = null.IntFrom(int(beaconValidator.Validator.ExitEpoch))
			validator.BeaconSlashed = null.BoolFrom(beaconValidator.Validator.Slashed)
			validator.BeaconWithdrawableEpoch = null.IntFrom(
				int(beaconValidator.Validator.WithdrawableEpoch),
			)
		}
		if err := validator.Upsert(ctx, db, true, []string{"public_key"}, boil.Infer(), boil.Infer()); err != nil {
			return fmt.Errorf("failed to upsert validator: %w", err)
		}
	}
	return nil
}

func recordHandledEvents(
	ctx context.Context,
	logger *zap.Logger,
	db *sql.DB,
	nodeStorage operatorstorage.Storage,
	ethClient *ethclient.Client,
	eventTraces <-chan eventhandler.EventTrace,
) error {
	recordedEvents := 0
	codeAt := make(map[common.Address][]byte) // Cache for CodeAt calls.
	defer func() {
		// Read from eventTraces until it's closed to avoid clogging EventHandler.
		go func() {
			for range eventTraces {
			}
		}()
	}()
	for {
		select {
		case <-ctx.Done():
			logger.Error("context done", zap.Error(ctx.Err()))
			return ctx.Err()
		case eventTrace, ok := <-eventTraces:
			if !ok {
				return nil
			}

			// Update ContractEvent with error, if any.
			var eventErr sql.NullString
			if eventTrace.Error != nil {
				eventErr.String = eventTrace.Error.Error()
				eventErr.Valid = true
			}
			n, err := models.ContractEvents(
				models.ContractEventWhere.BlockNumber.EQ(int(eventTrace.Log.BlockNumber)),
				models.ContractEventWhere.LogIndex.EQ(int(eventTrace.Log.Index)),
			).UpdateAll(ctx, db, models.M{
				models.ContractEventColumns.Error: eventErr,
			})
			if err != nil {
				return fmt.Errorf("failed to update event: %w", err)
			}
			if n != 1 {
				return fmt.Errorf("failed to update event: %w", sql.ErrNoRows)
			}

			// Insert ValidatorEvent(s).
			if eventTrace.Error != nil {
				continue
			}
			recordedEvents++
			databaseEvent, err := models.ContractEvents(
				models.ContractEventWhere.BlockNumber.EQ(int(eventTrace.Log.BlockNumber)),
				models.ContractEventWhere.LogIndex.EQ(int(eventTrace.Log.Index)),
			).One(ctx, db)
			if err != nil {
				return fmt.Errorf("failed to match ContractEvent: %w", err)
			}
			var (
				eventName    string
				pubKeys      []string
				ownerAddress common.Address
				activated    bool
			)
			switch v := eventTrace.Event.(type) {
			case *contract.ContractValidatorAdded:
				eventName = eventparser.ValidatorAdded
				activated = true
				ownerAddress = v.Owner
				pubKeys = []string{hex.EncodeToString(v.PublicKey)}
			case *contract.ContractValidatorRemoved:
				eventName = eventparser.ValidatorRemoved
				activated = false
				ownerAddress = v.Owner
				pubKeys = []string{hex.EncodeToString(v.PublicKey)}
			case *contract.ContractClusterLiquidated:
				eventName = eventparser.ClusterLiquidated
				activated = false
				ownerAddress = v.Owner
				clusterID, err := ssvtypes.ComputeClusterIDHash(v.Owner.Bytes(), v.OperatorIds)
				if err != nil {
					return fmt.Errorf("could not compute share cluster id: %w", err)
				}
				shares := nodeStorage.Shares().List(nil, registrystorage.ByClusterID(clusterID))
				for _, share := range shares {
					pubKeys = append(pubKeys, hex.EncodeToString(share.ValidatorPubKey))
				}
			case *contract.ContractClusterReactivated:
				eventName = eventparser.ClusterReactivated
				activated = true
				ownerAddress = v.Owner
				clusterID, err := ssvtypes.ComputeClusterIDHash(v.Owner.Bytes(), v.OperatorIds)
				if err != nil {
					return fmt.Errorf("could not compute share cluster id: %w", err)
				}
				shares := nodeStorage.Shares().List(nil, registrystorage.ByClusterID(clusterID))
				for _, share := range shares {
					pubKeys = append(pubKeys, hex.EncodeToString(share.ValidatorPubKey))
				}
			}

			// Check if owner is a contract.
			if _, ok := codeAt[ownerAddress]; !ok {
				codeAt[ownerAddress], err = ethClient.CodeAt(ctx, ownerAddress, nil)
				if err != nil {
					return fmt.Errorf(
						"failed to get code at address %q: %w",
						ownerAddress.String(),
						err,
					)
				}
				if len(codeAt[ownerAddress]) > 0 {
					var resp struct {
						Status  string `json:"status"`
						Message string `json:"message"`
						Result  []struct {
							ContractAddress string `json:"contractAddress"`
							ContractCreator string `json:"contractCreator"`
							TxHash          string `json:"txHash"`
						}
					}
					err := requests.URL("https://api.etherscan.io/api").
						Param("module", "contract").
						Param("action", "getcontractcreation").
						Param("contractaddresses", ownerAddress.String()).
						Param("apikey", "YourApiKeyToken").
						ToJSON(&resp).
						Fetch(ctx)
					if err != nil {
						return fmt.Errorf("failed to get contract creation: %w", err)
					}
					if resp.Status != "1" || !strings.HasPrefix(resp.Message, "OK-") {
						return fmt.Errorf("failed to get contract creation: %s", resp.Message)
					}
					if len(resp.Result) != 1 {
						return fmt.Errorf("failed to get contract creation: no result")
					}
					deployerAddress, err := hex.DecodeString(resp.Result[0].ContractAddress[2:])
					if err != nil {
						return fmt.Errorf("failed to decode deployer address: %w", err)
					}
					txHash, err := hex.DecodeString(resp.Result[0].TxHash[2:])
					if err != nil {
						return fmt.Errorf("failed to decode tx hash: %w", err)
					}
					deployer := models.Deployer{
						OwnerAddress:    hex.EncodeToString(ownerAddress[:]),
						DeployerAddress: hex.EncodeToString(deployerAddress),
						TXHash:          hex.EncodeToString(txHash),
					}
					if err := deployer.Insert(ctx, db, boil.Infer()); err != nil {
						return fmt.Errorf("failed to upsert deployer: %w", err)
					}
				}
			}

			for _, pubKey := range pubKeys {
				// Upsert Validator record.
				validator := models.Validator{
					PublicKey: pubKey,
					Active:    activated,
				}
				if err := validator.Upsert(ctx, db, false, []string{"public_key"}, boil.None(), boil.Whitelist("public_key", "active")); err != nil {
					return fmt.Errorf("failed to upsert validator: %w", err)
				}

				validatorEvent := models.ValidatorEvent{
					ContractEventID: databaseEvent.ID,
					Slot:            databaseEvent.Slot,
					BlockNumber:     int(eventTrace.Log.BlockNumber),
					BlockTime:       databaseEvent.BlockTime,
					LogIndex:        int(eventTrace.Log.Index),
					EventName:       eventName,
					OwnerAddress:    hex.EncodeToString(ownerAddress[:]),
					PublicKey:       pubKey,
					Activated:       activated,
				}
				if err := validatorEvent.Insert(ctx, db, boil.Infer()); err != nil {
					return fmt.Errorf("failed to insert validator event: %w", err)
				}
			}
		}
	}
}

type blankOperatorData struct{}

func (b *blankOperatorData) GetOperatorData() *registrystorage.OperatorData {
	return &registrystorage.OperatorData{}
}

func (b *blankOperatorData) SetOperatorData(*registrystorage.OperatorData) {}

type noopTaskExecutor struct{}

func (e *noopTaskExecutor) StartValidator(share *ssvtypes.SSVShare) error {
	panic("unexpected call to StartValidator")
}

func (e *noopTaskExecutor) StopValidator(pubKey spectypes.ValidatorPK) error {
	panic("unexpected call to StopValidator")
}

func (e *noopTaskExecutor) LiquidateCluster(
	owner common.Address,
	operatorIDs []uint64,
	toLiquidate []*ssvtypes.SSVShare,
) error {
	panic("unexpected call to LiquidateCluster")
}

func (e *noopTaskExecutor) ReactivateCluster(
	owner common.Address,
	operatorIDs []uint64,
	toReactivate []*ssvtypes.SSVShare,
) error {
	panic("unexpected call to ReactivateCluster")
}

func (e *noopTaskExecutor) UpdateFeeRecipient(
	owner common.Address,
	recipient common.Address,
) error {
	return nil
}
