package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
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
	// Verify that validator performance data is available.
	state, err := models.States().One(ctx, c.db)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}
	if state.EarliestValidatorPerformance.IsZero() || state.LatestValidatorPerformance.IsZero() {
		return fmt.Errorf("validator performance data is not available")
	}
	if state.EarliestValidatorPerformance.Time.After(state.LatestValidatorPerformance.Time) {
		return fmt.Errorf(
			"invalid state: earliest validator performance is after latest validator performance",
		)
	}
	if state.EarliestValidatorPerformance.Time.After(c.plan.Rounds[0].Period.FirstDay()) {
		return fmt.Errorf("validator performance data is not available for the first round")
	}

	// Select the rounds with available performance data.
	var completeRounds []rewards.Round
	for _, round := range c.plan.Rounds {
		if round.ETHAPR.Float().Cmp(big.NewFloat(0)) == 1 &&
			round.SSVETH.Float().Cmp(big.NewFloat(0)) == 1 &&
			round.Period.LastDay().Before(state.LatestValidatorPerformance.Time.AddDate(0, 0, 1)) {
			completeRounds = append(completeRounds, round)
		}
	}
	if len(completeRounds) == 0 {
		return fmt.Errorf("no rounds with available performance data")
	}

	// Calculate rewards.
	var byValidator []*ValidatorParticipationRound
	var byOwner []*OwnerParticipationRound
	var byRecipient []*RecipientParticipationRound
	var totalByValidator = map[string]*ValidatorParticipation{}
	var totalByOwner = map[string]*OwnerParticipation{}
	var totalByRecipient = map[string]*RecipientParticipation{}
	for _, round := range completeRounds {
		// Collect validator and owner participations.
		validatorParticipations, err := c.validatorParticipations(ctx, round.Period)
		if err != nil {
			return fmt.Errorf("failed to get validator participations: %w", err)
		}
		ownerParticipations, err := c.ownerParticipations(ctx, round.Period)
		if err != nil {
			return fmt.Errorf("failed to get owner participations: %w", err)
		}
		recipientParticipations, err := c.recipientParticipations(ctx, round.Period)
		if err != nil {
			return fmt.Errorf("failed to get recipient participations: %w", err)
		}

		// Calculate appropriate tier and rewards.
		tier, err := c.plan.Tier(round.Period, len(validatorParticipations))
		if err != nil {
			return fmt.Errorf("failed to get tier (period: %s): %w", round.Period, err)
		}
		dailyReward, monthlyReward, annualReward, err := c.plan.ValidatorRewards(
			round.Period,
			len(validatorParticipations),
		)
		if err != nil {
			return fmt.Errorf("failed to get reward: %w", err)
		}

		// Attach rewards to participations.
		ownerActiveDays := map[string]int{}
		for _, participation := range validatorParticipations {
			// participation.Reward = dailyReward * float64(participation.ActiveDays)
			participation.reward = new(
				big.Int,
			).Mul(dailyReward, big.NewInt(int64(participation.ActiveDays)))
			ownerActiveDays[participation.OwnerAddress] += participation.ActiveDays

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
		for _, participation := range ownerParticipations {
			participation.reward = new(
				big.Int,
			).Mul(dailyReward, big.NewInt(int64(participation.ActiveDays)))

			if participation.ActiveDays != ownerActiveDays[participation.OwnerAddress] {
				return fmt.Errorf(
					"inconsistent active days for owner %q",
					participation.OwnerAddress,
				)
			}

			byOwner = append(byOwner, &OwnerParticipationRound{
				Round:              round.Period,
				OwnerParticipation: participation,
			})
			if total, ok := totalByOwner[participation.OwnerAddress]; ok {
				total.ActiveDays += participation.ActiveDays
				total.reward = new(big.Int).Add(total.reward, participation.reward)
			} else {
				cpy := *participation
				totalByOwner[participation.OwnerAddress] = &cpy
			}
		}
		for _, participation := range recipientParticipations {
			participation.reward = new(
				big.Int,
			).Mul(dailyReward, big.NewInt(int64(participation.ActiveDays)))

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

		// Export rewards.
		dir := filepath.Join(dir, round.Period.String())
		if err := os.Mkdir(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %q: %w", dir, err)
		}
		for _, participation := range validatorParticipations {
			participation.Reward = precise.NewETH(nil).SetWei(participation.reward)
		}
		if err := exportCSV(validatorParticipations, filepath.Join(dir, "by-validator.csv")); err != nil {
			return fmt.Errorf("failed to export validator rewards: %w", err)
		}
		for _, participation := range ownerParticipations {
			participation.Reward = precise.NewETH(nil).SetWei(participation.reward)
		}
		if err := exportCSV(ownerParticipations, filepath.Join(dir, "by-owner.csv")); err != nil {
			return fmt.Errorf("failed to export owner rewards: %w", err)
		}
		for _, participation := range recipientParticipations {
			participation.Reward = precise.NewETH(nil).SetWei(participation.reward)
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
			zap.Int("validators", len(validatorParticipations)),
			zap.Int("tier", tier.MaxParticipants),
			zap.String("daily_reward", precise.NewETH(nil).SetWei(dailyReward).String()),
			zap.String("monthly_reward", precise.NewETH(nil).SetWei(monthlyReward).String()),
			zap.String("annual_reward", precise.NewETH(nil).SetWei(annualReward).String()),
		)
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
	for _, participation := range totalByValidator {
		participation.Reward = precise.NewETH(nil).SetWei(participation.reward)
	}
	if err := exportCSV(maps.Values(totalByValidator), filepath.Join(dir, "total-by-validator.csv")); err != nil {
		return fmt.Errorf("failed to export total validator rewards: %w", err)
	}
	for _, participation := range totalByOwner {
		participation.Reward = precise.NewETH(nil).SetWei(participation.reward)
	}
	if err := exportCSV(maps.Values(totalByOwner), filepath.Join(dir, "total-by-owner.csv")); err != nil {
		return fmt.Errorf("failed to export total owner rewards: %w", err)
	}
	for _, participation := range totalByRecipient {
		participation.Reward = precise.NewETH(nil).SetWei(participation.reward)
	}
	if err := exportCSV(maps.Values(totalByRecipient), filepath.Join(dir, "total-by-recipient.csv")); err != nil {
		return fmt.Errorf("failed to export total recipient rewards: %w", err)
	}

	// Export exclusions.
	exclusions, err := c.exclusions(
		ctx,
		completeRounds[0].Period,
		completeRounds[len(completeRounds)-1].Period,
	)
	if err != nil {
		return fmt.Errorf("failed to get exclusions: %w", err)
	}
	if err := exportCSV(exclusions, filepath.Join(dir, "exclusions.csv")); err != nil {
		return fmt.Errorf("failed to export exclusions: %w", err)
	}

	return nil
}

type ValidatorParticipation struct {
	OwnerAddress     string
	RecipientAddress string
	PublicKey        string
	ActiveDays       int
	Reward           *precise.ETH `boil:"-"`
	reward           *big.Int     `boil:"-"`
}

type ValidatorParticipationRound struct {
	Round rewards.Period
	*ValidatorParticipation
}

func (c *CalcCmd) validatorParticipations(
	ctx context.Context,
	period rewards.Period,
) ([]*ValidatorParticipation, error) {
	var participant []*ValidatorParticipation

	// Retrieve mechanics for the given period
	mechanics, err := c.plan.Mechanics.At(period)
	if err != nil {
		return nil, fmt.Errorf("failed to get mechanics for period %s: %w", period, err)
	}

	// Determine feature support
	gnosisSafeSupport := mechanics.Features.Enabled(rewards.FeatureGnosisSafe)
	ownerRedirectsSupport := len(mechanics.OwnerRedirects) > 0
	validatorRedirectsSupport := len(mechanics.ValidatorRedirects) > 0

	return participant, queries.Raw(
		"SELECT * FROM active_days_by_validator($1, $2, $3, $4, $5, $6, $7, $8)",
		c.PerformanceProvider,
		c.plan.Criteria.MinAttestationsPerDay,
		c.plan.Criteria.MinDecidedsPerDay,
		time.Time(period),
		nil, // to_period can be nil for single-period queries
		gnosisSafeSupport,
		ownerRedirectsSupport,
		validatorRedirectsSupport,
	).Bind(ctx, c.db, &participant)
}

type OwnerParticipation struct {
	OwnerAddress string
	Validators   int
	ActiveDays   int
	Reward       *precise.ETH `boil:"-"`
	reward       *big.Int     `boil:"-"`
}

type OwnerParticipationRound struct {
	Round rewards.Period
	*OwnerParticipation
}

func (c *CalcCmd) ownerParticipations(
	ctx context.Context,
	period rewards.Period,
) ([]*OwnerParticipation, error) {
	var rewards []*OwnerParticipation
	return rewards, queries.Raw(
		"SELECT * FROM active_days_by_owner($1, $2, $3, $4, $5, $6)",
		c.PerformanceProvider,                 // $1 -> provider_type
		c.plan.Criteria.MinAttestationsPerDay, // $2 -> integer
		c.plan.Criteria.MinDecidedsPerDay,     // $3 -> integer
		time.Time(period),                     // $4 -> date (from_period)
		nil,                                   // $5 -> date (to_period), use default
		false,                                 // $6 -> boolean (validator_redirects_support)
	).Bind(ctx, c.db, &rewards)
}

type RecipientParticipation struct {
	RecipientAddress string
	IsDeployer       bool
	ActiveDays       int
	Reward           *precise.ETH `boil:"-"`
	reward           *big.Int     `boil:"-"`
}

type RecipientParticipationRound struct {
	Round rewards.Period
	*RecipientParticipation
}

func (c *CalcCmd) recipientParticipations(
	ctx context.Context,
	period rewards.Period,
) ([]*RecipientParticipation, error) {
	mechanics, err := c.plan.Mechanics.At(period)
	if err != nil {
		return nil, fmt.Errorf("failed to get mechanics: %w", err)
	}
	gnosisSafeSupport := mechanics.Features.Enabled(rewards.FeatureGnosisSafe)

	ownerRedirectsSupport := len(mechanics.OwnerRedirects) > 0
	if ownerRedirectsSupport {
		err := c.populateOwnerRedirectsTable(ctx, mechanics.OwnerRedirects)
		if err != nil {
			return nil, fmt.Errorf("failed to populate reward redirects table: %w", err)
		}
	}

	validatorRedirectsSupport := len(mechanics.ValidatorRedirects) > 0
	if validatorRedirectsSupport {
		err := c.populateValidatorRedirectsTable(ctx, mechanics.ValidatorRedirects)
		if err != nil {
			return nil, fmt.Errorf("failed to populate reward redirects table: %w", err)
		}
	}

	var rewards []*RecipientParticipation
	return rewards, queries.Raw(
		"SELECT * FROM active_days_by_recipient($1, $2, $3, $4, $5, $6, $7, $8)",
		c.PerformanceProvider,
		c.plan.Criteria.MinAttestationsPerDay,
		c.plan.Criteria.MinDecidedsPerDay,
		time.Time(period),
		nil,
		gnosisSafeSupport,
		ownerRedirectsSupport,
		validatorRedirectsSupport,
	).Bind(ctx, c.db, &rewards)
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

func (c *CalcCmd) exclusions(
	ctx context.Context,
	fromPeriod rewards.Period,
	toPeriod rewards.Period,
) ([]*Exclusion, error) {
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
	err := queries.Raw(
		"SELECT * FROM inactive_days_by_validator($1, $2, $3, $4, $5)",
		c.PerformanceProvider,
		c.plan.Criteria.MinAttestationsPerDay,
		c.plan.Criteria.MinDecidedsPerDay,
		time.Time(fromPeriod),
		time.Time(toPeriod),
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

func exportCSV(data any, fileName string) error {
	// Use tabs as separators.
	gocsv.SetCSVWriter(func(out io.Writer) *gocsv.SafeCSVWriter {
		w := csv.NewWriter(out)
		w.Comma = '\t'
		return gocsv.NewSafeCSVWriter(w)
	})

	f, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("failed to create %q: %w", fileName, err)
	}
	defer f.Close()
	if err := gocsv.Marshal(data, f); err != nil {
		return fmt.Errorf("failed to marshal %q: %w", fileName, err)
	}
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
