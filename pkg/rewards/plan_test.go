package rewards

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseYAML(t *testing.T) {
	input := `
criteria:
  min_attestations_per_day: 202
  min_decideds_per_day: 22

tiers:
  - max_participants: 2000
    apr_boost: 0.5
  - max_participants: 5000
    apr_boost: 0.4
  - max_participants: 10000
    apr_boost: 0.3
  - max_participants: 15000
    apr_boost: 0.2
  - max_participants: ~
    apr_boost: 0.1

rounds:
  - period: 2023-07
    eth_apr: 0.047
    ssv_eth: 0.0088235294
  - period: 2023-08
    eth_apr:
    ssv_eth:
`
	expected := Plan{
		Criteria: Criteria{
			MinAttestationsPerDay: 202,
			MinDecidedsPerDay:     22,
		},
		Tiers: []Tier{
			{MaxParticipants: 2000, APRBoost: 0.5},
			{MaxParticipants: 5000, APRBoost: 0.4},
			{MaxParticipants: 10000, APRBoost: 0.3},
			{MaxParticipants: 15000, APRBoost: 0.2},
			{MaxParticipants: math.MaxInt, APRBoost: 0.1},
		},
		Rounds: []Round{
			{Period: NewPeriod(2023, time.July), ETHAPR: 0.047, SSVETH: 0.0088235294},
			{Period: NewPeriod(2023, time.August)},
		},
	}
	rewardPlan, err := ParseYAML([]byte(input))
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
			name:        "missing tiers",
			plan:        &Plan{Rounds: Rounds{{Period: NewPeriod(2020, 1)}}},
			expectedErr: "missing tiers",
		},
		{
			name: "tiers not sorted",
			plan: &Plan{
				Tiers:  Tiers{{MaxParticipants: 2}, {MaxParticipants: 1}},
				Rounds: Rounds{{Period: NewPeriod(2020, 1)}},
			},
			expectedErr: "tiers are not sorted by max participants",
		},
		{
			name: "duplicate tier",
			plan: &Plan{
				Tiers:  Tiers{{MaxParticipants: 1}, {MaxParticipants: 1}},
				Rounds: Rounds{{Period: NewPeriod(2020, 1)}},
			},
			expectedErr: "duplicate tier",
		},
		{
			name: "last tier limit",
			plan: &Plan{
				Tiers:  Tiers{{MaxParticipants: 1}, {MaxParticipants: 2}},
				Rounds: Rounds{{Period: NewPeriod(2020, 1)}},
			},
			expectedErr: "last tier must not limit participants",
		},
		{
			name:        "missing rounds",
			plan:        &Plan{Tiers: Tiers{{MaxParticipants: 1}, {MaxParticipants: math.MaxInt}}},
			expectedErr: "missing rounds",
		},
		{
			name: "rounds not sorted",
			plan: &Plan{
				Tiers:  Tiers{{MaxParticipants: 1}, {MaxParticipants: math.MaxInt}},
				Rounds: Rounds{{Period: NewPeriod(2020, 2)}, {Period: NewPeriod(2020, 1)}},
			},
			expectedErr: "rounds are not sorted by period",
		},
		{
			name: "duplicate round",
			plan: &Plan{
				Tiers:  Tiers{{MaxParticipants: 1}, {MaxParticipants: math.MaxInt}},
				Rounds: Rounds{{Period: NewPeriod(2020, 1)}, {Period: NewPeriod(2020, 1)}},
			},
			expectedErr: "duplicate round",
		},
		{
			name: "valid plan",
			plan: &Plan{
				Tiers:  Tiers{{MaxParticipants: 1}, {MaxParticipants: math.MaxInt}},
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
