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
