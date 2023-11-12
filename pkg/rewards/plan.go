package rewards

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"gopkg.in/yaml.v2"
)

const (
	// validatorETHBalance is the ETH balance of an Ethereum validator.
	validatorETHBalance = 32
)

type Plan struct {
	Criteria Criteria `yaml:"criteria"`
	Tiers    Tiers    `yaml:"tiers"`
	Rounds   Rounds   `yaml:"rounds"`
}

// ParseYAML parses the given YAML document into a Plan.
func ParseYAML(data []byte) (*Plan, error) {
	var plan Plan
	if err := yaml.Unmarshal(data, &plan); err != nil {
		return nil, err
	}
	for i := range plan.Tiers {
		if plan.Tiers[i].MaxParticipants == 0 {
			plan.Tiers[i].MaxParticipants = math.MaxInt
			break
		}
	}
	if err := plan.validate(); err != nil {
		return nil, err
	}
	return &plan, nil
}

func (r *Plan) validate() error {
	// Tiers.
	if len(r.Tiers) == 0 {
		return errors.New("missing tiers")
	}
	if !sort.IsSorted(r.Tiers) {
		return errors.New("tiers are not sorted by max participants")
	}
	for i := 1; i < len(r.Tiers); i++ {
		if r.Tiers[i-1].MaxParticipants == r.Tiers[i].MaxParticipants {
			return fmt.Errorf("duplicate tier: %d", r.Tiers[i].MaxParticipants)
		}
	}
	if r.Tiers[len(r.Tiers)-1].MaxParticipants != math.MaxInt {
		return errors.New("last tier must not limit participants")
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

func (r *Plan) ValidatorRewards(period Period, participants int) (daily, monthly, annual float64, err error) {
	tier, err := r.Tier(participants)
	if err != nil {
		err = fmt.Errorf("failed to determine tier: %w", err)
		return
	}
	for _, round := range r.Rounds {
		if round.Period == period {
			annual = (validatorETHBalance * round.ETHAPR) / round.SSVETH * tier.APRBoost
			monthly = annual / 12
			daily = monthly / float64(period.Days())
			return
		}
	}
	err = errors.New("period not found")
	return
}

func (p *Plan) Tier(participants int) (*Tier, error) {
	if participants <= 0 {
		return nil, errors.New("participants must be positive")
	}
	if !sort.IsSorted(p.Tiers) {
		return nil, errors.New("tiers aren't sorted")
	}
	for _, tier := range p.Tiers {
		if participants <= tier.MaxParticipants {
			return &tier, nil
		}
	}
	return nil, errors.New("participants exceeds max tier")
}

type Criteria struct {
	MinAttestationsPerDay int `yaml:"min_attestations_per_day"`
	MinDecidedsPerDay     int `yaml:"min_decideds_per_day"`
}

type Round struct {
	Period Period  `yaml:"period"`
	ETHAPR float64 `yaml:"eth_apr"`
	SSVETH float64 `yaml:"ssv_eth"`
}

type Rounds []Round

func (r Rounds) Len() int           { return len(r) }
func (r Rounds) Less(i, j int) bool { return time.Time(r[i].Period).Before(time.Time(r[j].Period)) }
func (r Rounds) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }

type Tier struct {
	MaxParticipants int     `yaml:"max_participants"`
	APRBoost        float64 `yaml:"apr_boost"`
}

type Tiers []Tier

func (t Tiers) Len() int           { return len(t) }
func (t Tiers) Less(i, j int) bool { return t[i].MaxParticipants < t[j].MaxParticipants }
func (t Tiers) Swap(i, j int)      { t[i], t[j] = t[j], t[i] }
