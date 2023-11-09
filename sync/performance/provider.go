package performance

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-rewards/beacon"
)

type ProviderType string

type DutyStat struct {
	Assigned int16
	Executed int16
	Missed   int16
}

type ValidatorPerformance struct {
	Effectiveness   *float32
	AttestationRate float32
	Proposals       DutyStat
	Attestations    DutyStat
	SyncCommittee   DutyStat
}

type Provider interface {
	Type() ProviderType
	ValidatorPerformance(
		ctx context.Context,
		spec beacon.Spec,
		day time.Time,
		fromEpoch, toEpoch, activationEpoch, exitEpoch phase0.Epoch,
		index phase0.ValidatorIndex,
	) (*ValidatorPerformance, error)
}
