package sync

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/bloxapp/ssv-rewards/beacon"
	"github.com/bloxapp/ssv-rewards/models"
	"github.com/bloxapp/ssv-rewards/sync/performance"
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/schollz/progressbar/v3"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
	"go.uber.org/zap"
)

func SyncValidatorPerformance(
	ctx context.Context,
	logger *zap.Logger,
	spec beacon.Spec,
	ethClient *ethclient.Client,
	cl eth2client.Service,
	db *sql.DB,
	provider performance.Provider,
	toBlock uint64,
) error {
	providerType := models.Provider(provider.Type())
	if err := providerType.IsValid(); err != nil {
		return fmt.Errorf("invalid provider type (%q): %w", providerType, err)
	}

	// Determine the latest day that we can fetch for,
	// performance data after that day is considered not available.
	maximumDay := time.Now().UTC().AddDate(0, 0, -2).Truncate(24 * time.Hour)

	// Fetch ValidatorEvents from the database to determine earliest and latest blocks
	// with validator activity and active validators at each day.
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

	// Determine the date ranges to fetch for.
	var fromDay time.Time
	for _, event := range validatorEvents {
		if event.EventName == eventparser.ValidatorAdded {
			earliestBlock, err := ethClient.
				BlockByNumber(ctx, new(big.Int).SetUint64(uint64(event.BlockNumber)))
			if err != nil {
				return fmt.Errorf("failed to get earliest block time: %w", err)
			}
			fromDay = time.Unix(int64(earliestBlock.Time()), 0).UTC().Truncate(24 * time.Hour)
			break
		}
	}
	if fromDay.IsZero() {
		return fmt.Errorf("failed to get earliest activity")
	}
	latestBlock, err := ethClient.BlockByNumber(ctx, new(big.Int).SetUint64(uint64(toBlock)))
	if err != nil {
		return fmt.Errorf("failed to get latest block time: %w", err)
	}
	toDay := time.Unix(int64(latestBlock.Time()), 0).UTC().Truncate(24 * time.Hour)
	if toDay.After(maximumDay) {
		toDay = maximumDay
	}
	if toDay.Before(fromDay) {
		return fmt.Errorf("not enough days with activity (%s - %s)", fromDay, toDay)
	}

	// For each day since the earliest block, fetch validator performance within the day's epoch range.
	bar := progressbar.New(int(toDay.Sub(fromDay).Hours()/24) + 1)
	bar.Describe("Fetching validator performance")
	defer bar.Clear()
	day := fromDay
	fetchedDays := 0
	totalDays := 0

	type activeValidator struct {
		Since        phase0.Epoch
		OwnerAddress string
	}
	activeValidators := map[phase0.BLSPubKey]activeValidator{}

	// Get validators known in the Beacon chain.
	beaconValidators, err := models.Validators(
		models.ValidatorWhere.Index.IsNotNull(),
	).All(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to get validators: %w", err)
	}
	validatorsByPubKey := map[phase0.BLSPubKey]*models.Validator{}
	for _, validator := range beaconValidators {
		pk, err := decodeValidatorPublicKey(validator.PublicKey)
		if err != nil {
			return fmt.Errorf("failed to decode validator public key: %w", err)
		}
		validatorsByPubKey[pk] = validator
	}

	for day.Before(toDay) || day.Equal(toDay) {
		bar.Describe(fmt.Sprintf("Fetching validator performance for %s", day.Format("2006-01-02")))
		totalDays++

		fromEpoch := phase0.Epoch(
			spec.EpochAt(spec.SlotAt(day)) + 1,
		)
		toEpoch := phase0.Epoch(
			spec.EpochAt(spec.SlotAt(day.AddDate(0, 0, 1))),
		)
		logger := logger.With(
			zap.Time("day", day),
			zap.Uint64("from_epoch", uint64(fromEpoch)),
			zap.Uint64("to_epoch", uint64(toEpoch)),
		)

		// Sanity check.
		epochsPerDay := time.Hour * 24 / spec.SlotDuration / time.Duration(spec.SlotsPerEpoch)
		if toEpoch-fromEpoch+1 != phase0.Epoch(epochsPerDay) {
			return errors.New("epoch range is not exactly a day")
		}

		// Keep track of active validators in this day.
		fromSlot := spec.FirstSlot(fromEpoch)
		toSlot := spec.LastSlot(toEpoch)
		for _, event := range validatorEvents {
			if event.Slot >= int(fromSlot) && event.Slot <= int(toSlot) {
				pk, err := decodeValidatorPublicKey(event.PublicKey)
				if err != nil {
					return fmt.Errorf("failed to decode validator public key: %w", err)
				}
				epoch := spec.EpochAt(phase0.Slot(event.Slot))
				if event.Activated {
					activeValidators[pk] = activeValidator{
						Since:        epoch,
						OwnerAddress: event.OwnerAddress,
					}
				} else {
					delete(activeValidators, pk)
				}
			}
		}

		// Skip if already fetched.
		existing, err := models.ValidatorPerformances(
			models.ValidatorPerformanceWhere.Provider.EQ(providerType),
			models.ValidatorPerformanceWhere.Day.EQ(day),
		).One(ctx, db)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("failed to get validator performance: %w", err)
			}
		} else {
			if existing.FromEpoch != int(fromEpoch) || existing.ToEpoch != int(toEpoch) {
				return fmt.Errorf("validator performance mismatch: %d-%d != %d-%d", existing.FromEpoch, existing.ToEpoch, fromEpoch, toEpoch)
			}
			day = day.AddDate(0, 0, 1)
			bar.Add(1)
			continue
		}

		// Insert ValidatorPerformance records.
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer tx.Rollback()
		for pubKey, activeValidator := range activeValidators {
			performance := models.ValidatorPerformance{
				Provider:       providerType,
				Day:            day,
				FromEpoch:      int(fromEpoch),
				ToEpoch:        int(toEpoch),
				OwnerAddress:   activeValidator.OwnerAddress,
				PublicKey:      hex.EncodeToString(pubKey[:]),
				ActiveWholeDay: activeValidator.Since < fromEpoch,
			}
			if validator, ok := validatorsByPubKey[pubKey]; ok {
				performance.Index = null.IntFrom(validator.Index.Int)

				phase0Validator := &phase0.Validator{
					PublicKey:        pubKey,
					EffectiveBalance: phase0.Gwei(validator.BeaconEffectiveBalance.Int64),
					Slashed:          validator.BeaconSlashed.Bool,
					ActivationEligibilityEpoch: phase0.Epoch(
						validator.BeaconActivationEligibilityEpoch.Int,
					),
					ActivationEpoch:   phase0.Epoch(validator.BeaconActivationEpoch.Int),
					ExitEpoch:         phase0.Epoch(validator.BeaconExitEpoch.Int),
					WithdrawableEpoch: phase0.Epoch(validator.BeaconWithdrawableEpoch.Int),
				}
				startState := v1.ValidatorToState(phase0Validator, fromEpoch, spec.FarFutureEpoch)
				endState := v1.ValidatorToState(phase0Validator, toEpoch, spec.FarFutureEpoch)
				performance.StartBeaconStatus = null.StringFrom(startState.String())
				performance.EndBeaconStatus = null.StringFrom(endState.String())

				data, err := provider.ValidatorPerformance(
					ctx,
					spec,
					day,
					fromEpoch,
					toEpoch,
					phase0.Epoch(validator.BeaconActivationEpoch.Int),
					phase0.Epoch(validator.BeaconExitEpoch.Int),
					phase0.ValidatorIndex(validator.Index.Int),
				)
				if err != nil {
					return fmt.Errorf("failed to get validator performance: %w", err)
				}
				if data != nil {
					performance.Effectiveness = null.Float32FromPtr(data.Effectiveness)
					performance.AttestationRate = null.Float32From(data.AttestationRate)
					performance.ProposalsAssigned = null.Int16From(data.Proposals.Assigned)
					performance.ProposalsExecuted = null.Int16From(data.Proposals.Executed)
					performance.ProposalsMissed = null.Int16From(data.Proposals.Missed)
					performance.AttestationsAssigned = null.Int16From(data.Attestations.Assigned)
					performance.AttestationsExecuted = null.Int16From(data.Attestations.Executed)
					performance.AttestationsMissed = null.Int16From(data.Attestations.Missed)
					performance.SyncCommitteeAssigned = null.Int16From(data.SyncCommittee.Assigned)
					performance.SyncCommitteeExecuted = null.Int16From(data.SyncCommittee.Executed)
					performance.SyncCommitteeMissed = null.Int16From(data.SyncCommittee.Missed)
				} else {
					if startState.IsAttesting() || endState.IsAttesting() {
						logger.Warn(
							"missing validator performance",
							zap.String("public_key", hex.EncodeToString(pubKey[:])),
							zap.Int("index", int(validator.Index.Int)),
						)
					}
				}
			}
			if err := performance.Insert(ctx, tx, boil.Infer()); err != nil {
				return fmt.Errorf("failed to insert validator performance: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}
		day = day.AddDate(0, 0, 1)
		bar.Add(1)
		fetchedDays++
	}
	bar.Clear()
	logger.Info("Fetched validator performance",
		zap.Time("from", fromDay),
		zap.Time("to", toDay),
		zap.Int("total_days", totalDays),
		zap.Int("fetched_days", fetchedDays),
	)
	return nil
}

func decodeValidatorPublicKey(hexEncoded string) (phase0.BLSPubKey, error) {
	pk, err := hex.DecodeString(hexEncoded)
	if err != nil {
		return phase0.BLSPubKey{}, fmt.Errorf("failed to decode hex: %w", err)
	}
	if len(pk) != len(phase0.BLSPubKey{}) {
		return phase0.BLSPubKey{}, fmt.Errorf("invalid public key length: %d", len(pk))
	}
	return phase0.BLSPubKey(pk), nil
}
