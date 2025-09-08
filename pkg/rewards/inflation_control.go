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

type InflationControl struct {
	AnnualInflationLimit float64          `yaml:"annual_inflation_limit"` // Decimal (0.15 = 15%)
	EnforcementStart     Period           `yaml:"enforcement_start"`
	InterimMonthlyCap    *precise.ETH     `yaml:"interim_monthly_cap"` // Monthly cap in SSV tokens (before snapshots)
	SupplySnapshots      []SupplySnapshot `yaml:"supply_snapshots"`    // Must be sorted by period (ascending)
}

type SupplySnapshot struct {
	Period Period       `yaml:"period"`
	Supply *precise.ETH `yaml:"supply"` // In SSV tokens
}

func (ic *InflationControl) Validate() error {
	if ic.AnnualInflationLimit <= 0 || ic.AnnualInflationLimit > 1 {
		return fmt.Errorf("annual_inflation_limit must be between 0 and 1, got %f", ic.AnnualInflationLimit)
	}

	if ic.EnforcementStart.IsZero() {
		return errors.New("enforcement_start is required")
	}

	if ic.InterimMonthlyCap == nil || ic.InterimMonthlyCap.Wei().Sign() <= 0 {
		return errors.New("interim_monthly_cap must be positive")
	}

	if len(ic.SupplySnapshots) > 0 {
		if ic.SupplySnapshots[0].Period.Before(ic.EnforcementStart) {
			return fmt.Errorf("first snapshot period %s cannot be before enforcement_start %s",
				ic.SupplySnapshots[0].Period, ic.EnforcementStart)
		}

		for i := 1; i < len(ic.SupplySnapshots); i++ {
			if ic.SupplySnapshots[i].Period.Equal(ic.SupplySnapshots[i-1].Period) {
				return fmt.Errorf("duplicate snapshot period: %s", ic.SupplySnapshots[i].Period)
			}
			if !ic.SupplySnapshots[i].Period.After(ic.SupplySnapshots[i-1].Period) {
				return errors.New("supply_snapshots must be sorted by period")
			}
		}

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
	if period.Before(ic.EnforcementStart) {
		return nil, nil // No cap before enforcement
	}

	// If no snapshots or period is before first snapshot, use interim monthly cap
	if len(ic.SupplySnapshots) == 0 || period.Before(ic.SupplySnapshots[0].Period) {
		return ic.InterimMonthlyCap, nil
	}

	// Find the applicable supply snapshot (most recent snapshot <= period)
	var supply *precise.ETH
	for i := len(ic.SupplySnapshots) - 1; i >= 0; i-- {
		if !period.Before(ic.SupplySnapshots[i].Period) {
			supply = ic.SupplySnapshots[i].Supply
			break
		}
	}

	if supply == nil {
		return nil, fmt.Errorf("no valid supply found for period %s", period)
	}

	// Convert percentage to basis points for integer math (0.15 -> 1500)
	basisPoints := int64(ic.AnnualInflationLimit * BasisPointsPerUnit)

	supplyWei := supply.Wei()
	annualCapWei := new(big.Int).Mul(supplyWei, big.NewInt(basisPoints))
	monthlyCapWei := new(big.Int).Div(annualCapWei, big.NewInt(BasisPointsPerUnit*12))

	monthlyCap := precise.NewETH(nil).SetWei(monthlyCapWei)
	return monthlyCap, nil
}

// EvaluateInflationCap evaluates if inflation cap is exceeded for a given period and determines final rewards
// Returns whether scaling is needed, the final rewards amount, and the inflation cap
func (ic *InflationControl) EvaluateInflationCap(
	period Period,
	totalRoundRewards *precise.ETH,
) (needsScaling bool, finalRewards *precise.ETH, inflationCap *precise.ETH, err error) {
	inflationCap, err = ic.GetPeriodInflationCap(period)
	if err != nil {
		return false, nil, nil, fmt.Errorf("failed to get inflation cap: %w", err)
	}

	// Default: final rewards equal original rewards
	finalRewards = totalRoundRewards

	if inflationCap != nil && totalRoundRewards != nil {
		if totalRoundRewards.Wei().Cmp(inflationCap.Wei()) > 0 {
			needsScaling = true
			// If scaling needed, final rewards will be capped
			finalRewards = inflationCap
		}
	}

	return needsScaling, finalRewards, inflationCap, nil
}
