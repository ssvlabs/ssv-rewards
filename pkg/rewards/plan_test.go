package rewards

import (
	"math"
	"testing"
	"time"

	"github.com/bloxapp/ssv-rewards/pkg/precise"
	"github.com/stretchr/testify/require"
)

func TestParseYAML(t *testing.T) {
	input := `
criteria:
  min_attestations_per_day: 202
  min_decideds_per_day: 22

mechanics:
  - since: 2023-07
    tiers:
      - max_participants: 2000
        apr_boost: 0.5
      - max_participants: 5000
        apr_boost: 0.4
      - max_participants: 10000
        apr_boost: 0.3
      - max_participants: 15000
        apr_boost: 0.2
      - max_participants: 30000
        apr_boost: 0.1
  - since: 2023-09
    tiers:
      - max_participants: 3000
        apr_boost: 0.05
      - max_participants: 6000
        apr_boost: 0.04

rounds:
  - period: 2023-07
    eth_apr: 0.047
    ssv_eth: 0.0088235294
  - period: 2023-08
    eth_apr: 0.048
    ssv_eth: 0.0088235294
  - period: 2023-09
    eth_apr: 0.049
    ssv_eth: 0.0088235294
  - period: 2023-10
    eth_apr: 
    ssv_eth: 
`
	expected := Plan{
		Criteria: Criteria{
			MinAttestationsPerDay: 202,
			MinDecidedsPerDay:     22,
		},
		Mechanics: MechanicsList{
			{
				Since: NewPeriod(2023, time.July),
				Tiers: Tiers{
					{MaxParticipants: 2000, APRBoost: mustParseETH("0.5")},
					{MaxParticipants: 5000, APRBoost: mustParseETH("0.4")},
					{MaxParticipants: 10000, APRBoost: mustParseETH("0.3")},
					{MaxParticipants: 15000, APRBoost: mustParseETH("0.2")},
					{MaxParticipants: 30000, APRBoost: mustParseETH("0.1")},
				},
			},
			{
				Since: NewPeriod(2023, time.September),
				Tiers: Tiers{
					{MaxParticipants: 3000, APRBoost: mustParseETH("0.05")},
					{MaxParticipants: 6000, APRBoost: mustParseETH("0.04")},
				},
			},
		},
		Rounds: []Round{
			{
				Period: NewPeriod(2023, time.July),
				ETHAPR: mustParseETH("0.047"),
				SSVETH: mustParseETH("0.0088235294"),
			},
			{
				Period: NewPeriod(2023, time.August),
				ETHAPR: mustParseETH("0.048"),
				SSVETH: mustParseETH("0.0088235294"),
			},
			{
				Period: NewPeriod(2023, time.September),
				ETHAPR: mustParseETH("0.049"),
				SSVETH: mustParseETH("0.0088235294"),
			},
			{
				Period: NewPeriod(2023, time.October),
			},
		},
	}
	rewardPlan, err := ParsePlan([]byte(input))
	require.NoError(t, err)
	require.NotNil(t, rewardPlan)
	require.Equal(t, expected, *rewardPlan)
}

func TestPlan_Validate(t *testing.T) {
	tests := []struct {
		name        string
		plan        *Plan
		expectedErr string
	}{
		{
			name:        "missing mechanics",
			plan:        &Plan{Rounds: Rounds{{Period: NewPeriod(2020, 1)}}},
			expectedErr: "missing mechanics",
		},
		{
			name:        "zero period in mechanics",
			plan:        &Plan{Mechanics: MechanicsList{{}}},
			expectedErr: "zero period in mechanics",
		},
		{
			name: "mechanics are not sorted",
			plan: &Plan{
				Mechanics: MechanicsList{
					{Since: NewPeriod(2020, 2)},
					{Since: NewPeriod(2020, 1)},
				},
			},
			expectedErr: "mechanics are not sorted by period",
		},
		{
			name: "missing tiers",
			plan: &Plan{
				Mechanics: MechanicsList{
					{Since: NewPeriod(2020, 1)},
				},
			},
			expectedErr: "missing tiers",
		},
		{
			name: "tiers not sorted",
			plan: &Plan{
				Mechanics: MechanicsList{
					{
						Since: NewPeriod(2020, 1),
						Tiers: Tiers{{MaxParticipants: 2}, {MaxParticipants: 1}},
					},
				},
				Rounds: Rounds{{Period: NewPeriod(2020, 1)}},
			},
			expectedErr: "tiers are not sorted by max participants in mechanics",
		},
		{
			name: "duplicate tier",
			plan: &Plan{
				Mechanics: MechanicsList{
					{
						Since: NewPeriod(2020, 1),
						Tiers: Tiers{{MaxParticipants: 1}, {MaxParticipants: 1}},
					},
				},
				Rounds: Rounds{{Period: NewPeriod(2020, 1)}},
			},
			expectedErr: "duplicate tier: 1 in mechanics",
		},
		{
			name: "zero max participants",
			plan: &Plan{
				Mechanics: MechanicsList{
					{
						Since: NewPeriod(2020, 1),
						Tiers: Tiers{{MaxParticipants: 0}},
					},
				},
				Rounds: Rounds{{Period: NewPeriod(2020, 1)}},
			},
			expectedErr: "max participants must be positive in mechanics",
		},
		{
			name:        "missing rounds",
			plan:        &Plan{Mechanics: MechanicsList{{Since: NewPeriod(2020, 1), Tiers: Tiers{{MaxParticipants: 1}, {MaxParticipants: math.MaxInt}}}}},
			expectedErr: "missing rounds",
		},
		{
			name: "rounds not sorted",
			plan: &Plan{
				Mechanics: MechanicsList{
					{
						Since: NewPeriod(2020, 1),
						Tiers: Tiers{{MaxParticipants: 1}, {MaxParticipants: math.MaxInt}},
					},
				},
				Rounds: Rounds{{Period: NewPeriod(2020, 2)}, {Period: NewPeriod(2020, 1)}},
			},
			expectedErr: "rounds are not sorted by period",
		},
		{
			name: "duplicate round",
			plan: &Plan{
				Mechanics: MechanicsList{
					{
						Since: NewPeriod(2020, 1),
						Tiers: Tiers{{MaxParticipants: 1}, {MaxParticipants: math.MaxInt}},
					},
				},
				Rounds: Rounds{{Period: NewPeriod(2020, 1)}, {Period: NewPeriod(2020, 1)}},
			},
			expectedErr: "duplicate round: 2020-01",
		},
		{
			name: "valid plan",
			plan: &Plan{
				Mechanics: MechanicsList{
					{
						Since: NewPeriod(2020, 1),
						Tiers: Tiers{{MaxParticipants: 1}, {MaxParticipants: math.MaxInt}},
					},
				},
				Rounds: Rounds{{Period: NewPeriod(2020, 1)}, {Period: NewPeriod(2020, 2)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plan.validate()
			if tt.expectedErr == "" {
				require.NoError(t, err, "%s: unexpected error: %v", tt.name, err)
			} else {
				require.ErrorContains(t, err, tt.expectedErr, "%s: unexpected error", tt.name)
			}
		})
	}
}

func mustParseETH(s string) *precise.ETH {
	e, err := precise.ParseETH(s)
	if err != nil {
		panic(err)
	}
	return e
}
