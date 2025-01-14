package rewards

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bloxapp/ssv-rewards/pkg/precise"
)

const (
	// FeatureGnosisSafe prevents rewarding deployer addresses of Gnosis Safes.
	FeatureGnosisSafe Feature = "gnosis_safe"
)

var AvailableFeatures = Features{
	FeatureGnosisSafe,
}

type Feature string

func (f Feature) String() string {
	return string(f)
}

func (f Feature) Valid() bool {
	return AvailableFeatures.Enabled(f)
}

type Features []Feature

func (f Features) Enabled(feature Feature) bool {
	for _, f := range f {
		if f == feature {
			return true
		}
	}
	return false
}

func (f Features) Validate() error {
	for _, feature := range f {
		if !feature.Valid() {
			return fmt.Errorf("invalid feature: %s", feature)
		}
	}
	return nil
}

type OwnerRedirects map[ExecutionAddress]ExecutionAddress
type ValidatorRedirects map[BLSPubKey]ExecutionAddress

type Mechanics struct {
	Since              Period             `yaml:"since"`
	Features           Features           `yaml:"features"`
	Tiers              Tiers              `yaml:"tiers"`
	OwnerRedirects     OwnerRedirects     `yaml:"owner_redirects"`
	ValidatorRedirects ValidatorRedirects `yaml:"validator_redirects"`
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
