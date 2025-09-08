package rewards

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv-rewards/pkg/precise"
)

func TestInflationControl_Validate(t *testing.T) {
	tests := []struct {
		name        string
		ic          *InflationControl
		expectedErr string
	}{
		{
			name: "valid inflation cap",
			ic: &InflationControl{
				AnnualPercentage: 0.15,
				EnforcementStart: NewPeriod(2025, 9),
				InterimMonthly:   precise.NewETH64(200000),
				SupplySnapshots: []SupplySnapshot{
					{Period: NewPeriod(2026, 1), Supply: precise.NewETH64(16000000)},
					{Period: NewPeriod(2027, 1), Supply: precise.NewETH64(18000000)},
				},
			},
		},
		{
			name: "invalid annual percentage - zero",
			ic: &InflationControl{
				AnnualPercentage: 0,
				EnforcementStart: NewPeriod(2025, 9),
				InterimMonthly:   precise.NewETH64(200000),
			},
			expectedErr: "annual_percentage must be between 0 and 1",
		},
		{
			name: "invalid annual percentage - over 1",
			ic: &InflationControl{
				AnnualPercentage: 1.5,
				EnforcementStart: NewPeriod(2025, 9),
				InterimMonthly:   precise.NewETH64(200000),
			},
			expectedErr: "annual_percentage must be between 0 and 1",
		},
		{
			name: "missing enforcement start",
			ic: &InflationControl{
				AnnualPercentage: 0.15,
				InterimMonthly:   precise.NewETH64(200000),
			},
			expectedErr: "enforcement_start is required",
		},
		{
			name: "missing interim monthly",
			ic: &InflationControl{
				AnnualPercentage: 0.15,
				EnforcementStart: NewPeriod(2025, 9),
			},
			expectedErr: "interim_monthly must be positive",
		},
		{
			name: "negative interim monthly",
			ic: &InflationControl{
				AnnualPercentage: 0.15,
				EnforcementStart: NewPeriod(2025, 9),
				InterimMonthly:   precise.NewETH64(-100),
			},
			expectedErr: "interim_monthly must be positive",
		},
		{
			name: "snapshot before enforcement start",
			ic: &InflationControl{
				AnnualPercentage: 0.15,
				EnforcementStart: NewPeriod(2025, 9),
				InterimMonthly:   precise.NewETH64(200000),
				SupplySnapshots: []SupplySnapshot{
					{Period: NewPeriod(2025, 8), Supply: precise.NewETH64(16000000)},
				},
			},
			expectedErr: "first snapshot period 2025-08 cannot be before enforcement_start 2025-09",
		},
		{
			name: "unsorted snapshots",
			ic: &InflationControl{
				AnnualPercentage: 0.15,
				EnforcementStart: NewPeriod(2025, 9),
				InterimMonthly:   precise.NewETH64(200000),
				SupplySnapshots: []SupplySnapshot{
					{Period: NewPeriod(2027, 1), Supply: precise.NewETH64(18000000)},
					{Period: NewPeriod(2026, 1), Supply: precise.NewETH64(16000000)},
				},
			},
			expectedErr: "supply_snapshots must be sorted by period",
		},
		{
			name: "duplicate snapshots",
			ic: &InflationControl{
				AnnualPercentage: 0.15,
				EnforcementStart: NewPeriod(2025, 9),
				InterimMonthly:   precise.NewETH64(200000),
				SupplySnapshots: []SupplySnapshot{
					{Period: NewPeriod(2026, 1), Supply: precise.NewETH64(16000000)},
					{Period: NewPeriod(2026, 1), Supply: precise.NewETH64(17000000)},
				},
			},
			expectedErr: "duplicate snapshot period: 2026-01",
		},
		{
			name: "negative supply",
			ic: &InflationControl{
				AnnualPercentage: 0.15,
				EnforcementStart: NewPeriod(2025, 9),
				InterimMonthly:   precise.NewETH64(200000),
				SupplySnapshots: []SupplySnapshot{
					{Period: NewPeriod(2026, 1), Supply: precise.NewETH64(-16000000)},
				},
			},
			expectedErr: "supply must be positive at snapshot 0",
		},
		{
			name: "nil supply",
			ic: &InflationControl{
				AnnualPercentage: 0.15,
				EnforcementStart: NewPeriod(2025, 9),
				InterimMonthly:   precise.NewETH64(200000),
				SupplySnapshots: []SupplySnapshot{
					{Period: NewPeriod(2026, 1), Supply: nil},
				},
			},
			expectedErr: "supply must be positive at snapshot 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ic.Validate()
			if tt.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.expectedErr)
			}
		})
	}
}

// requireETHEqual requires exact equality of ETH values
func requireETHEqual(t *testing.T, expected, actual *precise.ETH) {
	if expected == nil {
		require.Nil(t, actual)
		return
	}
	require.NotNil(t, actual)

	// Require exact equality - no tolerance for financial calculations
	require.Equal(t, 0, expected.Wei().Cmp(actual.Wei()),
		"expected %s, got %s", expected.String(), actual.String())
}

func TestInflationControl_GetPeriodInflationCap(t *testing.T) {
	ic := &InflationControl{
		AnnualPercentage: 0.15,
		EnforcementStart: NewPeriod(2025, 9),
		InterimMonthly:   precise.NewETH64(200000),
		SupplySnapshots: []SupplySnapshot{
			{Period: NewPeriod(2026, 1), Supply: precise.NewETH64(16000000)},
			{Period: NewPeriod(2027, 1), Supply: precise.NewETH64(18000000)},
		},
	}

	tests := []struct {
		name        string
		period      Period
		expectedCap *precise.ETH
	}{
		{
			name:        "before enforcement - no cap",
			period:      NewPeriod(2025, 8),
			expectedCap: nil,
		},
		{
			name:        "enforcement start - interim cap",
			period:      NewPeriod(2025, 9),
			expectedCap: precise.NewETH64(200000),
		},
		{
			name:        "during interim period - interim cap",
			period:      NewPeriod(2025, 12),
			expectedCap: precise.NewETH64(200000),
		},
		{
			name:        "first snapshot period - calculated cap",
			period:      NewPeriod(2026, 1),
			expectedCap: precise.NewETH64(200000), // (16M * 0.15) / 12 = 200,000
		},
		{
			name:        "between snapshots - use first snapshot",
			period:      NewPeriod(2026, 6),
			expectedCap: precise.NewETH64(200000), // Still using 16M supply
		},
		{
			name:        "second snapshot period - calculated cap",
			period:      NewPeriod(2027, 1),
			expectedCap: precise.NewETH64(225000), // (18M * 0.15) / 12 = 225,000
		},
		{
			name:        "after last snapshot - use last snapshot",
			period:      NewPeriod(2028, 1),
			expectedCap: precise.NewETH64(225000), // Still using 18M supply
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inflationCap, err := ic.GetPeriodInflationCap(tt.period)
			require.NoError(t, err)
			requireETHEqual(t, tt.expectedCap, inflationCap)
		})
	}
}

func TestInflationControl_GetPeriodInflationCap_NoSnapshots(t *testing.T) {
	ic := &InflationControl{
		AnnualPercentage: 0.15,
		EnforcementStart: NewPeriod(2025, 9),
		InterimMonthly:   precise.NewETH64(200000),
		SupplySnapshots:  []SupplySnapshot{}, // No snapshots
	}

	tests := []struct {
		name        string
		period      Period
		expectedCap *precise.ETH
	}{
		{
			name:        "before enforcement",
			period:      NewPeriod(2025, 8),
			expectedCap: nil,
		},
		{
			name:        "after enforcement - always interim cap",
			period:      NewPeriod(2025, 9),
			expectedCap: precise.NewETH64(200000),
		},
		{
			name:        "far future - still interim cap",
			period:      NewPeriod(2030, 1),
			expectedCap: precise.NewETH64(200000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inflationCap, err := ic.GetPeriodInflationCap(tt.period)
			require.NoError(t, err)
			requireETHEqual(t, tt.expectedCap, inflationCap)
		})
	}
}

func TestParseYAMLWithInflationControl(t *testing.T) {
	// Minimal valid plan with focus on inflation_cap parsing
	input := `
mechanics:
  - since: 2023-07
    criteria:
      min_attestations_per_day: 1
      min_decideds_per_day: 1
    tiers:
      - max_effective_balance: 32
        apr_boost: "0.1"

rounds:
  - period: 2023-07
    eth_apr: 0.04
    ssv_eth: 0.01

inflation_control:
  annual_percentage: 0.15
  enforcement_start: 2025-09
  interim_monthly: 200000
  supply_snapshots:
    - period: 2026-01
      supply: 16000000
    - period: 2027-01
      supply: 18000000
`
	expected := Plan{
		Version: 2,
		Mechanics: MechanicsList{
			{
				Since: NewPeriod(2023, time.July),
				Criteria: Criteria{
					MinAttestationsPerDay: 202,
					MinDecidedsPerDay:     22,
				},
				Tiers: Tiers{
					{MaxEffectiveBalance: precise.NewETH64(64000), APRBoost: mustParseETH("0.5")},
				},
			},
		},
		Rounds: []Round{
			{
				Period: NewPeriod(2023, time.July),
				ETHAPR: mustParseETH("0.047"),
				SSVETH: mustParseETH("0.0088235294"),
			},
		},
		InflationControl: &InflationControl{
			AnnualPercentage: 0.15,
			EnforcementStart: NewPeriod(2025, 9),
			InterimMonthly:   precise.NewETH64(200000),
			SupplySnapshots: []SupplySnapshot{
				{Period: NewPeriod(2026, 1), Supply: precise.NewETH64(16000000)},
				{Period: NewPeriod(2027, 1), Supply: precise.NewETH64(18000000)},
			},
		},
	}

	rewardPlan, err := ParsePlan([]byte(input))
	require.NoError(t, err)
	require.NotNil(t, rewardPlan)
	require.NotNil(t, rewardPlan.InflationControl)
	require.Equal(t, expected.InflationControl.AnnualPercentage, rewardPlan.InflationControl.AnnualPercentage)
	require.Equal(t, expected.InflationControl.EnforcementStart.String(), rewardPlan.InflationControl.EnforcementStart.String())
	require.Equal(t, 0, expected.InflationControl.InterimMonthly.Wei().Cmp(rewardPlan.InflationControl.InterimMonthly.Wei()))
	require.Equal(t, len(expected.InflationControl.SupplySnapshots), len(rewardPlan.InflationControl.SupplySnapshots))

	for i, snapshot := range expected.InflationControl.SupplySnapshots {
		require.Equal(t, snapshot.Period, rewardPlan.InflationControl.SupplySnapshots[i].Period)
		require.Equal(t, 0, snapshot.Supply.Wei().Cmp(rewardPlan.InflationControl.SupplySnapshots[i].Supply.Wei()))
	}
}

func TestPlan_GetPeriodInflationCap(t *testing.T) {
	tests := []struct {
		name        string
		plan        *Plan
		period      Period
		expectedCap *precise.ETH
	}{
		{
			name: "no inflation cap configured",
			plan: &Plan{
				InflationControl: nil,
			},
			period:      NewPeriod(2025, 9),
			expectedCap: nil,
		},
		{
			name: "with inflation cap",
			plan: &Plan{
				InflationControl: &InflationControl{
					AnnualPercentage: 0.15,
					EnforcementStart: NewPeriod(2025, 9),
					InterimMonthly:   precise.NewETH64(200000),
				},
			},
			period:      NewPeriod(2025, 9),
			expectedCap: precise.NewETH64(200000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inflationCap, err := tt.plan.GetPeriodInflationCap(tt.period)
			require.NoError(t, err)
			requireETHEqual(t, tt.expectedCap, inflationCap)
		})
	}
}
