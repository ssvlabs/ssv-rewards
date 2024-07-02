package rewards

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/bloxapp/ssv-rewards/pkg/precise"
	"gopkg.in/yaml.v3"
)

var (
	// validatorETHBalance is the ETH balance of an Ethereum validator.
	validatorETHBalance = precise.NewETH64(32)
)

type Plan struct {
	Version   int            `yaml:"version"`
	Criteria  Criteria       `yaml:"criteria"`
	Mechanics MechanicsSlice `yaml:"mechanics"`
	Rounds    Rounds         `yaml:"rounds"`
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

func (r *Plan) validate() error {
	// Mechanics.
	if len(r.Mechanics) == 0 {
		return errors.New("missing mechanics")
	}
	if !sort.IsSorted(r.Mechanics) {
		return errors.New("mechanics are not sorted by period")
	}
	for _, mechanics := range r.Mechanics {
		if time.Time(mechanics.Since).IsZero() {
			return errors.New("zero period in mechanics")
		}
		if len(mechanics.Tiers) == 0 {
			return errors.New("missing tiers in mechanics")
		}
		if !sort.IsSorted(mechanics.Tiers) {
			return errors.New("tiers are not sorted by max participants in mechanics")
		}
		if mechanics.Tiers[0].MaxParticipants == 0 {
			return errors.New("max participants must be positive in mechanics")
		}
		if len(mechanics.Tiers) > 1 {
			for i := 1; i < len(mechanics.Tiers); i++ {
				if mechanics.Tiers[i-1].MaxParticipants == mechanics.Tiers[i].MaxParticipants {
					return fmt.Errorf("duplicate tier: %d in mechanics", mechanics.Tiers[i].MaxParticipants)
				}
			}
		}
	}

	// Rounds.
	if len(r.Rounds) == 0 {
		return errors.New("missing rounds")
	}
	if !sort.IsSorted(r.Rounds) {
		return errors.New("rounds are not sorted by period")
	}
	for i := 1; i < len(r.Rounds); i++ {
		if r.Rounds[i-1].Period == r.Rounds[i].Period {
			return fmt.Errorf("duplicate round: %s", r.Rounds[i].Period)
		}
	}
	return nil
}

func (r *Plan) ValidatorRewards(
	period Period,
	participants int,
) (daily, monthly, annual *big.Int, err error) {
	tier, err := r.Tier(period, participants)
	if err != nil {
		err = fmt.Errorf("failed to determine tier: %w", err)
		return
	}
	for _, round := range r.Rounds {
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

func (p *Plan) Tier(period Period, participants int) (*Tier, error) {
	if participants <= 0 {
		return nil, errors.New("participants must be positive")
	}
	var mechanics *Mechanics
	for _, m := range p.Mechanics {
		if time.Time(m.Since).Before(time.Time(period)) {
			break
		}
		mechanics = &m
	}
	if mechanics == nil {
		return nil, errors.New("mechanics not found for the given period")
	}
	if !sort.IsSorted(mechanics.Tiers) {
		return nil, errors.New("tiers aren't sorted")
	}
	for _, tier := range mechanics.Tiers {
		if participants <= tier.MaxParticipants {
			return &tier, nil
		}
	}
	return nil, errors.New("participants exceed highest tier")
}

type Criteria struct {
	MinAttestationsPerDay int `yaml:"min_attestations_per_day"`
	MinDecidedsPerDay     int `yaml:"min_decideds_per_day"`
}

type Round struct {
	Period Period       `yaml:"period"`
	ETHAPR *precise.ETH `yaml:"eth_apr"`
	SSVETH *precise.ETH `yaml:"ssv_eth"`
}

type Rounds []Round

func (r Rounds) Len() int           { return len(r) }
func (r Rounds) Less(i, j int) bool { return time.Time(r[i].Period).Before(time.Time(r[j].Period)) }
func (r Rounds) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }

type MechanicsSlice []Mechanics

func (m MechanicsSlice) Len() int { return len(m) }
func (m MechanicsSlice) Less(i, j int) bool {
	return time.Time(m[i].Since).Before(time.Time(m[j].Since))
}
func (m MechanicsSlice) Swap(i, j int) { m[i], m[j] = m[j], m[i] }

type Mechanics struct {
	Since Period `yaml:"since"`
	Tiers Tiers  `yaml:"tiers"`
}

type Tier struct {
	MaxParticipants int          `yaml:"max_participants"`
	APRBoost        *precise.ETH `yaml:"apr_boost"`
}

type Tiers []Tier

func (t Tiers) Len() int           { return len(t) }
func (t Tiers) Less(i, j int) bool { return t[i].MaxParticipants < t[j].MaxParticipants }
func (t Tiers) Swap(i, j int)      { t[i], t[j] = t[j], t[i] }
