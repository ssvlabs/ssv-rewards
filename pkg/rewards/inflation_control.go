package rewards

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/bloxapp/ssv-rewards/pkg/precise"
)

const (
	// BasisPointsPerUnit represents the number of basis points in 1 whole unit (100%)
	// 1 basis point = 0.01%, so 10000 basis points = 100%
	BasisPointsPerUnit = 10000
)

// InflationControl defines the configuration for inflation control mechanism
type InflationControl struct {
	// AnnualPercentage as a decimal (0.15 = 15%). Internally converted to basis points for
	// deterministic calculations. While the YAML uses float64 for readability, all calculations
	// use integer basis points to ensure exact, reproducible results.
	AnnualPercentage float64          `yaml:"annual_percentage"` // Decimal (0.15 = 15%)
	EnforcementStart Period           `yaml:"enforcement_start"`
	InterimMonthly   *precise.ETH     `yaml:"interim_monthly"`  // Monthly cap in SSV tokens (before snapshots)
	SupplySnapshots  []SupplySnapshot `yaml:"supply_snapshots"` // Must be sorted by period (ascending)
}

// SupplySnapshot defines a supply snapshot at a specific period
type SupplySnapshot struct {
	Period Period       `yaml:"period"`
	Supply *precise.ETH `yaml:"supply"` // In SSV tokens
}

// Validate validates the inflation control configuration
func (ic *InflationControl) Validate() error {
	// Validate annual percentage is reasonable (0 < x <= 1 = 100%)
	if ic.AnnualPercentage <= 0 || ic.AnnualPercentage > 1 {
		return fmt.Errorf("annual_percentage must be between 0 and 1, got %f", ic.AnnualPercentage)
	}

	// Validate enforcement start is not zero value
	if ic.EnforcementStart.IsZero() {
		return errors.New("enforcement_start is required")
	}

	// Validate interim monthly is positive
	if ic.InterimMonthly == nil || ic.InterimMonthly.Wei().Sign() <= 0 {
		return errors.New("interim_monthly must be positive")
	}

	// If snapshots exist, validate them
	if len(ic.SupplySnapshots) > 0 {
		// Validate enforcement start is before or equal to first snapshot
		if ic.SupplySnapshots[0].Period.Before(ic.EnforcementStart) {
			return fmt.Errorf("first snapshot period %s cannot be before enforcement_start %s",
				ic.SupplySnapshots[0].Period, ic.EnforcementStart)
		}

		// Validate snapshots are sorted by period and check for duplicates
		for i := 1; i < len(ic.SupplySnapshots); i++ {
			if ic.SupplySnapshots[i].Period.Equal(ic.SupplySnapshots[i-1].Period) {
				return fmt.Errorf("duplicate snapshot period: %s", ic.SupplySnapshots[i].Period)
			}
			if !ic.SupplySnapshots[i].Period.After(ic.SupplySnapshots[i-1].Period) {
				return errors.New("supply_snapshots must be sorted by period")
			}
		}

		// Validate supplies are positive
		for i, snapshot := range ic.SupplySnapshots {
			if snapshot.Supply == nil || snapshot.Supply.Wei().Sign() <= 0 {
				return fmt.Errorf("supply must be positive at snapshot %d", i)
			}
		}
	}

	return nil
}

// GetPeriodInflationCap calculates the inflation cap for a given period
// Returns the cap as *precise.ETH (representing SSV tokens with 18 decimal precision)
func (ic *InflationControl) GetPeriodInflationCap(period Period) (*precise.ETH, error) {
	// Check if enforcement has started
	if period.Before(ic.EnforcementStart) {
		return nil, nil // No cap before enforcement
	}

	// If no snapshots or period is before first snapshot, use interim monthly
	if len(ic.SupplySnapshots) == 0 || period.Before(ic.SupplySnapshots[0].Period) {
		// InterimMonthly is already in precise.ETH
		return ic.InterimMonthly, nil
	}

	// Find the applicable supply snapshot (most recent snapshot <= period)
	var supply *precise.ETH
	for i := len(ic.SupplySnapshots) - 1; i >= 0; i-- {
		if !period.Before(ic.SupplySnapshots[i].Period) {
			supply = ic.SupplySnapshots[i].Supply
			break
		}
	}

	// Ensure we have a valid supply
	if supply == nil {
		return nil, fmt.Errorf("no valid supply found for period %s", period)
	}

	// Calculate: (supply * annual_percentage) / 12
	// Convert percentage to basis points for integer math to ensure deterministic results
	// 0.15 -> 1500 basis points
	basisPoints := int64(ic.AnnualPercentage * BasisPointsPerUnit)

	// Calculate: (supply_wei * basis_points) / (BasisPointsPerUnit * 12)
	supplyWei := supply.Wei()
	annualCapWei := new(big.Int).Mul(supplyWei, big.NewInt(basisPoints))
	monthlyCapWei := new(big.Int).Div(annualCapWei, big.NewInt(BasisPointsPerUnit*12))

	// Convert back to precise.ETH
	monthlyCap := precise.NewETH(nil).SetWei(monthlyCapWei)
	return monthlyCap, nil
}
