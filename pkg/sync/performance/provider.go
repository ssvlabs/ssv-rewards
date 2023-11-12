package performance

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-rewards/pkg/beacon"
)

type ProviderType string

type Provider interface {
	Type() ProviderType

	// ValidatorPerformance returns the performance of a validator for the given epoch range
	// or day (in that order of preference, depending on the provider's granularity).
	ValidatorPerformance(
		ctx context.Context,
		spec beacon.Spec,
		day time.Time,
		fromEpoch, toEpoch, activationEpoch, exitEpoch phase0.Epoch,
		index phase0.ValidatorIndex,
	) (*ValidatorPerformance, error)
}

type ValidatorPerformance struct {
	Effectiveness   *float32
	AttestationRate float32
	Proposals       DutyPerformance
	Attestations    DutyPerformance
	SyncCommittee   DutyPerformance
}

type DutyPerformance struct {
	Assigned int16
	Executed int16
	Missed   int16
}
