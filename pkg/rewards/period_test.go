package rewards

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParsePeriod(t *testing.T) {
	p, err := ParsePeriod("2006-01")
	require.NoError(t, err, "ParsePeriod failed")
	require.Equal(t, NewPeriod(2006, time.January), p, "ParsePeriod failed")
}

func TestPeriodString(t *testing.T) {
	p := NewPeriod(2006, time.January)
	require.Equal(t, "2006-01", p.String(), "Period.String failed")
}

func TestPeriodMarshalText(t *testing.T) {
	p := NewPeriod(2006, time.January)
	text, err := p.MarshalText()
	require.NoError(t, err, "Period.MarshalText failed")
	require.Equal(t, "2006-01", string(text), "Period.MarshalText failed")
}

func TestPeriodUnmarshalText(t *testing.T) {
	var p Period
	err := p.UnmarshalText([]byte("2006-01"))
	require.NoError(t, err, "Period.UnmarshalText failed")
	require.Equal(t, "2006-01", p.String(), "Period.UnmarshalText failed")
}

func TestPeriodBefore(t *testing.T) {
	tests := []struct {
		name     string
		p1       Period
		p2       Period
		expected bool
	}{
		{
			name:     "same period",
			p1:       NewPeriod(2025, 1),
			p2:       NewPeriod(2025, 1),
			expected: false,
		},
		{
			name:     "p1 before p2 - different months",
			p1:       NewPeriod(2025, 1),
			p2:       NewPeriod(2025, 2),
			expected: true,
		},
		{
			name:     "p1 after p2 - different months",
			p1:       NewPeriod(2025, 2),
			p2:       NewPeriod(2025, 1),
			expected: false,
		},
		{
			name:     "p1 before p2 - different years",
			p1:       NewPeriod(2024, 12),
			p2:       NewPeriod(2025, 1),
			expected: true,
		},
		{
			name:     "p1 after p2 - different years",
			p1:       NewPeriod(2025, 1),
			p2:       NewPeriod(2024, 12),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.p1.Before(tt.p2)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestPeriodAfter(t *testing.T) {
	tests := []struct {
		name     string
		p1       Period
		p2       Period
		expected bool
	}{
		{
			name:     "same period",
			p1:       NewPeriod(2025, 1),
			p2:       NewPeriod(2025, 1),
			expected: false,
		},
		{
			name:     "p1 after p2 - different months",
			p1:       NewPeriod(2025, 2),
			p2:       NewPeriod(2025, 1),
			expected: true,
		},
		{
			name:     "p1 before p2 - different months",
			p1:       NewPeriod(2025, 1),
			p2:       NewPeriod(2025, 2),
			expected: false,
		},
		{
			name:     "p1 after p2 - different years",
			p1:       NewPeriod(2025, 1),
			p2:       NewPeriod(2024, 12),
			expected: true,
		},
		{
			name:     "p1 before p2 - different years",
			p1:       NewPeriod(2024, 12),
			p2:       NewPeriod(2025, 1),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.p1.After(tt.p2)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestPeriodEqual(t *testing.T) {
	tests := []struct {
		name     string
		p1       Period
		p2       Period
		expected bool
	}{
		{
			name:     "same period - same instance",
			p1:       NewPeriod(2025, 1),
			p2:       NewPeriod(2025, 1),
			expected: true,
		},
		{
			name:     "different months",
			p1:       NewPeriod(2025, 1),
			p2:       NewPeriod(2025, 2),
			expected: false,
		},
		{
			name:     "different years",
			p1:       NewPeriod(2025, 1),
			p2:       NewPeriod(2024, 1),
			expected: false,
		},
		{
			name:     "both different",
			p1:       NewPeriod(2025, 3),
			p2:       NewPeriod(2024, 7),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.p1.Equal(tt.p2)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestPeriodComparisonConsistency(t *testing.T) {
	// Test that comparison methods are consistent with each other
	p1 := NewPeriod(2025, 1)
	p2 := NewPeriod(2025, 6)
	p3 := NewPeriod(2025, 1) // Same as p1

	// If p1.Before(p2) is true, then p2.After(p1) should also be true
	require.True(t, p1.Before(p2))
	require.True(t, p2.After(p1))

	// If p1.Equal(p3) is true, then neither Before nor After should be true
	require.True(t, p1.Equal(p3))
	require.False(t, p1.Before(p3))
	require.False(t, p1.After(p3))

	// A period should be equal to itself
	require.True(t, p1.Equal(p1))
	require.False(t, p1.Before(p1))
	require.False(t, p1.After(p1))
}

func TestPeriodIsZero(t *testing.T) {
	tests := []struct {
		name     string
		period   Period
		expected bool
	}{
		{
			name:     "zero period",
			period:   Period{},
			expected: true,
		},
		{
			name:     "non-zero period",
			period:   NewPeriod(2025, 1),
			expected: false,
		},
		{
			name:     "very old period",
			period:   NewPeriod(1970, 1),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.period.IsZero())
		})
	}
}
