package sync

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/eth/eventparser"
	"github.com/carlmjohnson/requests"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/schollz/progressbar/v3"
	"github.com/sourcegraph/conc/pool"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv-rewards/pkg/beacon"
	"github.com/bloxapp/ssv-rewards/pkg/models"
	"github.com/bloxapp/ssv-rewards/pkg/sync/httpretry"
	"github.com/bloxapp/ssv-rewards/pkg/sync/performance"
)

func SyncValidatorPerformance(
	ctx context.Context,
	logger *zap.Logger,
	spec beacon.Spec,
	ethClient *ethclient.Client,
	db *sql.DB,
	ssvAPIEndpoint string,
	provider performance.Provider,
	fromDay time.Time,
	toDay time.Time,
	highestBlockNumber uint64,
	cacheDir string,
) error {
	providerType := models.ProviderType(provider.Type())
	if err := providerType.IsValid(); err != nil {
		return fmt.Errorf("invalid provider type (%q): %w", providerType, err)
	}

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

	existingDays := make(map[time.Time]bool)
	rows, err := models.ValidatorPerformances(
		models.ValidatorPerformanceWhere.Provider.EQ(providerType),
	).All(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to load existing validator performance days: %w", err)
	}
	for _, row := range rows {
		existingDays[row.Day.UTC().Truncate(24*time.Hour)] = true
	}

	// Don't undershoot the earliest block with validator activity.
	var earliestActiveDay time.Time
	for _, event := range validatorEvents {
		if event.EventName == eventparser.ValidatorAdded {
			block, err := ethClient.BlockByNumber(ctx, new(big.Int).SetUint64(uint64(event.BlockNumber)))
			if err != nil {
				return fmt.Errorf("failed to get earliest block time: %w", err)
			}
			earliestActiveDay = time.Unix(int64(block.Time()), 0).UTC().Truncate(24 * time.Hour)
			break
		}
	}
	if earliestActiveDay.IsZero() {
		return fmt.Errorf("failed to determine earliest active day")
	}
	if fromDay.Before(earliestActiveDay) {
		fromDay = earliestActiveDay
	}

	// Don't exceed the day before highestBlockNumber.
	highestBlock, err := ethClient.BlockByNumber(ctx, new(big.Int).SetUint64(highestBlockNumber))
	if err != nil {
		return fmt.Errorf("failed to get latest block time: %w", err)
	}
	highestDay := time.Unix(int64(highestBlock.Time()), 0).
		UTC().
		Truncate(24*time.Hour).
		AddDate(0, 0, -1)
	if toDay.After(highestDay) {
		toDay = highestDay
	}

	// Don't exceed the latest day with reliable performance data.
	cutoffDay := time.Now().UTC().AddDate(0, 0, -2).Truncate(24 * time.Hour)
	if toDay.After(cutoffDay) {
		toDay = cutoffDay
	}
	if toDay.Before(fromDay) {
		return fmt.Errorf("not enough days with activity (%s - %s)", fromDay, toDay)
	}

	// Set the state's earliest_validator_performance.
	_, err = models.States().UpdateAll(ctx, db, models.M{"earliest_validator_performance": fromDay})
	if err != nil {
		return fmt.Errorf("failed to update state: %w", err)
	}

	// For each day since the earliest block, fetch validator performance within the day's epoch range.
	logger.Info("Fetching validator performance", zap.Time("from", fromDay), zap.Time("to", toDay))
	bar := progressbar.New(int(toDay.Sub(fromDay).Hours()/24) + 1)
	defer bar.Clear()
	fetchedDays := 0
	totalDays := 0

	type activeValidator struct {
		Since        phase0.Epoch
		OwnerAddress string
	}

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

	activeByDay := make(map[time.Time]map[phase0.BLSPubKey]activeValidator)
	current := make(map[phase0.BLSPubKey]activeValidator)
	var lastSnapshotDay time.Time

	for _, event := range validatorEvents {
		eventDay := event.BlockTime.UTC().Truncate(24 * time.Hour)
		pk, err := decodeValidatorPublicKey(event.PublicKey)
		if err != nil {
			return fmt.Errorf("failed to decode validator public key: %w", err)
		}

		switch event.EventName {
		case eventparser.ValidatorAdded:
			current[pk] = activeValidator{
				Since:        spec.EpochAt(phase0.Slot(event.Slot)),
				OwnerAddress: event.OwnerAddress,
			}
		case eventparser.ValidatorRemoved:
			delete(current, pk)
		case eventparser.ClusterLiquidated, eventparser.ClusterReactivated:
			continue
		default:
			return fmt.Errorf("unexpected validator event: %s", event.EventName)
		}

		// Only snapshot once per day
		if eventDay != lastSnapshotDay {
			snapshot := make(map[phase0.BLSPubKey]activeValidator, len(current))
			for k, v := range current {
				snapshot[k] = v
			}
			activeByDay[eventDay] = snapshot
			lastSnapshotDay = eventDay
		}
	}

	var (
		inMemorySSVCache   = make(map[string]*ssvCacheItem)
		inMemorySSVCacheMu sync.Mutex
	)

	for day := fromDay; !day.After(toDay); day = day.AddDate(0, 0, 1) {
		if existingDays[day.UTC().Truncate(24*time.Hour)] {
			bar.Add(1)
			continue
		}

		bar.Describe(day.Format("2006-01-02"))
		totalDays++

		// Determine the epoch range for the day.
		beaconDay := beaconDay(spec, day.Year(), day.Month(), day.Day())
		fromEpoch := spec.EpochAt(spec.SlotAt(beaconDay))
		toEpoch := spec.EpochAt(spec.SlotAt(beaconDay.AddDate(0, 0, 1))) - 1
		logger := logger.With(
			zap.String("day", day.Format("2006-01-02")),
			zap.Time("beacon_day", beaconDay),
			zap.Uint64("from_epoch", uint64(fromEpoch)),
			zap.Uint64("to_epoch", uint64(toEpoch)),
		)

		// Sanity check.
		epochsPerDay := time.Hour * 24 / spec.SlotDuration / time.Duration(spec.SlotsPerEpoch)
		if toEpoch-fromEpoch+1 != phase0.Epoch(epochsPerDay) {
			return errors.New("epoch range is not exactly a day")
		}

		activeValidators := activeByDay[day.UTC().Truncate(24*time.Hour)]

		var decideds map[string]int
		dayKey := day.Format("2006-01-02")

		dutyCountsStart := time.Now()

		// Check memory cache first (no time check needed)
		inMemorySSVCacheMu.Lock()
		memItem, found := inMemorySSVCache[dayKey]
		inMemorySSVCacheMu.Unlock()

		if found {
			logger.Info("Using SSV data from memory cache", zap.String("day", dayKey))
			decideds = make(map[string]int, len(memItem.Validators))
			for k, v := range memItem.Validators {
				decideds[k] = v
			}
		} else {
			// Check file cache
			cachedItem, err := loadSSVCache(cacheDir, dayKey)
			if err == nil && cachedItem.Time.After(day.Add(48*time.Hour)) {
				logger.Info("Using SSV data from file cache", zap.String("day", dayKey))
				decideds = make(map[string]int, len(cachedItem.Validators))

				inMemorySSVCacheMu.Lock()
				inMemorySSVCache[dayKey] = cachedItem // Populate memory cache
				inMemorySSVCacheMu.Unlock()

				for k, v := range cachedItem.Validators {
					decideds[k] = v
				}
			} else {
				// Fetch fresh
				logger.Info("Fetching fresh SSV data", zap.String("day", dayKey))

				var resp struct {
					Error      string
					Validators map[string]struct{ Duties int }
				}
				url := ssvAPIEndpoint
				if url[len(url)-1] != '/' {
					url += "/"
				}
				err := requests.URL(url).
					Client(httpretry.Client).
					Pathf("%s/validators/duty_counts/%d/%d", spec.Network, fromEpoch, toEpoch).
					ToJSON(&resp).
					Fetch(ctx)
				if err != nil {
					return fmt.Errorf("failed to get validator duties: %w", err)
				}
				if resp.Error != "" {
					return fmt.Errorf("ssv api returned error: %s", resp.Error)
				}

				decideds = make(map[string]int, len(resp.Validators))
				for pubKey, data := range resp.Validators {
					decideds[pubKey] = data.Duties
				}

				newItem := &ssvCacheItem{
					Time:       time.Now(),
					Validators: decideds,
				}

				inMemorySSVCacheMu.Lock()
				inMemorySSVCache[dayKey] = newItem
				inMemorySSVCacheMu.Unlock()

				if err := saveSSVCache(cacheDir, dayKey, *newItem); err != nil {
					logger.Warn("Failed to save SSV cache", zap.Error(err))
				}
			}
		}
		dutyCountsDuration := time.Since(dutyCountsStart)

		beaconchaStart := time.Now()
		performancesPool := pool.New().WithContext(ctx).WithCancelOnError().WithFirstError().WithMaxGoroutines(4)
		var performances []*models.ValidatorPerformance
		var performancesMutex sync.Mutex
		for pubKey, activeValidator := range activeValidators {
			pubKey, activeValidator := pubKey, activeValidator
			performancesPool.Go(func(ctx context.Context) error {
				performance := models.ValidatorPerformance{
					Provider:        providerType,
					Day:             day,
					FromEpoch:       int(fromEpoch),
					ToEpoch:         int(toEpoch),
					OwnerAddress:    activeValidator.OwnerAddress,
					PublicKey:       hex.EncodeToString(pubKey[:]),
					SolventWholeDay: activeValidator.Since < fromEpoch,
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
					startState := v1.ValidatorToState(
						phase0Validator,
						&phase0Validator.EffectiveBalance,
						fromEpoch,
						spec.FarFutureEpoch,
					)
					endState := v1.ValidatorToState(
						phase0Validator,
						&phase0Validator.EffectiveBalance,
						toEpoch,
						spec.FarFutureEpoch,
					)
					performance.StartBeaconStatus = null.StringFrom(startState.String())
					performance.EndBeaconStatus = null.StringFrom(endState.String())

					// Measure beaconcha API duration
					data, err := provider.ValidatorPerformance(
						ctx,
						logger,
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
						performance.EndEffectiveBalance = null.Int64From(data.EndEffectiveBalance)
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
								zap.Int("index", validator.Index.Int),
							)
						}
					}
				}
				performancesMutex.Lock()
				performances = append(performances, &performance)
				performancesMutex.Unlock()
				return nil
			})
		}

		// Wait for all performances to complete
		if err := performancesPool.Wait(); err != nil {
			return fmt.Errorf("failed waiting for validator performance: %w", err)
		}

		beaconchaDuration := time.Since(beaconchaStart)

		// Insert ValidatorPerformance records.
		insertStart := time.Now()
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer tx.Rollback()

		for _, performance := range performances {
			if decideds, ok := decideds[performance.PublicKey]; ok {
				performance.Decideds = null.IntFrom(decideds)
			}

			if err := performance.Insert(ctx, tx, boil.Infer()); err != nil {
				logger.Error("failed to insert validator performance",
					zap.String("public_key", performance.PublicKey),
					zap.Time("day", performance.Day),
					zap.Int64("end_effective_balance", performance.EndEffectiveBalance.Int64),
					zap.Any("performance", performance),
					zap.Error(err),
				)
				return fmt.Errorf("failed to insert validator performance for %s at %s: %w", performance.PublicKey, performance.Day, err)
			}
		}
		insertDuration := time.Since(insertStart)

		commitStart := time.Now()
		// Set the state's latest_validator_performance.
		if _, err = models.States().UpdateAll(ctx, db, models.M{"latest_validator_performance": day}); err != nil {
			return fmt.Errorf("failed to update state: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}
		commitDuration := time.Since(commitStart)

		bar.Add(1)
		fetchedDays++

		logger.Info("Fetched validator performance",
			zap.Time("day", day),
			zap.Duration("duty_counts_duration", dutyCountsDuration),
			zap.Duration("insert_duration", insertDuration),
			zap.Duration("commit_duration", commitDuration),
			zap.Duration("beaconcha_total_duration", beaconchaDuration),
		)
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

type ssvCacheItem struct {
	Time       time.Time      `json:"time"`
	Validators map[string]int `json:"validators"`
}

func loadSSVCache(cacheDir, date string) (*ssvCacheItem, error) {
	cachePath := filepath.Join(cacheDir, fmt.Sprintf("%s.json", date))
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}

	var item ssvCacheItem
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func saveSSVCache(cacheDir, date string, item ssvCacheItem) error {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	cachePath := filepath.Join(cacheDir, fmt.Sprintf("%s.json", date))
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cachePath, data, 0644)
}

// beaconDay returns the time of the first slot of the given day,
// counting from the Beacon genesis time.
func beaconDay(spec beacon.Spec, year int, month time.Month, day int) time.Time {
	return time.Date(
		year,
		month,
		day,
		spec.GenesisTime.Hour(),
		spec.GenesisTime.Minute(),
		spec.GenesisTime.Second(),
		spec.GenesisTime.Nanosecond(),
		time.UTC,
	)
}
