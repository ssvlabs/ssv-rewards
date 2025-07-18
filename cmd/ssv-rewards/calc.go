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

// BaseEffectiveBalanceGwei 32 ETH in Gwei (32 * 1e9 = 32_000_000_000)
const BaseEffectiveBalanceGwei = 32_000_000_000
const Gwei = int64(1e9)

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
	)

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

		// Fetch participations
		validatorParticipations, err := c.validatorParticipations(ctx, round.Period, mechanics, ownerRedirectsSupport, validatorRedirectsSupport)
		if err != nil {
			return fmt.Errorf("failed to get validator participations: %w", err)
		}
		ownerParticipations, err := c.ownerParticipations(ctx, round.Period, mechanics, ownerRedirectsSupport, validatorRedirectsSupport)
		if err != nil {
			return fmt.Errorf("failed to get owner participations: %w", err)
		}
		recipientParticipations, err := c.recipientParticipations(ctx, round.Period, mechanics, ownerRedirectsSupport, validatorRedirectsSupport)
		if err != nil {
			return fmt.Errorf("failed to get recipient participations: %w", err)
		}

		// Calculate appropriate tier and rewards.
		var totalEffectiveBalanceGwei int64
		for _, v := range validatorParticipations {
			// TODO can it happen?
			if v.ActiveDays == 0 {
				continue
			}

			totalEffectiveBalanceGwei += v.TotalActiveEffectiveBalance / int64(v.ActiveDays)
		}

		tier, err := c.plan.Tier(round.Period, totalEffectiveBalanceGwei)
		if err != nil {
			return fmt.Errorf("failed to get tier (period: %s): %w", round.Period, err)
		}
		dailyReward, monthlyReward, annualReward, err := c.plan.ValidatorRewards(
			round.Period,
			totalEffectiveBalanceGwei,
		)
		if err != nil {
			return fmt.Errorf("failed to get reward: %w", err)
		}

		roundDays := round.Period.Days()
		networkFee := round.NetworkFee

		// -- Validator rewards  --
		for _, participation := range validatorParticipations {
			participation.reward, participation.feeDeduction, err = c.calculateReward(
				participation.TotalActiveEffectiveBalance,
				participation.TotalRegisteredEffectiveBalance,
				participation.RegisteredDays,
				roundDays,
				dailyReward,
				networkFee.Gwei(),
			)
			if err != nil {
				return fmt.Errorf("failed to calculate validator reward: %w", err)
			}

			byValidator = append(byValidator, &ValidatorParticipationRound{
				Round:                  round.Period,
				ValidatorParticipation: participation,
			})
			if total, ok := totalByValidator[participation.PublicKey]; ok {
				total.ActiveDays += participation.ActiveDays
				total.reward = new(big.Int).Add(total.reward, participation.reward)
			} else {
				cpy := *participation
				totalByValidator[participation.PublicKey] = &cpy
			}
		}

		// -- Owner rewards  --
		for _, participation := range ownerParticipations {
			participation.reward, participation.feeDeduction, err = c.calculateReward(
				participation.TotalActiveEffectiveBalance,
				participation.TotalRegisteredEffectiveBalance,
				participation.RegisteredDays,
				roundDays,
				dailyReward,
				networkFee.Gwei(),
			)
			if err != nil {
				return fmt.Errorf("failed to calculate owner reward: %w", err)
			}

			byOwner = append(byOwner, &OwnerParticipationRound{
				Round:              round.Period,
				OwnerParticipation: participation,
			})

			key := participation.OwnerAddress
			if total, ok := totalByOwner[key]; ok {
				total.ActiveDays += participation.ActiveDays
				total.reward = new(big.Int).Add(total.reward, participation.reward)
			} else {
				cpy := *participation
				totalByOwner[key] = &cpy
			}
		}

		// -- Recipient rewards  --
		for _, participation := range recipientParticipations {
			participation.reward, participation.feeDeduction, err = c.calculateReward(
				participation.TotalActiveEffectiveBalance,
				participation.TotalRegisteredEffectiveBalance,
				participation.RegisteredDays,
				roundDays,
				dailyReward,
				networkFee.Gwei(),
			)
			if err != nil {
				return fmt.Errorf("failed to calculate recipient reward: %w", err)
			}

			byRecipient = append(byRecipient, &RecipientParticipationRound{
				Round:                  round.Period,
				RecipientParticipation: participation,
			})

			if total, ok := totalByRecipient[participation.RecipientAddress]; ok {
				total.ActiveDays += participation.ActiveDays
				total.reward = new(big.Int).Add(total.reward, participation.reward)
			} else {
				cpy := *participation
				totalByRecipient[participation.RecipientAddress] = &cpy
			}
		}

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
			// Calculate total fee deductions
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

			// Only add network fee entries if total fees > 0
			if totalFees.Sign() > 0 {
				// Convert ExecutionAddress to string format (hex without 0x)
				networkFeeAddr := mechanics.NetworkFeeAddress.String()

				// Add to owner participations
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

				// Add to recipient participations
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

				// Add to round-level aggregations
				byOwner = append(byOwner, &OwnerParticipationRound{
					Round:              round.Period,
					OwnerParticipation: ownerFeeEntry,
				})

				byRecipient = append(byRecipient, &RecipientParticipationRound{
					Round:                  round.Period,
					RecipientParticipation: recipientFeeEntry,
				})

				// Add to totals
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
		dir := filepath.Join(dir, round.Period.String())
		if err := os.Mkdir(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %q: %w", dir, err)
		}
		if err := exportCSV(validatorParticipations, filepath.Join(dir, "by-validator.csv")); err != nil {
			return fmt.Errorf("failed to export validator rewards: %w", err)
		}
		if err := exportCSV(ownerParticipations, filepath.Join(dir, "by-owner.csv")); err != nil {
			return fmt.Errorf("failed to export owner rewards: %w", err)
		}
		if err := exportCSV(recipientParticipations, filepath.Join(dir, "by-recipient.csv")); err != nil {
			return fmt.Errorf("failed to export recipient rewards: %w", err)
		}

		// Export cumulative rewards.
		totalRewards := map[string]string{}
		for _, participation := range totalByRecipient {
			totalRewards["0x"+participation.RecipientAddress] = participation.reward.String()
		}
		f, err := os.Create(filepath.Join(dir, "cumulative.json"))
		if err != nil {
			return fmt.Errorf("failed to create cumulative.json: %w", err)
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		if err := enc.Encode(totalRewards); err != nil {
			return fmt.Errorf("failed to encode total rewards: %w", err)
		}

		logger.Info(
			"Exported rewards for round",
			zap.String("period", round.Period.String()),
			zap.Int64("total_effective_balance", totalEffectiveBalanceGwei/Gwei),
			zap.Int64("tier", tier.MaxEffectiveBalance),
			zap.String("network_fee", networkFee.String()),
			zap.String("daily_reward", precise.NewETH(nil).SetWei(dailyReward).String()),
			zap.String("monthly_reward", precise.NewETH(nil).SetWei(monthlyReward).String()),
			zap.String("annual_reward", precise.NewETH(nil).SetWei(annualReward).String()),
		)
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

	// Export exclusions.
	exclusions, err := c.exclusions(
		ctx,
		completeRounds,
	)
	if err != nil {
		return fmt.Errorf("failed to get exclusions: %w", err)
	}
	if err := exportCSV(exclusions, filepath.Join(dir, "exclusions.csv")); err != nil {
		return fmt.Errorf("failed to export exclusions: %w", err)
	}

	return nil
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
	unitBase := new(big.Int).Mul(big.NewInt(BaseEffectiveBalanceGwei), roundDays) // 32 * roundDays
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
	TotalActiveEffectiveBalance     int64
	TotalRegisteredEffectiveBalance int64
	FeeDeduction                    *precise.ETH `boil:"-"`
	feeDeduction                    *big.Int     `boil:"-"`
	Reward                          *precise.ETH `boil:"-"`
	reward                          *big.Int     `boil:"-"`
}

func (p *ValidatorParticipation) Normalize() {
	p.Reward = precise.NewETH(nil).SetWei(p.reward)
	p.FeeDeduction = precise.NewETH(nil).SetWei(p.feeDeduction)
	p.TotalActiveEffectiveBalance /= Gwei
	p.TotalRegisteredEffectiveBalance /= Gwei
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
) ([]*ValidatorParticipation, error) {
	var participations []*ValidatorParticipation
	return participations, queries.Raw(
		"SELECT * FROM participations_by_validator($1, $2, $3, $4, $5, $6, $7, $8)",
		c.PerformanceProvider,
		mechanics.Criteria.MinAttestationsPerDay,
		mechanics.Criteria.MinDecidedsPerDay,
		time.Time(period),
		nil, // to_period can be nil for single-period queries
		ownerRedirectsSupport,
		validatorRedirectsSupport,
		mechanics.PectraSupport,
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
	p.TotalActiveEffectiveBalance /= Gwei
	p.TotalRegisteredEffectiveBalance /= Gwei
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
) ([]*OwnerParticipation, error) {
	var participations []*OwnerParticipation
	return participations, queries.Raw(
		"SELECT * FROM participations_by_owner($1, $2, $3, $4, $5, $6, $7, $8)",
		c.PerformanceProvider,
		mechanics.Criteria.MinAttestationsPerDay,
		mechanics.Criteria.MinDecidedsPerDay,
		time.Time(period),
		nil,
		ownerRedirectsSupport,
		validatorRedirectsSupport,
		mechanics.PectraSupport,
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
	p.TotalActiveEffectiveBalance /= Gwei
	p.TotalRegisteredEffectiveBalance /= Gwei
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
) ([]*RecipientParticipation, error) {
	var participations []*RecipientParticipation
	return participations, queries.Raw(
		"SELECT * FROM participations_by_recipient($1, $2, $3, $4, $5, $6, $7, $8)",
		c.PerformanceProvider,
		mechanics.Criteria.MinAttestationsPerDay,
		mechanics.Criteria.MinDecidedsPerDay,
		time.Time(period),
		nil,
		ownerRedirectsSupport,
		validatorRedirectsSupport,
		mechanics.PectraSupport,
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
		"SELECT * FROM exclusions_by_validator($1, $2, $3, $4, $5)",
		c.PerformanceProvider,
		mechanics.Criteria.MinAttestationsPerDay,
		mechanics.Criteria.MinDecidedsPerDay,
		time.Time(period),
		time.Time(period),
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
) ([]*Exclusion, error) {
	var exclusions []*Exclusion
	for _, round := range rounds {
		e, err := c.exclusionsForRound(ctx, round.Period)
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
