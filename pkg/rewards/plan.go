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
	// validatorETHBalance is the ETH balance of an Ethereum validator.
	validatorETHBalance = precise.NewETH64(32)
)

type Plan struct {
	Version   int           `yaml:"version"`
	Mechanics MechanicsList `yaml:"mechanics"`
	Rounds    Rounds        `yaml:"rounds"`
}

// ParsePlan parses the given YAML document into a Plan.
func ParsePlan(data []byte) (*Plan, error) {
	var plan Plan
	if err := yaml.Unmarshal(data, &plan); err != nil {
		return nil, err
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
		if mechanics.Tiers[0].MaxEffectiveBalance == 0 {
			return errors.New("max effective balance must be positive in mechanics")
		}
		if len(mechanics.Tiers) > 1 {
			for j := 1; j < len(mechanics.Tiers); j++ {
				if mechanics.Tiers[j-1].MaxEffectiveBalance == mechanics.Tiers[j].MaxEffectiveBalance {
					return fmt.Errorf("duplicate tier: %d in mechanics", mechanics.Tiers[j].MaxEffectiveBalance)
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

	// Validate Rounds.
	if len(p.Rounds) == 0 {
		return errors.New("missing rounds")
	}
	if !sort.IsSorted(p.Rounds) {
		return errors.New("rounds are not sorted by period")
	}
	for i := 1; i < len(p.Rounds); i++ {
		round := p.Rounds[i-1]
		if round.NetworkFee != nil && round.NetworkFee.Gwei().Sign() < 0 {
			return fmt.Errorf("network_fee cannot be negative in round %s", round.Period)
		}
		if p.Rounds[i-1].Period == p.Rounds[i].Period {
			return fmt.Errorf("duplicate round: %s", p.Rounds[i].Period)
		}
	}
	return nil
}

func (p *Plan) ValidatorRewards(
	period Period,
	totalEffectiveBalanceGwei int64,
) (daily, monthly, annual *big.Int, err error) {
	tier, err := p.Tier(period, totalEffectiveBalanceGwei)
	if err != nil {
		err = fmt.Errorf("failed to determine tier: %w", err)
		return
	}
	for _, round := range p.Rounds {
		if round.Period == period {
			// (validatorETHBalance * round.ETHAPR) / round.SSVETH * tier.APRBoost
			annualETH := precise.NewETH(nil).Mul(validatorETHBalance, round.ETHAPR)
			annualETH.Quo(annualETH, round.SSVETH)
			annualETH.Mul(annualETH, tier.APRBoost)
			annual = annualETH.Wei()

			// annual / 12
			monthlyETH := precise.NewETH(nil).Quo(annualETH, precise.NewETH64(12))
			monthly = monthlyETH.Wei()

			// monthly / period.Days()
			dailyETH := precise.NewETH(nil).
				Quo(monthlyETH, precise.NewETH64(float64(period.Days())))
			daily = dailyETH.Wei()
			return
		}
	}
	err = errors.New("period not found")
	return
}

func (p *Plan) Tier(period Period, totalEffectiveBalanceGwei int64) (*Tier, error) {
	if totalEffectiveBalanceGwei <= 0 {
		return nil, errors.New("totalEffectiveBalance must be positive")
	}
	mechanics, err := p.Mechanics.At(period)
	if err != nil {
		return nil, fmt.Errorf("failed to get mechanics: %w", err)
	}
	if !sort.IsSorted(mechanics.Tiers) {
		return nil, errors.New("tiers aren't sorted")
	}

	totalEffectiveBalance := totalEffectiveBalanceGwei / 1e9 // Convert Gwei to Wei

	for _, tier := range mechanics.Tiers {
		if totalEffectiveBalance <= tier.MaxEffectiveBalance {
			return &tier, nil
		}
	}
	return nil, errors.New("totalEffectiveBalance exceed highest tier")
}

type Round struct {
	Period     Period       `yaml:"period"`
	ETHAPR     *precise.ETH `yaml:"eth_apr"`
	SSVETH     *precise.ETH `yaml:"ssv_eth"`
	NetworkFee *precise.ETH `yaml:"network_fee,omitempty"`
}

type Rounds []Round

func (r Rounds) Len() int           { return len(r) }
func (r Rounds) Less(i, j int) bool { return time.Time(r[i].Period).Before(time.Time(r[j].Period)) }
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
