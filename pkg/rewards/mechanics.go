package rewards

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bloxapp/ssv-rewards/pkg/precise"
)

type Criteria struct {
	MinAttestationsPerDay int `yaml:"min_attestations_per_day"`
	MinDecidedsPerDay     int `yaml:"min_decideds_per_day"`
}

func (c Criteria) Validate() error {
	if c == (Criteria{}) {
		return fmt.Errorf("missing criteria")
	}

	if c.MinAttestationsPerDay <= 0 {
		return fmt.Errorf("missing or invalid min_attestations_per_day in criteria")
	}

	if c.MinDecidedsPerDay <= 0 {
		return fmt.Errorf("missing or invalid min_decideds_per_day in criteria")
	}

	return nil
}

type OwnerRedirects map[ExecutionAddress]ExecutionAddress
type ValidatorRedirects map[BLSPubKey]ExecutionAddress

type Mechanics struct {
	Since    Period   `yaml:"since"`
	Tiers    Tiers    `yaml:"tiers"`
	Criteria Criteria `yaml:"criteria"`

	OwnerRedirects         OwnerRedirects     `yaml:"owner_redirects"`
	ValidatorRedirects     ValidatorRedirects `yaml:"validator_redirects"`
	OwnerRedirectsFile     string             `yaml:"owner_redirects_file"`
	ValidatorRedirectsFile string             `yaml:"validator_redirects_file"`
}

type Tier struct {
	MaxParticipants int          `yaml:"max_participants"`
	APRBoost        *precise.ETH `yaml:"apr_boost"`
}

type Tiers []Tier

func (t Tiers) Len() int           { return len(t) }
func (t Tiers) Less(i, j int) bool { return t[i].MaxParticipants < t[j].MaxParticipants }
func (t Tiers) Swap(i, j int)      { t[i], t[j] = t[j], t[i] }

type MechanicsList []Mechanics

func (m MechanicsList) At(period Period) (*Mechanics, error) {
	if !sort.IsSorted(m) {
		return nil, errors.New("mechanics list is not sorted")
	}
	var selection *Mechanics
	for _, v := range m {
		if v.Since.FirstDay().Before(period.FirstDay()) || v.Since.FirstDay().Equal(period.FirstDay()) {
			cpy := v
			selection = &cpy
		}
	}
	if selection == nil {
		return nil, errors.New("mechanics not found")
	}
	return selection, nil
}

func (m MechanicsList) Len() int { return len(m) }
func (m MechanicsList) Less(i, j int) bool {
	return time.Time(m[i].Since).Before(time.Time(m[j].Since))
}
func (m MechanicsList) Swap(i, j int) { m[i], m[j] = m[j], m[i] }
