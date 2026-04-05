package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/gocarina/gocsv"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/bloxapp/ssv-rewards/pkg/models"
	"github.com/bloxapp/ssv-rewards/pkg/precise"
	"github.com/bloxapp/ssv-rewards/pkg/rewards"
)

// tierCalculationCutoff is the period from which the new tier calculation applies.
// Before this: total EB is sum of average EB per validator (TotalActiveEB / ActiveDays).
// From this period: total EB is average daily EB (Sum(TotalActiveEB) / PeriodDays).
var tierCalculationCutoff = rewards.NewPeriod(2025, 11)

type CalcCmd struct {
	Dir                 string `default:"./rewards" help:"Path to save the rewards to,"`
	PerformanceProvider string `default:"beaconcha" help:"Performance provider to use." enum:"beaconcha,e2m"`

	plan *rewards.Plan
	db   *sql.DB
}

func (c *CalcCmd) Run(
	logger *zap.Logger,
	db *sql.DB,
	network networkconfig.NetworkConfig,
	plan *rewards.Plan,
) error {
	c.db = db
	ctx := context.Background()

	// Create or replace stored procedures.
	rewardsSQL, err := os.ReadFile("rewards.sql")
	if err != nil {
		return fmt.Errorf("failed to read rewards.sql: %w", err)
	}
	if _, err := db.ExecContext(ctx, string(rewardsSQL)); err != nil {
		return fmt.Errorf("failed to execute rewards.sql: %w", err)
	}
	logger.Info("Applied stored procedures")

	// Parse the rewards plan.
	data, err := os.ReadFile("rewards.yaml")
	if err != nil {
		return fmt.Errorf("failed to read rewards.yaml: %w", err)
	}
	c.plan, err = rewards.ParsePlan(data)
	if err != nil {
		return fmt.Errorf("failed to parse rewards plan: %w", err)
	}

	// Empty the existing rewards directory.
	if err := os.Mkdir(c.Dir, 0755); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to create %q: %w", c.Dir, err)
	}
	dir := filepath.Join(c.Dir, network.Name)
	switch _, err := os.Stat(dir); {
	case os.IsNotExist(err):
	case err != nil:
		return fmt.Errorf("failed to stat %q: %w", dir, err)
	default:
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("failed to remove %q: %w", dir, err)
		}
	}

	// Create a temporary directory for the rewards.
	if err := os.Mkdir(".tmp", 0755); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	tmpDir, err := os.MkdirTemp(".tmp", "rewards")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer os.RemoveAll(".tmp")

	// Populate the inputs directory.
	inputsDir := filepath.Join(tmpDir, "inputs")
	if err := os.Mkdir(inputsDir, 0755); err != nil {
		return fmt.Errorf("failed to create inputs directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(inputsDir, "rewards.yaml"), data, 0644); err != nil {
		return fmt.Errorf("failed to write rewards.yaml: %w", err)
	}
	planJSON, err := json.MarshalIndent(c.plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal rewards plan: %w", err)
	}
	if err := os.WriteFile(filepath.Join(inputsDir, "rewards.json"), planJSON, 0644); err != nil {
		return fmt.Errorf("failed to write rewards.json: %w", err)
	}

	// Export redirects from files.
	for _, mechanics := range c.plan.Mechanics {
		if mechanics.OwnerRedirectsFile != "" {
			filePath := filepath.Join(inputsDir, filepath.Base(mechanics.OwnerRedirectsFile))
			if err := exportRedirectsToCSV(mechanics.OwnerRedirects, filePath); err != nil {
				return fmt.Errorf("failed to export owner redirects for period %s: %w", mechanics.Since, err)
			}
		}
		if mechanics.ValidatorRedirectsFile != "" {
			filePath := filepath.Join(inputsDir, filepath.Base(mechanics.ValidatorRedirectsFile))
			if err := exportRedirectsToCSV(mechanics.ValidatorRedirects, filePath); err != nil {
				return fmt.Errorf("failed to export validator redirects for period %s: %w", mechanics.Since, err)
			}
		}
	}

	// Calculate rewards.
	if err := c.run(ctx, logger, tmpDir); err != nil {
		return fmt.Errorf("failed to calculate rewards: %w", err)
	}

	// Move the temporary directory to the rewards directory.
	if err := os.Rename(tmpDir, dir); err != nil {
		return fmt.Errorf("failed to move temporary directory: %w", err)
	}

	return nil
}

func (c *CalcCmd) run(ctx context.Context, logger *zap.Logger, dir string) error {
	// 1. Validate state
	state, err := models.States().One(ctx, c.db)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}
	if state.EarliestValidatorPerformance.IsZero() || state.LatestValidatorPerformance.IsZero() {
		return fmt.Errorf("validator performance data is not available")
	}
	if state.EarliestValidatorPerformance.Time.After(state.LatestValidatorPerformance.Time) {
		return fmt.Errorf("invalid state: earliest validator performance is after latest validator performance")
	}
	if state.EarliestValidatorPerformance.Time.After(c.plan.Rounds[0].Period.FirstDay()) {
		return fmt.Errorf("validator performance data is not available for the first round")
	}

	// 2. Filter complete rounds
	var completeRounds []rewards.Round
	for _, round := range c.plan.Rounds {
		if round.NetworkFee == nil {
			round.NetworkFee = precise.NewETH(nil)
		}
		if round.ETHAPR.Float().Cmp(big.NewFloat(0)) == 1 &&
			round.SSVETH.Float().Cmp(big.NewFloat(0)) == 1 &&
			round.Period.LastDay().Before(state.LatestValidatorPerformance.Time.AddDate(0, 0, 1)) {
			completeRounds = append(completeRounds, round)
		}
	}
	if len(completeRounds) == 0 {
		return fmt.Errorf("no rounds with available performance data")
	}

	// 3. Rewards by round
	var (
		byValidator      []*ValidatorParticipationRound
		byOwner          []*OwnerParticipationRound
		byRecipient      []*RecipientParticipationRound
		totalByValidator = map[string]*ValidatorParticipation{}
		totalByOwner     = map[string]*OwnerParticipation{}
		totalByRecipient = map[string]*RecipientParticipation{}

		// ETH tree accumulators (used when StakingUpgrade is configured).
		ethByValidator      []*ValidatorParticipationRound
		ethByOwner          []*OwnerParticipationRound
		ethByRecipient      []*RecipientParticipationRound
		ethTotalByValidator = map[string]*ValidatorParticipation{}
		ethTotalByOwner     = map[string]*OwnerParticipation{}
		ethTotalByRecipient = map[string]*RecipientParticipation{}
	)

	legacyCalculationCutoff := c.plan.LegacyCalculationCutoff
	hasMigrationSupport := c.plan.StakingUpgrade != nil

	for _, round := range completeRounds {
		mechanics, err := c.plan.Mechanics.At(round.Period)
		if err != nil {
			return fmt.Errorf("failed to get mechanics for period %s: %w", round.Period, err)
		}

		ownerRedirectsSupport, validatorRedirectsSupport, err := c.prepareRedirections(
			ctx,
			mechanics,
		)
		if err != nil {
			return fmt.Errorf("failed to prepare redirections for period %s: %w", round.Period, err)
		}

		var results *roundResults
		var ethResults *roundResults
		if round.Period.Before(legacyCalculationCutoff) {
			results, err = c.processRoundLegacy(ctx, round, mechanics, ownerRedirectsSupport, validatorRedirectsSupport)
		} else if hasMigrationSupport {
			results, ethResults, err = c.processRoundWithMigration(ctx, round, mechanics, ownerRedirectsSupport, validatorRedirectsSupport)
		} else {
			results, err = c.processRound(ctx, round, mechanics, ownerRedirectsSupport, validatorRedirectsSupport)
		}
		if err != nil {
			return fmt.Errorf("failed to process round %s: %w", round.Period, err)
		}

		validatorParticipations := results.validatorParticipations
		ownerParticipations := results.ownerParticipations
		recipientParticipations := results.recipientParticipations

		accumulateValidators(validatorParticipations, round.Period, &byValidator, totalByValidator)
		accumulateOwners(ownerParticipations, round.Period, &byOwner, totalByOwner)
		accumulateRecipients(recipientParticipations, round.Period, &byRecipient, totalByRecipient)

		// Normalize all participations
		for _, p := range validatorParticipations {
			p.Normalize()
		}
		for _, p := range ownerParticipations {
			p.Normalize()
		}
		for _, p := range recipientParticipations {
			p.Normalize()
		}

		// Add network fee address entries if configured
		if mechanics.NetworkFeeAddress != (rewards.ExecutionAddress{}) {
			totalFees := big.NewInt(0)
			totalActiveDays := 0
			totalRegisteredDays := 0

			for _, p := range recipientParticipations {
				if p.feeDeduction != nil {
					totalFees.Add(totalFees, p.feeDeduction)
					totalActiveDays += p.ActiveDays
					totalRegisteredDays += p.RegisteredDays
				}
			}

			if totalFees.Sign() > 0 {
				networkFeeAddr := mechanics.NetworkFeeAddress.String()

				ownerFeeEntry := &OwnerParticipation{
					OwnerAddress:                    networkFeeAddr,
					RecipientAddress:                networkFeeAddr,
					Validators:                      0,
					ActiveDays:                      totalActiveDays,
					RegisteredDays:                  totalRegisteredDays,
					TotalActiveEffectiveBalance:     0,
					TotalRegisteredEffectiveBalance: 0,
					feeDeduction:                    big.NewInt(0),
					reward:                          totalFees,
				}
				ownerFeeEntry.Normalize()
				ownerParticipations = append(ownerParticipations, ownerFeeEntry)

				recipientFeeEntry := &RecipientParticipation{
					RecipientAddress:                networkFeeAddr,
					Validators:                      0,
					ActiveDays:                      totalActiveDays,
					RegisteredDays:                  totalRegisteredDays,
					TotalActiveEffectiveBalance:     0,
					TotalRegisteredEffectiveBalance: 0,
					feeDeduction:                    big.NewInt(0),
					reward:                          totalFees,
				}
				recipientFeeEntry.Normalize()
				recipientParticipations = append(recipientParticipations, recipientFeeEntry)

				byOwner = append(byOwner, &OwnerParticipationRound{
					Round:              round.Period,
					OwnerParticipation: ownerFeeEntry,
				})

				byRecipient = append(byRecipient, &RecipientParticipationRound{
					Round:                  round.Period,
					RecipientParticipation: recipientFeeEntry,
				})

				if existing, ok := totalByOwner[networkFeeAddr]; ok {
					existing.ActiveDays += totalActiveDays
					existing.reward = new(big.Int).Add(existing.reward, totalFees)
				} else {
					totalByOwner[networkFeeAddr] = &OwnerParticipation{
						OwnerAddress:                    networkFeeAddr,
						RecipientAddress:                networkFeeAddr,
						Validators:                      0,
						ActiveDays:                      totalActiveDays,
						RegisteredDays:                  totalRegisteredDays,
						TotalActiveEffectiveBalance:     0,
						TotalRegisteredEffectiveBalance: 0,
						feeDeduction:                    big.NewInt(0),
						reward:                          new(big.Int).Set(totalFees),
					}
				}

				if existing, ok := totalByRecipient[networkFeeAddr]; ok {
					existing.ActiveDays += totalActiveDays
					existing.reward = new(big.Int).Add(existing.reward, totalFees)
				} else {
					totalByRecipient[networkFeeAddr] = &RecipientParticipation{
						RecipientAddress:                networkFeeAddr,
						Validators:                      0,
						ActiveDays:                      totalActiveDays,
						RegisteredDays:                  totalRegisteredDays,
						TotalActiveEffectiveBalance:     0,
						TotalRegisteredEffectiveBalance: 0,
						feeDeduction:                    big.NewInt(0),
						reward:                          new(big.Int).Set(totalFees),
					}
				}
			}
		}

		// Export CSVs
		roundDir := filepath.Join(dir, round.Period.String())
		if err := os.Mkdir(roundDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %q: %w", roundDir, err)
		}
		if err := exportCSV(validatorParticipations, filepath.Join(roundDir, "by-validator.csv")); err != nil {
			return fmt.Errorf("failed to export validator rewards: %w", err)
		}
		if err := exportCSV(ownerParticipations, filepath.Join(roundDir, "by-owner.csv")); err != nil {
			return fmt.Errorf("failed to export owner rewards: %w", err)
		}
		if err := exportCSV(recipientParticipations, filepath.Join(roundDir, "by-recipient.csv")); err != nil {
			return fmt.Errorf("failed to export recipient rewards: %w", err)
		}

		// Export cumulative rewards.
		totalRewards := map[string]string{}
		for _, participation := range totalByRecipient {
			totalRewards["0x"+participation.RecipientAddress] = participation.reward.String()
		}
		f, err := os.Create(filepath.Join(roundDir, "cumulative.json"))
		if err != nil {
			return fmt.Errorf("failed to create cumulative.json: %w", err)
		}
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		if err := enc.Encode(totalRewards); err != nil {
			f.Close() // Close before returning error
			return fmt.Errorf("failed to encode total rewards: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("failed to close cumulative.json: %w", err)
		}

		// ETH tree: accumulate, normalize, and export (no network fee).
		if ethResults != nil && len(ethResults.validatorParticipations) > 0 {
			ethVPs := ethResults.validatorParticipations
			ethOPs := ethResults.ownerParticipations
			ethRPs := ethResults.recipientParticipations

			accumulateValidators(ethVPs, round.Period, &ethByValidator, ethTotalByValidator)
			accumulateOwners(ethOPs, round.Period, &ethByOwner, ethTotalByOwner)
			accumulateRecipients(ethRPs, round.Period, &ethByRecipient, ethTotalByRecipient)

			for _, p := range ethVPs {
				p.Normalize()
			}
			for _, p := range ethOPs {
				p.Normalize()
			}
			for _, p := range ethRPs {
				p.Normalize()
			}

			if err := exportCSV(ethVPs, filepath.Join(roundDir, "by-validator-eth.csv")); err != nil {
				return fmt.Errorf("export ETH validator rewards: %w", err)
			}
			if err := exportCSV(ethOPs, filepath.Join(roundDir, "by-owner-eth.csv")); err != nil {
				return fmt.Errorf("export ETH owner rewards: %w", err)
			}
			if err := exportCSV(ethRPs, filepath.Join(roundDir, "by-recipient-eth.csv")); err != nil {
				return fmt.Errorf("export ETH recipient rewards: %w", err)
			}
		}

		// Write cumulative ETH rewards for every round that has accumulated
		// totals, even when the current round has no new ETH participations.
		if len(ethTotalByRecipient) > 0 {
			ethTotalRewards := map[string]string{}
			for _, p := range ethTotalByRecipient {
				ethTotalRewards["0x"+p.RecipientAddress] = p.reward.String()
			}
			ef, err := os.Create(filepath.Join(roundDir, "cumulative-eth.json"))
			if err != nil {
				return fmt.Errorf("create cumulative-eth.json: %w", err)
			}
			eEnc := json.NewEncoder(ef)
			eEnc.SetIndent("", "  ")
			if err := eEnc.Encode(ethTotalRewards); err != nil {
				ef.Close()
				return fmt.Errorf("encode ETH total rewards: %w", err)
			}
			if err := ef.Close(); err != nil {
				return fmt.Errorf("close cumulative-eth.json: %w", err)
			}
		}

		var dailyReward, monthlyReward, annualReward *big.Int
		if round.Period.Before(legacyCalculationCutoff) {
			dailyReward, monthlyReward, annualReward, err = c.plan.ValidatorRewardsLegacy(round.Period, results.tier)
		} else {
			dailyReward, monthlyReward, annualReward, err = c.plan.ValidatorRewards(round.Period, results.tier)
		}
		if err != nil {
			return fmt.Errorf("failed to get rewards: %w", err)
		}

		logFields := []zap.Field{
			zap.String("period", round.Period.String()),
			zap.String("total_effective_balance", results.totalEffectiveBalance.Display()),
			zap.String("tier", results.tier.MaxEffectiveBalance.Display()),
			zap.String("network_fee", round.NetworkFee.Display()),
			zap.String("daily_reward", precise.NewETH(nil).SetWei(dailyReward).Display()),
			zap.String("monthly_reward", precise.NewETH(nil).SetWei(monthlyReward).Display()),
			zap.String("annual_reward", precise.NewETH(nil).SetWei(annualReward).Display()),
		}

		if round.InflationCap != nil {
			// For two-tree rounds, compute combined original/final for scaling ratio.
			combinedOriginal := new(big.Int).Set(results.originalRewards.Wei())
			combinedFinal := new(big.Int).Set(results.finalRewards.Wei())
			if ethResults != nil && len(ethResults.validatorParticipations) > 0 {
				combinedOriginal.Add(combinedOriginal, ethResults.originalRewards.Wei())
				combinedFinal.Add(combinedFinal, ethResults.finalRewards.Wei())
			}

			scalingRatio := 1.0
			if combinedOriginal.Sign() > 0 {
				scalingRatioFloat := new(big.Float).Quo(
					new(big.Float).SetInt(combinedFinal),
					new(big.Float).SetInt(combinedOriginal),
				)
				scalingRatio, _ = scalingRatioFloat.Float64()
			}

			logFields = append(logFields,
				zap.String("inflation_cap", round.InflationCap.Display()),
				zap.Float64("scaling_ratio", scalingRatio),
				zap.String("original_rewards", precise.NewETH(nil).SetWei(combinedOriginal).Display()),
				zap.String("final_rewards", precise.NewETH(nil).SetWei(combinedFinal).Display()),
			)
		}

		if ethResults != nil && len(ethResults.validatorParticipations) > 0 {
			logFields = append(logFields,
				zap.String("ssv_final_rewards", results.finalRewards.Display()),
				zap.String("eth_final_rewards", ethResults.finalRewards.Display()),
				zap.Int("ssv_validators", len(results.validatorParticipations)),
				zap.Int("eth_validators", len(ethResults.validatorParticipations)),
			)
		}

		logger.Info("Exported rewards for round", logFields...)
	}

	for _, v := range totalByValidator {
		v.Normalize()
	}
	for _, o := range totalByOwner {
		o.Normalize()
	}
	for _, r := range totalByRecipient {
		r.Normalize()
	}

	// Export total rewards.
	if err := exportCSV(byValidator, filepath.Join(dir, "by-validator.csv")); err != nil {
		return fmt.Errorf("failed to export total validator rewards: %w", err)
	}
	if err := exportCSV(byOwner, filepath.Join(dir, "by-owner.csv")); err != nil {
		return fmt.Errorf("failed to export total owner rewards: %w", err)
	}
	if err := exportCSV(byRecipient, filepath.Join(dir, "by-recipient.csv")); err != nil {
		return fmt.Errorf("failed to export total recipient rewards: %w", err)
	}
	if err := exportCSV(maps.Values(totalByValidator), filepath.Join(dir, "total-by-validator.csv")); err != nil {
		return fmt.Errorf("failed to export total validator rewards: %w", err)
	}
	if err := exportCSV(maps.Values(totalByOwner), filepath.Join(dir, "total-by-owner.csv")); err != nil {
		return fmt.Errorf("failed to export total owner rewards: %w", err)
	}
	if err := exportCSV(maps.Values(totalByRecipient), filepath.Join(dir, "total-by-recipient.csv")); err != nil {
		return fmt.Errorf("failed to export total recipient rewards: %w", err)
	}

	// Export ETH tree cross-round totals (only when entries exist).
	if len(ethTotalByRecipient) > 0 {
		for _, v := range ethTotalByValidator {
			v.Normalize()
		}
		for _, o := range ethTotalByOwner {
			o.Normalize()
		}
		for _, r := range ethTotalByRecipient {
			r.Normalize()
		}

		if err := exportCSV(ethByValidator, filepath.Join(dir, "by-validator-eth.csv")); err != nil {
			return fmt.Errorf("export ETH total validator rewards: %w", err)
		}
		if err := exportCSV(ethByOwner, filepath.Join(dir, "by-owner-eth.csv")); err != nil {
			return fmt.Errorf("export ETH total owner rewards: %w", err)
		}
		if err := exportCSV(ethByRecipient, filepath.Join(dir, "by-recipient-eth.csv")); err != nil {
			return fmt.Errorf("export ETH total recipient rewards: %w", err)
		}
		if err := exportCSV(maps.Values(ethTotalByValidator), filepath.Join(dir, "total-by-validator-eth.csv")); err != nil {
			return fmt.Errorf("export ETH total validator rewards: %w", err)
		}
		if err := exportCSV(maps.Values(ethTotalByOwner), filepath.Join(dir, "total-by-owner-eth.csv")); err != nil {
			return fmt.Errorf("export ETH total owner rewards: %w", err)
		}
		if err := exportCSV(maps.Values(ethTotalByRecipient), filepath.Join(dir, "total-by-recipient-eth.csv")); err != nil {
			return fmt.Errorf("export ETH total recipient rewards: %w", err)
		}
	}

	// Export exclusions.
	exclusions, err := c.exclusions(ctx, completeRounds, "ssv")
	if err != nil {
		return fmt.Errorf("failed to get exclusions: %w", err)
	}
	if err := exportCSV(exclusions, filepath.Join(dir, "exclusions.csv")); err != nil {
		return fmt.Errorf("failed to export exclusions: %w", err)
	}

	// Export ETH tree exclusions (only when migration support is configured).
	if c.plan.StakingUpgrade != nil {
		ethExclusions, err := c.exclusions(ctx, completeRounds, "eth")
		if err != nil {
			return fmt.Errorf("failed to get ETH exclusions: %w", err)
		}
		if len(ethExclusions) > 0 {
			if err := exportCSV(ethExclusions, filepath.Join(dir, "exclusions-eth.csv")); err != nil {
				return fmt.Errorf("failed to export ETH exclusions: %w", err)
			}
		}
	}

	return nil
}

// processRoundLegacy handles reward calculation for periods before the legacy cutoff
// It uses SQL-aggregated data to preserve backward compatibility with published merkle trees
func (c *CalcCmd) processRoundLegacy(
	ctx context.Context,
	round rewards.Round,
	mechanics *rewards.Mechanics,
	ownerRedirectsSupport, validatorRedirectsSupport bool,
) (*roundResults, error) {
	validatorParticipations, err := c.validatorParticipations(ctx, round.Period, mechanics, ownerRedirectsSupport, validatorRedirectsSupport, "ssv")
	if err != nil {
		return nil, fmt.Errorf("failed to get validator participations: %w", err)
	}

	ownerParticipations, err := c.ownerParticipations(ctx, round.Period, mechanics, ownerRedirectsSupport, validatorRedirectsSupport, "ssv")
	if err != nil {
		return nil, fmt.Errorf("failed to get owner participations: %w", err)
	}

	recipientParticipations, err := c.recipientParticipations(ctx, round.Period, mechanics, ownerRedirectsSupport, validatorRedirectsSupport, "ssv")
	if err != nil {
		return nil, fmt.Errorf("failed to get recipient participations: %w", err)
	}

	totalEffectiveBalance := c.calculateTotalEffectiveBalanceLegacy(validatorParticipations)
	tier, err := c.plan.Tier(round.Period, totalEffectiveBalance)
	if err != nil {
		return nil, fmt.Errorf("failed to get tier: %w", err)
	}

	dailyReward, _, _, err := c.plan.ValidatorRewardsLegacy(round.Period, tier)
	if err != nil {
		return nil, fmt.Errorf("failed to get rewards: %w", err)
	}

	roundDays := round.Period.Days()
	networkFee := round.NetworkFee

	totalRoundRewards := big.NewInt(0)
	for _, participation := range validatorParticipations {
		participation.reward, participation.feeDeduction, err = c.calculateReward(
			participation.TotalActiveEffectiveBalance,
			participation.TotalRegisteredEffectiveBalance,
			participation.RegisteredDays,
			roundDays,
			dailyReward,
			networkFee.Wei(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate validator reward: %w", err)
		}
		totalRoundRewards.Add(totalRoundRewards, participation.reward)
	}

	for _, participation := range ownerParticipations {
		participation.reward, participation.feeDeduction, err = c.calculateReward(
			participation.TotalActiveEffectiveBalance,
			participation.TotalRegisteredEffectiveBalance,
			participation.RegisteredDays,
			roundDays,
			dailyReward,
			networkFee.Wei(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate owner reward: %w", err)
		}
	}

	for _, participation := range recipientParticipations {
		participation.reward, participation.feeDeduction, err = c.calculateReward(
			participation.TotalActiveEffectiveBalance,
			participation.TotalRegisteredEffectiveBalance,
			participation.RegisteredDays,
			roundDays,
			dailyReward,
			networkFee.Wei(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate recipient reward: %w", err)
		}
	}

	originalRewards := precise.NewETH(nil).SetWei(totalRoundRewards)
	finalRewards := originalRewards

	// Apply inflation cap if exceeded
	if round.InflationCap != nil && totalRoundRewards.Cmp(round.InflationCap.Wei()) > 0 {
		scaleRewards(validatorParticipations, round.InflationCap, totalRoundRewards)
		scaleRewards(ownerParticipations, round.InflationCap, totalRoundRewards)
		scaleRewards(recipientParticipations, round.InflationCap, totalRoundRewards)
		finalRewards = round.InflationCap
	}

	return &roundResults{
		validatorParticipations: validatorParticipations,
		ownerParticipations:     ownerParticipations,
		recipientParticipations: recipientParticipations,
		totalEffectiveBalance:   totalEffectiveBalance,
		tier:                    tier,
		originalRewards:         originalRewards,
		finalRewards:            finalRewards,
	}, nil
}

// processRound handles reward calculation for periods from the legacy cutoff onwards
// It calculates fees per validator before aggregation for correct fee handling
func (c *CalcCmd) processRound(
	ctx context.Context,
	round rewards.Round,
	mechanics *rewards.Mechanics,
	ownerRedirectsSupport, validatorRedirectsSupport bool,
) (*roundResults, error) {
	validatorParticipations, err := c.validatorParticipations(ctx, round.Period, mechanics, ownerRedirectsSupport, validatorRedirectsSupport, "ssv")
	if err != nil {
		return nil, fmt.Errorf("failed to get validator participations: %w", err)
	}

	var totalEffectiveBalance *precise.ETH
	if round.Period.Before(tierCalculationCutoff) {
		totalEffectiveBalance = c.calculateTotalEffectiveBalanceLegacy(validatorParticipations)
	} else {
		totalEffectiveBalance = c.calculateTotalEffectiveBalance(validatorParticipations, round.Period.Days())
	}
	tier, err := c.plan.Tier(round.Period, totalEffectiveBalance)
	if err != nil {
		return nil, fmt.Errorf("failed to get tier: %w", err)
	}

	dailyReward, _, _, err := c.plan.ValidatorRewards(round.Period, tier)
	if err != nil {
		return nil, fmt.Errorf("failed to get rewards: %w", err)
	}

	roundDays := round.Period.Days()
	networkFee := round.NetworkFee

	// Compute the total base reward (without fee reduction),
	// and the total rewards with fee reduction (for logging).
	totalBaseReward := big.NewInt(0)
	originalRewardsWei := big.NewInt(0)
	for _, v := range validatorParticipations {
		deductedReward, feeDeduction, err := c.calculateReward(
			v.TotalActiveEffectiveBalance,
			v.TotalRegisteredEffectiveBalance,
			v.RegisteredDays,
			roundDays,
			dailyReward,
			networkFee.Wei(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate original validator reward: %w", err)
		}
		originalRewardsWei.Add(originalRewardsWei, deductedReward)

		baseReward := new(big.Int).Add(deductedReward, feeDeduction)
		totalBaseReward.Add(totalBaseReward, baseReward)
	}

	// Scale the daily reward rate, if needed.
	scaledDailyReward := new(big.Int).Set(dailyReward)
	if round.InflationCap != nil &&
		totalBaseReward.Sign() > 0 && totalBaseReward.Cmp(round.InflationCap.Wei()) > 0 {
		scaledDailyReward.Mul(scaledDailyReward, round.InflationCap.Wei())
		scaledDailyReward.Div(scaledDailyReward, totalBaseReward)
	}

	// Compute rewards/fees with the (potentially) scaled daily reward.
	totalRoundRewards := big.NewInt(0)
	for _, v := range validatorParticipations {
		var err error
		v.reward, v.feeDeduction, err = c.calculateReward(
			v.TotalActiveEffectiveBalance,
			v.TotalRegisteredEffectiveBalance,
			v.RegisteredDays,
			roundDays,
			scaledDailyReward,
			networkFee.Wei(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate validator reward: %w", err)
		}
		totalRoundRewards.Add(totalRoundRewards, v.reward)
	}

	originalRewards := precise.NewETH(nil).SetWei(originalRewardsWei)
	finalRewards := precise.NewETH(nil).SetWei(totalRoundRewards)

	ownerParticipations := c.aggregateByOwner(validatorParticipations)
	recipientParticipations := c.aggregateByRecipient(validatorParticipations)

	return &roundResults{
		validatorParticipations: validatorParticipations,
		ownerParticipations:     ownerParticipations,
		recipientParticipations: recipientParticipations,
		totalEffectiveBalance:   totalEffectiveBalance,
		tier:                    tier,
		finalRewards:            finalRewards,
		originalRewards:         originalRewards,
	}, nil
}

// processRoundWithMigration handles reward calculation for rounds with ETH migration support.
// It queries both SSV and ETH validator participations, computes a combined effective balance
// for tier selection, and applies a single shared inflation cap across both trees.
func (c *CalcCmd) processRoundWithMigration(
	ctx context.Context,
	round rewards.Round,
	mechanics *rewards.Mechanics,
	ownerRedirectsSupport, validatorRedirectsSupport bool,
) (ssvResults *roundResults, ethResults *roundResults, err error) {
	ssvVPs, err := c.validatorParticipations(ctx, round.Period, mechanics, ownerRedirectsSupport, validatorRedirectsSupport, "ssv")
	if err != nil {
		return nil, nil, fmt.Errorf("get SSV validator participations: %w", err)
	}
	ethVPs, err := c.validatorParticipations(ctx, round.Period, mechanics, ownerRedirectsSupport, validatorRedirectsSupport, "eth")
	if err != nil {
		return nil, nil, fmt.Errorf("get ETH validator participations: %w", err)
	}

	// Combined effective balance for tier selection.
	allVPs := make([]*ValidatorParticipation, 0, len(ssvVPs)+len(ethVPs))
	allVPs = append(allVPs, ssvVPs...)
	allVPs = append(allVPs, ethVPs...)

	var totalEffectiveBalance *precise.ETH
	if round.Period.Before(tierCalculationCutoff) {
		totalEffectiveBalance = c.calculateTotalEffectiveBalanceLegacy(allVPs)
	} else {
		totalEffectiveBalance = c.calculateTotalEffectiveBalance(allVPs, round.Period.Days())
	}
	tier, err := c.plan.Tier(round.Period, totalEffectiveBalance)
	if err != nil {
		return nil, nil, fmt.Errorf("get tier: %w", err)
	}

	dailyReward, _, _, err := c.plan.ValidatorRewards(round.Period, tier)
	if err != nil {
		return nil, nil, fmt.Errorf("get rewards: %w", err)
	}

	roundDays := round.Period.Days()
	ssvNetworkFee := round.NetworkFee.Wei()
	ethNetworkFee := big.NewInt(0)

	// First pass: compute combined base reward for inflation cap.
	totalBaseReward := big.NewInt(0)
	ssvOriginalWei := big.NewInt(0)
	ethOriginalWei := big.NewInt(0)

	for _, v := range ssvVPs {
		reward, fee, err := c.calculateReward(
			v.TotalActiveEffectiveBalance, v.TotalRegisteredEffectiveBalance,
			v.RegisteredDays, roundDays, dailyReward, ssvNetworkFee,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("calculate SSV base reward: %w", err)
		}
		ssvOriginalWei.Add(ssvOriginalWei, reward)
		totalBaseReward.Add(totalBaseReward, new(big.Int).Add(reward, fee))
	}
	for _, v := range ethVPs {
		reward, fee, err := c.calculateReward(
			v.TotalActiveEffectiveBalance, v.TotalRegisteredEffectiveBalance,
			v.RegisteredDays, roundDays, dailyReward, ethNetworkFee,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("calculate ETH base reward: %w", err)
		}
		ethOriginalWei.Add(ethOriginalWei, reward)
		totalBaseReward.Add(totalBaseReward, new(big.Int).Add(reward, fee))
	}

	// Scale daily reward if inflation cap exceeded.
	scaledDailyReward := new(big.Int).Set(dailyReward)
	if round.InflationCap != nil &&
		totalBaseReward.Sign() > 0 && totalBaseReward.Cmp(round.InflationCap.Wei()) > 0 {
		scaledDailyReward.Mul(scaledDailyReward, round.InflationCap.Wei())
		scaledDailyReward.Div(scaledDailyReward, totalBaseReward)
	}

	// Second pass: compute final rewards with (potentially) scaled daily reward.
	ssvFinalWei := big.NewInt(0)
	for _, v := range ssvVPs {
		v.reward, v.feeDeduction, err = c.calculateReward(
			v.TotalActiveEffectiveBalance, v.TotalRegisteredEffectiveBalance,
			v.RegisteredDays, roundDays, scaledDailyReward, ssvNetworkFee,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("calculate SSV final reward: %w", err)
		}
		ssvFinalWei.Add(ssvFinalWei, v.reward)
	}

	ethFinalWei := big.NewInt(0)
	for _, v := range ethVPs {
		v.reward, v.feeDeduction, err = c.calculateReward(
			v.TotalActiveEffectiveBalance, v.TotalRegisteredEffectiveBalance,
			v.RegisteredDays, roundDays, scaledDailyReward, ethNetworkFee,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("calculate ETH final reward: %w", err)
		}
		ethFinalWei.Add(ethFinalWei, v.reward)
	}

	ssvResults = &roundResults{
		validatorParticipations: ssvVPs,
		ownerParticipations:     c.aggregateByOwner(ssvVPs),
		recipientParticipations: c.aggregateByRecipient(ssvVPs),
		totalEffectiveBalance:   totalEffectiveBalance,
		tier:                    tier,
		originalRewards:         precise.NewETH(nil).SetWei(ssvOriginalWei),
		finalRewards:            precise.NewETH(nil).SetWei(ssvFinalWei),
	}
	ethResults = &roundResults{
		validatorParticipations: ethVPs,
		ownerParticipations:     c.aggregateByOwner(ethVPs),
		recipientParticipations: c.aggregateByRecipient(ethVPs),
		totalEffectiveBalance:   totalEffectiveBalance,
		tier:                    tier,
		originalRewards:         precise.NewETH(nil).SetWei(ethOriginalWei),
		finalRewards:            precise.NewETH(nil).SetWei(ethFinalWei),
	}

	return ssvResults, ethResults, nil
}

func accumulateValidators(
	participations []*ValidatorParticipation,
	period rewards.Period,
	byRound *[]*ValidatorParticipationRound,
	totals map[string]*ValidatorParticipation,
) {
	for _, p := range participations {
		*byRound = append(*byRound, &ValidatorParticipationRound{
			Round:                  period,
			ValidatorParticipation: p,
		})
		if total, ok := totals[p.PublicKey]; ok {
			total.ActiveDays += p.ActiveDays
			total.RegisteredDays += p.RegisteredDays
			total.TotalActiveEffectiveBalance += p.TotalActiveEffectiveBalance
			total.TotalRegisteredEffectiveBalance += p.TotalRegisteredEffectiveBalance
			total.reward = new(big.Int).Add(total.reward, p.reward)
			total.feeDeduction = new(big.Int).Add(total.feeDeduction, p.feeDeduction)
		} else {
			cpy := *p
			totals[p.PublicKey] = &cpy
		}
	}
}

func accumulateOwners(
	participations []*OwnerParticipation,
	period rewards.Period,
	byRound *[]*OwnerParticipationRound,
	totals map[string]*OwnerParticipation,
) {
	for _, p := range participations {
		*byRound = append(*byRound, &OwnerParticipationRound{
			Round:              period,
			OwnerParticipation: p,
		})
		key := p.OwnerAddress
		if total, ok := totals[key]; ok {
			total.ActiveDays += p.ActiveDays
			total.RegisteredDays += p.RegisteredDays
			total.Validators += p.Validators
			total.TotalActiveEffectiveBalance += p.TotalActiveEffectiveBalance
			total.TotalRegisteredEffectiveBalance += p.TotalRegisteredEffectiveBalance
			total.reward = new(big.Int).Add(total.reward, p.reward)
			total.feeDeduction = new(big.Int).Add(total.feeDeduction, p.feeDeduction)
		} else {
			cpy := *p
			totals[key] = &cpy
		}
	}
}

func accumulateRecipients(
	participations []*RecipientParticipation,
	period rewards.Period,
	byRound *[]*RecipientParticipationRound,
	totals map[string]*RecipientParticipation,
) {
	for _, p := range participations {
		*byRound = append(*byRound, &RecipientParticipationRound{
			Round:                  period,
			RecipientParticipation: p,
		})
		if total, ok := totals[p.RecipientAddress]; ok {
			total.ActiveDays += p.ActiveDays
			total.RegisteredDays += p.RegisteredDays
			total.Validators += p.Validators
			total.TotalActiveEffectiveBalance += p.TotalActiveEffectiveBalance
			total.TotalRegisteredEffectiveBalance += p.TotalRegisteredEffectiveBalance
			total.reward = new(big.Int).Add(total.reward, p.reward)
			total.feeDeduction = new(big.Int).Add(total.feeDeduction, p.feeDeduction)
		} else {
			cpy := *p
			totals[p.RecipientAddress] = &cpy
		}
	}
}

// calculateTotalEffectiveBalanceLegacy calculates total EB as sum of average EB per validator.
// Used for periods before tierCalculationCutoff.
func (c *CalcCmd) calculateTotalEffectiveBalanceLegacy(validators []*ValidatorParticipation) *precise.ETH {
	var totalEffectiveBalanceGwei int64
	for _, v := range validators {
		if v.ActiveDays == 0 {
			continue
		}
		totalEffectiveBalanceGwei += v.TotalActiveEffectiveBalance / int64(v.ActiveDays)
	}
	return precise.NewETH(nil).SetGwei(big.NewInt(totalEffectiveBalanceGwei))
}

// calculateTotalEffectiveBalance calculates total EB as average daily EB across the period.
// Used for periods from tierCalculationCutoff onwards.
func (c *CalcCmd) calculateTotalEffectiveBalance(validators []*ValidatorParticipation, periodDays int) *precise.ETH {
	var totalActiveEBGwei int64
	for _, v := range validators {
		totalActiveEBGwei += v.TotalActiveEffectiveBalance
	}
	return precise.NewETH(nil).SetGwei(big.NewInt(totalActiveEBGwei / int64(periodDays)))
}

// scaleRewards applies proportional scaling to participation rewards when inflation cap is exceeded
func scaleRewards(participations interface{}, inflationCap *precise.ETH, totalRoundRewards *big.Int) {
	inflationCapWei := inflationCap.Wei()
	switch p := participations.(type) {
	case []*ValidatorParticipation:
		for _, part := range p {
			part.reward.Mul(part.reward, inflationCapWei)
			part.reward.Div(part.reward, totalRoundRewards)
		}
	case []*OwnerParticipation:
		for _, part := range p {
			part.reward.Mul(part.reward, inflationCapWei)
			part.reward.Div(part.reward, totalRoundRewards)
		}
	case []*RecipientParticipation:
		for _, part := range p {
			part.reward.Mul(part.reward, inflationCapWei)
			part.reward.Div(part.reward, totalRoundRewards)
		}
	}
}

func (c *CalcCmd) aggregateByOwner(validators []*ValidatorParticipation) []*OwnerParticipation {
	type ownerRecipientKey struct {
		owner     string
		recipient string
	}
	aggregations := make(map[ownerRecipientKey]*OwnerParticipation)

	for _, v := range validators {
		key := ownerRecipientKey{
			owner:     v.OwnerAddress,
			recipient: v.RecipientAddress,
		}

		if existing, ok := aggregations[key]; ok {
			existing.Validators++
			existing.ActiveDays += v.ActiveDays
			existing.RegisteredDays += v.RegisteredDays
			existing.TotalActiveEffectiveBalance += v.TotalActiveEffectiveBalance
			existing.TotalRegisteredEffectiveBalance += v.TotalRegisteredEffectiveBalance
			existing.reward = new(big.Int).Add(existing.reward, v.reward)
			existing.feeDeduction = new(big.Int).Add(existing.feeDeduction, v.feeDeduction)
		} else {
			aggregations[key] = &OwnerParticipation{
				OwnerAddress:                    v.OwnerAddress,
				RecipientAddress:                v.RecipientAddress,
				Validators:                      1,
				ActiveDays:                      v.ActiveDays,
				RegisteredDays:                  v.RegisteredDays,
				TotalActiveEffectiveBalance:     v.TotalActiveEffectiveBalance,
				TotalRegisteredEffectiveBalance: v.TotalRegisteredEffectiveBalance,
				reward:                          new(big.Int).Set(v.reward),
				feeDeduction:                    new(big.Int).Set(v.feeDeduction),
			}
		}
	}

	result := make([]*OwnerParticipation, 0, len(aggregations))
	for _, participation := range aggregations {
		result = append(result, participation)
	}
	return result
}

func (c *CalcCmd) aggregateByRecipient(validators []*ValidatorParticipation) []*RecipientParticipation {
	aggregations := make(map[string]*RecipientParticipation)

	for _, v := range validators {
		recipientAddr := v.RecipientAddress

		if existing, ok := aggregations[recipientAddr]; ok {
			existing.Validators++
			existing.ActiveDays += v.ActiveDays
			existing.RegisteredDays += v.RegisteredDays
			existing.TotalActiveEffectiveBalance += v.TotalActiveEffectiveBalance
			existing.TotalRegisteredEffectiveBalance += v.TotalRegisteredEffectiveBalance
			existing.reward = new(big.Int).Add(existing.reward, v.reward)
			existing.feeDeduction = new(big.Int).Add(existing.feeDeduction, v.feeDeduction)
		} else {
			aggregations[recipientAddr] = &RecipientParticipation{
				RecipientAddress:                recipientAddr,
				Validators:                      1,
				ActiveDays:                      v.ActiveDays,
				RegisteredDays:                  v.RegisteredDays,
				TotalActiveEffectiveBalance:     v.TotalActiveEffectiveBalance,
				TotalRegisteredEffectiveBalance: v.TotalRegisteredEffectiveBalance,
				reward:                          new(big.Int).Set(v.reward),
				feeDeduction:                    new(big.Int).Set(v.feeDeduction),
			}
		}
	}

	result := make([]*RecipientParticipation, 0, len(aggregations))
	for _, participation := range aggregations {
		result = append(result, participation)
	}
	return result
}

// roundResults contains all the calculated participations for a round
type roundResults struct {
	validatorParticipations []*ValidatorParticipation
	ownerParticipations     []*OwnerParticipation
	recipientParticipations []*RecipientParticipation
	totalEffectiveBalance   *precise.ETH
	tier                    *rewards.Tier
	finalRewards            *precise.ETH // Final rewards distributed (after scaling)
	originalRewards         *precise.ETH // Original rewards before scaling
}

func (c *CalcCmd) calculateReward(
	wActiveEBi int64,
	wRegEBi int64,
	registeredDaysI int,
	roundDaysI int,
	dailyReward *big.Int,
	networkFee *big.Int,
) (*big.Int, *big.Int, error) {
	if roundDaysI == 0 {
		return nil, nil, fmt.Errorf("round days cannot be zero")
	}

	// ---- inputs → big.Int ----
	wActiveEB := big.NewInt(wActiveEBi)
	wRegEB := big.NewInt(wRegEBi)
	registeredDays := big.NewInt(int64(registeredDaysI))
	roundDays := big.NewInt(int64(roundDaysI))

	// ---- shared factors ----
	baseEffectiveBalanceGwei := rewards.BaseEffectiveBalance.Gwei()
	unitBase := new(big.Int).Mul(baseEffectiveBalanceGwei, roundDays) // 32 ETH * roundDays
	rewardTier := new(big.Int).Mul(dailyReward, roundDays)

	// 1. baseRewardᵢ = (rewardCap × ΣwActiveEBᵢ) / unitBase
	baseReward := new(big.Int).Mul(rewardTier, wActiveEB)
	baseReward.Div(baseReward, unitBase)

	// 2. rawFeeᵢ = max( (NF × ΣwRegEBᵢ)/unitBase − (NF × ΣregisteredDaysᵢ)/roundDays , 0 )
	feeFromEB := new(big.Int).Mul(networkFee, wRegEB)
	feeFromEB.Div(feeFromEB, unitBase)

	feeCredit := new(big.Int).Mul(networkFee, registeredDays)
	feeCredit.Div(feeCredit, roundDays)

	rawFee := new(big.Int).Sub(feeFromEB, feeCredit)
	if rawFee.Sign() < 0 {
		rawFee.SetInt64(0)
	}

	// 3. finalFeeᵢ = min(baseRewardᵢ, rawFeeᵢ)
	finalFee := new(big.Int)
	if baseReward.Cmp(rawFee) <= 0 {
		finalFee.Set(baseReward)
	} else {
		finalFee.Set(rawFee)
	}

	// 4. finalRewardᵢ = baseRewardᵢ − feeDeductedᵢ
	finalReward := new(big.Int).Sub(baseReward, finalFee)

	return finalReward, finalFee, nil
}

type ValidatorParticipation struct {
	RecipientAddress                string
	OwnerAddress                    string
	PublicKey                       string
	ActiveDays                      int
	RegisteredDays                  int
	TotalActiveEffectiveBalance     int64        // In Gwei
	TotalRegisteredEffectiveBalance int64        // In Gwei
	FeeDeduction                    *precise.ETH `boil:"-"`
	feeDeduction                    *big.Int     `boil:"-"`
	Reward                          *precise.ETH `boil:"-"`
	reward                          *big.Int     `boil:"-"`
}

func (p *ValidatorParticipation) Normalize() {
	p.Reward = precise.NewETH(nil).SetWei(p.reward)
	p.FeeDeduction = precise.NewETH(nil).SetWei(p.feeDeduction)

	// Convert Gwei to ETH using precise package
	totalActiveETH := precise.NewETH(nil).SetGwei(big.NewInt(p.TotalActiveEffectiveBalance))
	totalRegisteredETH := precise.NewETH(nil).SetGwei(big.NewInt(p.TotalRegisteredEffectiveBalance))

	// Get whole ETH for CSV export
	p.TotalActiveEffectiveBalance = totalActiveETH.ETH()
	p.TotalRegisteredEffectiveBalance = totalRegisteredETH.ETH()
}

type ValidatorParticipationRound struct {
	Round rewards.Period
	*ValidatorParticipation
}

func (c *CalcCmd) validatorParticipations(
	ctx context.Context,
	period rewards.Period,
	mechanics *rewards.Mechanics,
	ownerRedirectsSupport, validatorRedirectsSupport bool,
	migrationFilter string,
) ([]*ValidatorParticipation, error) {
	var participations []*ValidatorParticipation
	return participations, queries.Raw(
		"SELECT * FROM participations_by_validator($1, $2, $3, $4, $5, $6, $7, $8, $9)",
		c.PerformanceProvider,
		mechanics.Criteria.MinAttestationsPerDay,
		mechanics.Criteria.MinDecidedsPerDay,
		time.Time(period),
		nil, // to_period can be nil for single-period queries
		ownerRedirectsSupport,
		validatorRedirectsSupport,
		mechanics.PectraSupport,
		migrationFilter,
	).Bind(ctx, c.db, &participations)
}

type OwnerParticipation struct {
	OwnerAddress                    string
	RecipientAddress                string
	Validators                      int
	ActiveDays                      int
	RegisteredDays                  int
	TotalActiveEffectiveBalance     int64
	TotalRegisteredEffectiveBalance int64
	FeeDeduction                    *precise.ETH `boil:"-"`
	feeDeduction                    *big.Int     `boil:"-"`
	Reward                          *precise.ETH `boil:"-"`
	reward                          *big.Int     `boil:"-"`
}

func (p *OwnerParticipation) Normalize() {
	p.Reward = precise.NewETH(nil).SetWei(p.reward)
	p.FeeDeduction = precise.NewETH(nil).SetWei(p.feeDeduction)

	// Convert Gwei to ETH using precise package
	totalActiveETH := precise.NewETH(nil).SetGwei(big.NewInt(p.TotalActiveEffectiveBalance))
	totalRegisteredETH := precise.NewETH(nil).SetGwei(big.NewInt(p.TotalRegisteredEffectiveBalance))

	// Get whole ETH for CSV export
	p.TotalActiveEffectiveBalance = totalActiveETH.ETH()
	p.TotalRegisteredEffectiveBalance = totalRegisteredETH.ETH()
}

type OwnerParticipationRound struct {
	Round rewards.Period
	*OwnerParticipation
}

func (c *CalcCmd) ownerParticipations(
	ctx context.Context,
	period rewards.Period,
	mechanics *rewards.Mechanics,
	ownerRedirectsSupport, validatorRedirectsSupport bool,
	migrationFilter string,
) ([]*OwnerParticipation, error) {
	var participations []*OwnerParticipation
	return participations, queries.Raw(
		"SELECT * FROM participations_by_owner($1, $2, $3, $4, $5, $6, $7, $8, $9)",
		c.PerformanceProvider,
		mechanics.Criteria.MinAttestationsPerDay,
		mechanics.Criteria.MinDecidedsPerDay,
		time.Time(period),
		nil,
		ownerRedirectsSupport,
		validatorRedirectsSupport,
		mechanics.PectraSupport,
		migrationFilter,
	).Bind(ctx, c.db, &participations)
}

type RecipientParticipation struct {
	RecipientAddress                string
	Validators                      int
	ActiveDays                      int
	RegisteredDays                  int
	TotalActiveEffectiveBalance     int64        `csv:"wActiveEF"`
	TotalRegisteredEffectiveBalance int64        `csv:"wRegEF"`
	FeeDeduction                    *precise.ETH `boil:"-"`
	feeDeduction                    *big.Int     `boil:"-"`
	Reward                          *precise.ETH `boil:"-"`
	reward                          *big.Int     `boil:"-"`
}

func (p *RecipientParticipation) Normalize() {
	p.Reward = precise.NewETH(nil).SetWei(p.reward)
	p.FeeDeduction = precise.NewETH(nil).SetWei(p.feeDeduction)

	// Convert Gwei to ETH using precise package
	totalActiveETH := precise.NewETH(nil).SetGwei(big.NewInt(p.TotalActiveEffectiveBalance))
	totalRegisteredETH := precise.NewETH(nil).SetGwei(big.NewInt(p.TotalRegisteredEffectiveBalance))

	// Get whole ETH for CSV export
	p.TotalActiveEffectiveBalance = totalActiveETH.ETH()
	p.TotalRegisteredEffectiveBalance = totalRegisteredETH.ETH()
}

type RecipientParticipationRound struct {
	Round rewards.Period
	*RecipientParticipation
}

func (c *CalcCmd) recipientParticipations(
	ctx context.Context,
	period rewards.Period,
	mechanics *rewards.Mechanics,
	ownerRedirectsSupport, validatorRedirectsSupport bool,
	migrationFilter string,
) ([]*RecipientParticipation, error) {
	var participations []*RecipientParticipation
	return participations, queries.Raw(
		"SELECT * FROM participations_by_recipient($1, $2, $3, $4, $5, $6, $7, $8, $9)",
		c.PerformanceProvider,
		mechanics.Criteria.MinAttestationsPerDay,
		mechanics.Criteria.MinDecidedsPerDay,
		time.Time(period),
		nil,
		ownerRedirectsSupport,
		validatorRedirectsSupport,
		mechanics.PectraSupport,
		migrationFilter,
	).Bind(ctx, c.db, &participations)
}

func (c *CalcCmd) prepareRedirections(
	ctx context.Context,
	mechanics *rewards.Mechanics,
) (bool, bool, error) {
	// Check and populate Owner Redirects
	ownerRedirectsSupport := len(mechanics.OwnerRedirects) > 0
	if ownerRedirectsSupport {
		if err := c.populateOwnerRedirectsTable(ctx, mechanics.OwnerRedirects); err != nil {
			return false, false, fmt.Errorf("failed to populate owner redirects: %w", err)
		}
	}

	// Check and populate Validator Redirects
	validatorRedirectsSupport := len(mechanics.ValidatorRedirects) > 0
	if validatorRedirectsSupport {
		if err := c.populateValidatorRedirectsTable(ctx, mechanics.ValidatorRedirects); err != nil {
			return false, false, fmt.Errorf("failed to populate validator redirects: %w", err)
		}
	}

	// Return whether redirects are supported
	return ownerRedirectsSupport, validatorRedirectsSupport, nil
}

func (c *CalcCmd) populateOwnerRedirectsTable(
	ctx context.Context,
	redirects rewards.OwnerRedirects,
) error {
	// Truncate the owner_redirects table.
	_, err := queries.Raw(
		"TRUNCATE TABLE "+models.TableNames.OwnerRedirects,
	).ExecContext(ctx, c.db)
	if err != nil {
		return fmt.Errorf("failed to truncate owner_redirects: %w", err)
	}

	// Verify that the table is empty.
	count, err := models.OwnerRedirects().Count(ctx, c.db)
	if err != nil {
		return fmt.Errorf("failed to count owner_redirects: %w", err)
	}
	if count != 0 {
		return fmt.Errorf("owner_redirects table was not truncated")
	}

	// Populate with given redirects.
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()
	for from, to := range redirects {
		model := models.OwnerRedirect{
			FromAddress: from.String(),
			ToAddress:   to.String(),
		}
		if err := model.Insert(ctx, tx, boil.Infer()); err != nil {
			return fmt.Errorf("failed to insert rewards_redirect: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Verify that the table is populated.
	count, err = models.OwnerRedirects().Count(ctx, c.db)
	if err != nil {
		return fmt.Errorf("failed to count owner_redirects: %w", err)
	}
	if int(count) != len(redirects) {
		return fmt.Errorf("owner_redirects table was not populated")
	}

	return nil
}

func (c *CalcCmd) populateValidatorRedirectsTable(
	ctx context.Context,
	redirects rewards.ValidatorRedirects,
) error {
	// Truncate the validator_redirects table.
	_, err := queries.Raw(
		"TRUNCATE TABLE "+models.TableNames.ValidatorRedirects,
	).ExecContext(ctx, c.db)
	if err != nil {
		return fmt.Errorf("failed to truncate validator_redirects: %w", err)
	}

	// Verify that the table is empty.
	count, err := models.ValidatorRedirects().Count(ctx, c.db)
	if err != nil {
		return fmt.Errorf("failed to count validator_redirects: %w", err)
	}
	if count != 0 {
		return fmt.Errorf("validator_redirects table was not truncated")
	}

	// Populate with given redirects.
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()
	for pubkey, to := range redirects {
		model := models.ValidatorRedirect{
			PublicKey: pubkey.String(),
			ToAddress: to.String(),
		}
		if err := model.Insert(ctx, tx, boil.Infer()); err != nil {
			return fmt.Errorf("failed to insert rewards_redirect: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Verify that the table is populated.
	count, err = models.ValidatorRedirects().Count(ctx, c.db)
	if err != nil {
		return fmt.Errorf("failed to count validator_redirects: %w", err)
	}
	if int(count) != len(redirects) {
		return fmt.Errorf("validator_redirects table was not populated")
	}

	return nil
}

type Exclusion struct {
	Day               time.Time
	FromEpoch         phase0.Epoch
	ToEpoch           phase0.Epoch
	PublicKey         string
	StartBeaconStatus string
	EndBeaconStatus   string
	Events            string
	ExclusionReason   string
}

func (c *CalcCmd) exclusionsForRound(
	ctx context.Context,
	period rewards.Period,
	migrationFilter string,
) ([]*Exclusion, error) {
	mechanics, err := c.plan.Mechanics.At(period)
	if err != nil {
		return nil, fmt.Errorf("failed to get mechanics for period %s: %w", period, err)
	}

	var rows []struct {
		Day               time.Time
		FromEpoch         phase0.Epoch
		ToEpoch           phase0.Epoch
		PublicKey         string
		StartBeaconStatus sql.NullString
		EndBeaconStatus   sql.NullString
		Events            sql.NullString
		ExclusionReason   string
	}

	err = queries.Raw(
		"SELECT * FROM exclusions_by_validator($1, $2, $3, $4, $5, $6)",
		c.PerformanceProvider,
		mechanics.Criteria.MinAttestationsPerDay,
		mechanics.Criteria.MinDecidedsPerDay,
		time.Time(period),
		time.Time(period),
		migrationFilter,
	).Bind(ctx, c.db, &rows)
	if err != nil {
		return nil, err
	}

	exclusions := make([]*Exclusion, len(rows))
	for i, row := range rows {
		exclusions[i] = &Exclusion{
			Day:               row.Day,
			FromEpoch:         row.FromEpoch,
			ToEpoch:           row.ToEpoch,
			PublicKey:         row.PublicKey,
			StartBeaconStatus: row.StartBeaconStatus.String,
			EndBeaconStatus:   row.EndBeaconStatus.String,
			Events:            row.Events.String,
			ExclusionReason:   row.ExclusionReason,
		}
	}
	return exclusions, nil
}

func (c *CalcCmd) exclusions(
	ctx context.Context,
	rounds []rewards.Round,
	migrationFilter string,
) ([]*Exclusion, error) {
	var exclusions []*Exclusion
	for _, round := range rounds {
		e, err := c.exclusionsForRound(ctx, round.Period, migrationFilter)
		if err != nil {
			return nil, fmt.Errorf("failed to get exclusions for round %s: %w", round.Period, err)
		}
		exclusions = append(exclusions, e...)
	}

	return exclusions, nil
}

func exportCSV(data any, fileName string) error {
	f, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("failed to create %q: %w", fileName, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Comma = '\t' // Set tab delimiter locally

	if err := gocsv.MarshalCSV(data, gocsv.NewSafeCSVWriter(w)); err != nil {
		return fmt.Errorf("failed to marshal %q: %w", fileName, err)
	}

	w.Flush()
	return nil
}

func exportRedirectsToCSV(redirects interface{}, fileName string) error {
	type RedirectRow struct {
		From string `csv:"from"`
		To   string `csv:"to"`
	}

	var rows []RedirectRow

	switch r := redirects.(type) {
	case rewards.OwnerRedirects:
		for from, to := range r {
			rows = append(rows, RedirectRow{
				From: from.String(),
				To:   to.String(),
			})
		}
	case rewards.ValidatorRedirects:
		for from, to := range r {
			rows = append(rows, RedirectRow{
				From: from.String(),
				To:   to.String(),
			})
		}
	default:
		return fmt.Errorf("unsupported redirects type: %T", redirects)
	}

	return exportCSV(rows, fileName)
}
