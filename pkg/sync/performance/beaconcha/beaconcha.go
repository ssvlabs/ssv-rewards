package beaconcha

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-rewards/pkg/beacon"
	"github.com/bloxapp/ssv-rewards/pkg/sync/httpretry"
	"github.com/bloxapp/ssv-rewards/pkg/sync/performance"
	"github.com/carlmjohnson/requests"
)

const (
	ProviderType performance.ProviderType = "beaconcha"
)

type Client struct {
	endpoint string
	apiKey   string
	ticker   *time.Ticker
	cache    map[phase0.ValidatorIndex][]dailyData
	cacheMu  sync.Mutex
}

func New(endpoint string, apiKey string, requestsPerMinute float64) *Client {
	return &Client{
		endpoint: endpoint,
		apiKey:   apiKey,
		ticker:   time.NewTicker(time.Duration(float64(time.Minute) / requestsPerMinute)),
		cache:    make(map[phase0.ValidatorIndex][]dailyData),
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
	m.cacheMu.Lock()
	data, ok := m.cache[index]
	m.cacheMu.Unlock()
	if !ok {
		// Fetch from the Beaconcha API.
		select {
		case <-m.ticker.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		var resp response
		err := requests.URL(m.endpoint).
			Client(httpretry.Client).
			Pathf("/api/v1/validator/stats/%d", index).
			Param("apikey", m.apiKey).
			ToJSON(&resp).
			Fetch(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch validator performance: %w", err)
		}
		if resp.Status != "OK" {
			return nil, fmt.Errorf("failed to fetch validator performance: %s", resp.Status)
		}
		data = resp.Data

		// Cache the data.
		m.cacheMu.Lock()
		m.cache[index] = data
		m.cacheMu.Unlock()
	}
	for _, d := range data {
		if d.DayStart.UTC().Truncate(24*time.Hour) != day.UTC().Truncate(24*time.Hour) {
			continue
		}

		// Count the number of active epochs in the day.
		activeEpochs := deriveActiveEpochs(spec, fromEpoch, toEpoch, activationEpoch, exitEpoch)

		performance := &performance.ValidatorPerformance{
			Attestations: performance.DutyPerformance{
				Assigned: int16(activeEpochs),
				Executed: int16(activeEpochs) - int16(d.MissedAttestations),
				Missed:   int16(d.MissedAttestations),
			},
			Proposals: performance.DutyPerformance{
				Assigned: int16(d.ProposedBlocks + d.MissedBlocks),
				Executed: int16(d.ProposedBlocks),
				Missed:   int16(d.MissedBlocks),
			},
			SyncCommittee: performance.DutyPerformance{
				Assigned: int16(d.ParticipatedSync + d.MissedSync),
				Executed: int16(d.ParticipatedSync),
				Missed:   int16(d.MissedSync),
			},
		}
		performance.AttestationRate = float32(
			performance.Attestations.Executed,
		) / float32(
			performance.Attestations.Assigned,
		)
		return performance, nil
	}
	return nil, nil
}

type response struct {
	Status string      `json:"status"`
	Data   []dailyData `json:"data"`
}

type dailyData struct {
	Day                   int       `json:"day"`
	AttesterSlashings     int       `json:"attester_slashings"`
	DayEnd                time.Time `json:"day_end"`
	DayStart              time.Time `json:"day_start"`
	Deposits              uint64    `json:"deposits"`
	DepositsAmount        uint64    `json:"deposits_amount"`
	EndBalance            uint64    `json:"end_balance"`
	EndEffectiveBalance   uint64    `json:"end_effective_balance"`
	MaxBalance            uint64    `json:"max_balance"`
	MaxEffectiveBalance   uint64    `json:"max_effective_balance"`
	MinBalance            uint64    `json:"min_balance"`
	MinEffectiveBalance   uint64    `json:"min_effective_balance"`
	MissedAttestations    int       `json:"missed_attestations"`
	MissedBlocks          int       `json:"missed_blocks"`
	MissedSync            int       `json:"missed_sync"`
	OrphanedAttestations  int       `json:"orphaned_attestations"`
	OrphanedBlocks        int       `json:"orphaned_blocks"`
	OrphanedSync          int       `json:"orphaned_sync"`
	ParticipatedSync      int       `json:"participated_sync"`
	ProposedBlocks        int       `json:"proposed_blocks"`
	ProposerSlashings     int       `json:"proposer_slashings"`
	StartBalance          uint64    `json:"start_balance"`
	StartEffectiveBalance uint64    `json:"start_effective_balance"`
	ValidatorIndex        int       `json:"validatorindex"`
	Withdrawals           uint64    `json:"withdrawals"`
	WithdrawalsAmount     uint64    `json:"withdrawals_amount"`
}

func deriveActiveEpochs(
	spec beacon.Spec, fromEpoch, toEpoch, activationEpoch, exitEpoch phase0.Epoch,
) phase0.Epoch {
	activeEpochs := toEpoch - fromEpoch + 1
	if activationEpoch > fromEpoch {
		if activationEpoch > toEpoch {
			activeEpochs = 0
		} else {
			activeEpochs -= activationEpoch - fromEpoch
		}
	}
	if exitEpoch != spec.FarFutureEpoch && exitEpoch <= toEpoch {
		if exitEpoch <= fromEpoch {
			activeEpochs = 0
		} else {
			activeEpochs -= toEpoch - exitEpoch + 1
		}
	}
	return activeEpochs
}
