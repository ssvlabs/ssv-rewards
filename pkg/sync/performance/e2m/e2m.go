package e2m

import (
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-rewards/pkg/beacon"
	"github.com/bloxapp/ssv-rewards/pkg/sync/httpretry"
	"github.com/bloxapp/ssv-rewards/pkg/sync/performance"
	"github.com/carlmjohnson/requests"
)

const (
	ProviderType performance.ProviderType = "e2m"
)

type stat struct {
	Assigned uint64
	Executed uint64
	Missed   uint64
}

type stats struct {
	Proposals       stat
	Attestations    stat
	SyncCommittee   stat
	Effectiveness   float64
	AttestationRate float64
}

type response struct {
	HeadSlot  phase0.Slot
	FromEpoch phase0.Epoch
	ToEpoch   phase0.Epoch
	Data      map[string]stats
}

type epochRange struct {
	fromEpoch phase0.Epoch
	toEpoch   phase0.Epoch
}

type Client struct {
	endpoint string
	cache    map[epochRange]map[string]stats
}

func New(endpoint string) *Client {
	return &Client{
		endpoint: endpoint,
		cache:    make(map[epochRange]map[string]stats),
	}
}

func (m *Client) Type() performance.ProviderType {
	return ProviderType
}

func (m *Client) ValidatorPerformance(
	ctx context.Context,
	spec beacon.Spec,
	day time.Time,
	fromEpoch, toEpoch, activationEpoch, exitEpoch phase0.Epoch,
	index phase0.ValidatorIndex,
) (*performance.ValidatorPerformance, error) {
	epochRange := epochRange{fromEpoch, toEpoch}
	data, ok := m.cache[epochRange]
	if !ok {
		var resp response
		err := requests.URL(m.endpoint).
			Client(httpretry.Client).
			Path("/api/stats/validators").
			ParamInt("from", int(fromEpoch)).
			ParamInt("to", int(toEpoch)).
			ToJSON(&resp).
			Fetch(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch validator performance: %w", err)
		}
		if spec.EpochAt(resp.HeadSlot) < toEpoch-1 {
			return nil, fmt.Errorf(
				"ethereum2-monitor is not up to date (head slot: %d)",
				resp.HeadSlot,
			)
		}
		data = resp.Data
	}
	validator, ok := data[fmt.Sprint(index)]
	if !ok {
		return nil, nil
	}
	effectiveness := float32(validator.Effectiveness)
	return &performance.ValidatorPerformance{
		Effectiveness:   &effectiveness,
		AttestationRate: float32(validator.AttestationRate),
		Proposals:       dutyStat(validator.Proposals),
		Attestations:    dutyStat(validator.Attestations),
		SyncCommittee:   dutyStat(validator.SyncCommittee),
	}, nil
}

func dutyStat(s stat) performance.DutyPerformance {
	return performance.DutyPerformance{
		Assigned: int16(s.Assigned),
		Executed: int16(s.Executed),
		Missed:   int16(s.Missed),
	}
}
