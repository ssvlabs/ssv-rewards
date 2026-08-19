package rewards

import (
	"encoding/csv"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/bloxapp/ssv-rewards/pkg/precise"
)

var (
	// BaseEffectiveBalance is the base ETH effective balance of an Ethereum validator (32 ETH).
	BaseEffectiveBalance = precise.NewETH64(32)

	// defaultLegacyCalculationCutoff is the default cutoff period for legacy calculation methods.
	// Set to 2025-08 to preserve merkle roots for already published periods.
	defaultLegacyCalculationCutoff = NewPeriod(2025, 8)
)

type Plan struct {
	Version   int           `yaml:"version"`
	Mechanics MechanicsList `yaml:"mechanics"`
	Rounds    Rounds        `yaml:"rounds"`

	// LegacyCalculationCutoff defines the cutoff for using legacy reward calculation methods.
	// Periods before this date use:
	//   - SQL-aggregated data (causing fee calculation issues for multi-validator recipients)
	//   - Daily rewards that vary by month length (monthly = annual/12, then daily = monthly/days_in_month)
	// Periods from this date onwards use:
	//   - Per-validator fee calculation before aggregation (correct for multi-validator recipients)
	//   - Constant daily rewards (daily = annual/365, then monthly = daily * days_in_month)
	// Default: defaultLegacyCalculationCutoff (2025-08)
	LegacyCalculationCutoff Period `yaml:"legacy_calculation_cutoff,omitempty"`

	StakingUpgrade *StakingUpgrade `yaml:"staking_upgrade,omitempty"`
}

// StakingUpgrade is the on-chain event position that separates pre-upgrade
// validators (SSV reward tree) from post-upgrade validators (ETH reward tree).
type StakingUpgrade struct {
	Block    int `yaml:"block"`
	LogIndex int `yaml:"log_index"`
}

// ParsePlan parses the given YAML document into a Plan.
func ParsePlan(data []byte) (*Plan, error) {
	var plan Plan
	if err := yaml.Unmarshal(data, &plan); err != nil {
		return nil, err
	}

	// Set default LegacyCalculationCutoff if not specified
	if plan.LegacyCalculationCutoff.IsZero() {
		plan.LegacyCalculationCutoff = defaultLegacyCalculationCutoff
	}

	if err := plan.validate(); err != nil {
		return nil, err
	}
	return &plan, nil
}

func (p *Plan) validate() error {
	// Validate Mechanics.
	if len(p.Mechanics) == 0 {
		return errors.New("missing mechanics")
	}
	if !sort.IsSorted(p.Mechanics) {
		return errors.New("mechanics are not sorted by period")
	}
	for i := range p.Mechanics { // Use index to modify Mechanics in-place
		mechanics := &p.Mechanics[i]

		if time.Time(mechanics.Since).IsZero() {
			return errors.New("zero period in mechanics")
		}
		if len(mechanics.Tiers) == 0 {
			return errors.New("missing tiers in mechanics")
		}
		if !sort.IsSorted(mechanics.Tiers) {
			return errors.New("tiers are not sorted by max effective balance in mechanics")
		}

		for j, tier := range mechanics.Tiers {
			if tier.MaxEffectiveBalance == nil || tier.MaxEffectiveBalance.Wei().Sign() <= 0 {
				return fmt.Errorf("max effective balance must be positive in mechanics at tier %d", j)
			}
			if tier.APRBoost == nil || tier.APRBoost.Wei().Sign() < 0 {
				return fmt.Errorf("apr_boost must be non-negative in mechanics at tier %d", j)
			}
		}

		if len(mechanics.Tiers) > 1 {
			for j := 1; j < len(mechanics.Tiers); j++ {
				if mechanics.Tiers[j-1].MaxEffectiveBalance.Wei().Cmp(mechanics.Tiers[j].MaxEffectiveBalance.Wei()) == 0 {
					return fmt.Errorf("duplicate tier: %s in mechanics", mechanics.Tiers[j].MaxEffectiveBalance.String())
				}
			}
		}

		if err := mechanics.Criteria.Validate(); err != nil {
			return fmt.Errorf("failed to validate criteria at period %s: %w", mechanics.Since, err)
		}

		// Check for conflicting redirects.
		if len(mechanics.OwnerRedirects) > 0 && mechanics.OwnerRedirectsFile != "" {
			return fmt.Errorf("both owner_redirects and owner_redirects_file specified for period %s", mechanics.Since)
		}
		if len(mechanics.ValidatorRedirects) > 0 && mechanics.ValidatorRedirectsFile != "" {
			return fmt.Errorf("both validator_redirects and validator_redirects_file specified for period %s", mechanics.Since)
		}

		// Load CSV redirects if specified.
		if mechanics.OwnerRedirectsFile != "" {
			loadedRedirects, err := loadOwnerRedirectsFromCSV(mechanics.OwnerRedirectsFile)
			if err != nil {
				return fmt.Errorf("failed to load owner redirects from file %q: %w", mechanics.OwnerRedirectsFile, err)
			}
			mechanics.OwnerRedirects = loadedRedirects
		}
		if mechanics.ValidatorRedirectsFile != "" {
			loadedRedirects, err := loadValidatorRedirectsFromCSV(mechanics.ValidatorRedirectsFile)
			if err != nil {
				return fmt.Errorf("failed to load validator redirects from file %q: %w", mechanics.ValidatorRedirectsFile, err)
			}
			mechanics.ValidatorRedirects = loadedRedirects
		}
	}

	// Validate upgrade boundary.
	if p.StakingUpgrade != nil {
		if p.StakingUpgrade.Block <= 0 {
			return errors.New("staking_upgrade.block must be positive")
		}
		if p.StakingUpgrade.LogIndex < 0 {
			return errors.New("staking_upgrade.log_index must be non-negative")
		}
	}

	// Validate Rounds.
	if len(p.Rounds) == 0 {
		return errors.New("missing rounds")
	}
	if !sort.IsSorted(p.Rounds) {
		return errors.New("rounds are not sorted by period")
	}
	for i := 0; i < len(p.Rounds); i++ {
		round := p.Rounds[i]
		if round.NetworkFee != nil && round.NetworkFee.Wei().Sign() < 0 {
			return fmt.Errorf("network_fee cannot be negative in round %s", round.Period)
		}
		if round.InflationCap != nil && round.InflationCap.Wei().Sign() <= 0 {
			return fmt.Errorf("inflation_cap must be positive if specified in round %s", round.Period)
		}
		if i > 0 && p.Rounds[i-1].Period == p.Rounds[i].Period {
			return fmt.Errorf("duplicate round: %s", p.Rounds[i].Period)
		}
	}

	return nil
}

// ValidatorRewardsLegacy calculates rewards with the original bug where daily rewards
// vary by month length. Used for periods before the legacy cutoff to preserve merkle roots.
func (p *Plan) ValidatorRewardsLegacy(
	period Period,
	tier *Tier,
) (daily, monthly, annual *big.Int, err error) {
	for _, round := range p.Rounds {
		if round.Period == period {
			// (BaseEffectiveBalance * round.ETHAPR) / round.SSVETH * tier.APRBoost
			annualETH := precise.NewETH(nil).Mul(BaseEffectiveBalance, round.ETHAPR)
			annualETH.Quo(annualETH, round.SSVETH)
			annualETH.Mul(annualETH, tier.APRBoost)
			annual = annualETH.Wei()

			monthlyETH := precise.NewETH(nil).Quo(annualETH, precise.NewETH64(12))
			monthly = monthlyETH.Wei()

			dailyETH := precise.NewETH(nil).Quo(monthlyETH, precise.NewETH64(float64(period.Days())))
			daily = dailyETH.Wei()
			return
		}
	}
	err = errors.New("period not found")
	return
}

// ValidatorRewards calculates rewards with correct daily rate (annual/365).
// Used for periods from the legacy cutoff onwards.
func (p *Plan) ValidatorRewards(
	period Period,
	tier *Tier,
) (daily, monthly, annual *big.Int, err error) {
	for _, round := range p.Rounds {
		if round.Period == period {
			// (BaseEffectiveBalance * round.ETHAPR) / round.SSVETH * tier.APRBoost
			annualETH := precise.NewETH(nil).Mul(BaseEffectiveBalance, round.ETHAPR)
			annualETH.Quo(annualETH, round.SSVETH)
			annualETH.Mul(annualETH, tier.APRBoost)
			annual = annualETH.Wei()

			dailyETH := precise.NewETH(nil).Quo(annualETH, precise.NewETH64(365))
			daily = dailyETH.Wei()

			monthlyETH := precise.NewETH(nil).Mul(dailyETH, precise.NewETH64(float64(period.Days())))
			monthly = monthlyETH.Wei()
			return
		}
	}
	err = errors.New("period not found")
	return
}

func (p *Plan) Tier(period Period, totalEffectiveBalance *precise.ETH) (*Tier, error) {
	if totalEffectiveBalance == nil || totalEffectiveBalance.Wei().Sign() <= 0 {
		return nil, errors.New("totalEffectiveBalance must be positive")
	}
	mechanics, err := p.Mechanics.At(period)
	if err != nil {
		return nil, fmt.Errorf("failed to get mechanics: %w", err)
	}

	for _, tier := range mechanics.Tiers {
		if totalEffectiveBalance.Wei().Cmp(tier.MaxEffectiveBalance.Wei()) <= 0 {
			return &tier, nil
		}
	}
	return nil, errors.New("totalEffectiveBalance exceed highest tier")
}

type Round struct {
	Period       Period       `yaml:"period"`
	ETHAPR       *precise.ETH `yaml:"eth_apr"`
	SSVETH       *precise.ETH `yaml:"ssv_eth"`
	NetworkFee   *precise.ETH `yaml:"network_fee,omitempty"`
	InflationCap *precise.ETH `yaml:"inflation_cap,omitempty"`
}

type Rounds []Round

func (r Rounds) Len() int           { return len(r) }
func (r Rounds) Less(i, j int) bool { return r[i].Period.Before(r[j].Period) }
func (r Rounds) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }

func loadOwnerRedirectsFromCSV(filePath string) (OwnerRedirects, error) {
	if filePath == "" {
		return nil, nil
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open owner redirects CSV file %q: %w", filePath, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read the first row and ensure it is the header row "from,to".
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read header from CSV file %q: %w", filePath, err)
	}
	if len(header) != 2 || !strings.EqualFold(header[0], "from") || !strings.EqualFold(header[1], "to") {
		return nil, fmt.Errorf("invalid or missing header in CSV file %q: expected 'from,to'", filePath)
	}

	// Read the remaining records.
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV file %q: %w", filePath, err)
	}

	redirects := make(OwnerRedirects)
	for i, record := range records {
		if len(record) != 2 {
			return nil, fmt.Errorf("invalid CSV format on line %d", i+2) // +2 accounts for the header row
		}

		from, err := ExecutionAddressFromHex(record[0])
		if err != nil {
			return nil, fmt.Errorf("invalid execution address on line %d: %w", i+2, err)
		}

		to, err := ExecutionAddressFromHex(record[1])
		if err != nil {
			return nil, fmt.Errorf("invalid execution address on line %d: %w", i+2, err)
		}

		// Check for duplicate "from" keys.
		if _, exists := redirects[from]; exists {
			return nil, fmt.Errorf("duplicate entry for 'from' address on line %d: %s", i+2, record[0])
		}

		redirects[from] = to
	}
	return redirects, nil
}

func loadValidatorRedirectsFromCSV(filePath string) (ValidatorRedirects, error) {
	if filePath == "" {
		return nil, nil
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open validator redirects CSV file %q: %w", filePath, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read the first row and ensure it is the header row "from,to".
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read header from CSV file %q: %w", filePath, err)
	}
	if len(header) != 2 || !strings.EqualFold(header[0], "from") || !strings.EqualFold(header[1], "to") {
		return nil, fmt.Errorf("invalid or missing header in CSV file %q: expected 'from,to'", filePath)
	}

	// Read the remaining records.
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV file %q: %w", filePath, err)
	}

	redirects := make(ValidatorRedirects)
	for i, record := range records {
		if len(record) != 2 {
			return nil, fmt.Errorf("invalid CSV format on line %d", i+2) // +2 accounts for the header row
		}

		from, err := BLSPubKeyFromHex(record[0])
		if err != nil {
			return nil, fmt.Errorf("invalid BLS public key on line %d: %w", i+2, err)
		}

		to, err := ExecutionAddressFromHex(record[1])
		if err != nil {
			return nil, fmt.Errorf("invalid execution address on line %d: %w", i+2, err)
		}

		// Check for duplicate "from" keys.
		if _, exists := redirects[from]; exists {
			return nil, fmt.Errorf("duplicate entry for 'from' key on line %d: %s", i+2, record[0])
		}

		redirects[from] = to
	}
	return redirects, nil
}
