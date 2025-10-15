package precise

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewETH(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		eth := NewETH(nil)
		require.Equal(t, "0.000000000000000000", eth.String())
	})

	t.Run("zero", func(t *testing.T) {
		eth := NewETH(big.NewFloat(0))
		require.Equal(t, "0.000000000000000000", eth.String())
	})

	t.Run("one", func(t *testing.T) {
		eth := NewETH(big.NewFloat(1))
		require.Equal(t, "1.000000000000000000", eth.String())
	})

	t.Run("very small", func(t *testing.T) {
		eth := NewETH(big.NewFloat(0.000000000000000001))
		require.Equal(t, "0.000000000000000001", eth.String())
	})
}

func TestNewETH64(t *testing.T) {
	t.Run("zero", func(t *testing.T) {
		eth := NewETH64(0)
		require.Equal(t, "0.000000000000000000", eth.String())
	})

	t.Run("integer", func(t *testing.T) {
		eth := NewETH64(100)
		require.Equal(t, "100.000000000000000000", eth.String())
	})

	t.Run("negative", func(t *testing.T) {
		eth := NewETH64(-50.5)
		require.Equal(t, "-50.500000000000000000", eth.String())
	})
}

func TestParseETH(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{
			name:     "integer",
			input:    "100",
			expected: "100.000000000000000000",
		},
		{
			name:     "decimal",
			input:    "123.456789",
			expected: "123.456789000000000000",
		},
		{
			name:     "scientific notation",
			input:    "1.5e2",
			expected: "150.000000000000000000",
		},
		{
			name:        "invalid",
			input:       "not a number",
			expectError: true,
		},
		{
			name:     "negative",
			input:    "-10.5",
			expected: "-10.500000000000000000",
		},
		{
			name:     "very precise",
			input:    "0.123456789012345678",
			expected: "0.123456789012345678",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eth, err := ParseETH(tt.input)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, eth.String())
			}
		})
	}
}

func TestETH_Wei(t *testing.T) {
	tests := []struct {
		name     string
		eth      *ETH
		expected *big.Int
	}{
		{
			name:     "zero",
			eth:      NewETH64(0),
			expected: big.NewInt(0),
		},
		{
			name:     "one ETH",
			eth:      NewETH64(1),
			expected: new(big.Int).SetUint64(1e18),
		},
		{
			name:     "decimal ETH",
			eth:      NewETH64(0.5),
			expected: new(big.Int).SetUint64(5e17),
		},
		{
			name:     "large amount",
			eth:      NewETH64(1000000),
			expected: new(big.Int).Mul(big.NewInt(1000000), new(big.Int).SetUint64(1e18)),
		},
		{
			name:     "very small amount",
			eth:      NewETH64(0.000000000000000001), // 1 Wei
			expected: big.NewInt(1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wei := tt.eth.Wei()
			require.Equal(t, 0, tt.expected.Cmp(wei), "expected %s, got %s", tt.expected.String(), wei.String())
		})
	}
}

func TestETH_SetWei(t *testing.T) {
	tests := []struct {
		name     string
		wei      *big.Int
		expected string
	}{
		{
			name:     "zero",
			wei:      big.NewInt(0),
			expected: "0.000000000000000000",
		},
		{
			name:     "one Wei",
			wei:      big.NewInt(1),
			expected: "0.000000000000000001",
		},
		{
			name:     "one ETH in Wei",
			wei:      new(big.Int).SetUint64(1e18),
			expected: "1.000000000000000000",
		},
		{
			name:     "decimal ETH in Wei",
			wei:      new(big.Int).SetUint64(1234567890123456789),
			expected: "1.234567890123456789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eth := NewETH(nil).SetWei(tt.wei)
			require.Equal(t, tt.expected, eth.String())
		})
	}
}

func TestETH_SetGwei(t *testing.T) {
	tests := []struct {
		name     string
		gwei     *big.Int
		expected string
	}{
		{
			name:     "zero",
			gwei:     big.NewInt(0),
			expected: "0.000000000000000000",
		},
		{
			name:     "one Gwei",
			gwei:     big.NewInt(1),
			expected: "0.000000001000000000",
		},
		{
			name:     "one ETH in Gwei",
			gwei:     big.NewInt(1e9),
			expected: "1.000000000000000000",
		},
		{
			name:     "32 ETH in Gwei (validator balance)",
			gwei:     big.NewInt(32e9),
			expected: "32.000000000000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eth := NewETH(nil).SetGwei(tt.gwei)
			require.Equal(t, tt.expected, eth.String())
		})
	}
}

func TestETH_Arithmetic(t *testing.T) {
	t.Run("Add integers", func(t *testing.T) {
		a := NewETH64(10)
		b := NewETH64(20)
		result := NewETH(nil).Add(a, b)
		require.Equal(t, "30.000000000000000000", result.String())
	})

	t.Run("Sub integers", func(t *testing.T) {
		a := NewETH64(30)
		b := NewETH64(10)
		result := NewETH(nil).Sub(a, b)
		require.Equal(t, "20.000000000000000000", result.String())
	})

	t.Run("Mul", func(t *testing.T) {
		a := NewETH64(10)
		b := NewETH64(2.5)
		result := NewETH(nil).Mul(a, b)
		require.Equal(t, "25.000000000000000000", result.String())
	})

	t.Run("Quo", func(t *testing.T) {
		a := NewETH64(100)
		b := NewETH64(4)
		result := NewETH(nil).Quo(a, b)
		require.Equal(t, "25.000000000000000000", result.String())
	})

	t.Run("Chaining operations", func(t *testing.T) {
		// (10 + 5) * 2 / 3 = 10
		result := NewETH(nil).Add(NewETH64(10), NewETH64(5))
		result.Mul(result, NewETH64(2))
		result.Quo(result, NewETH64(3))
		require.Equal(t, "10.000000000000000000", result.String())
	})
}

func TestETH_MarshalUnmarshalText(t *testing.T) {
	tests := []struct {
		name string
		eth  *ETH
	}{
		{
			name: "zero",
			eth:  NewETH64(0),
		},
		{
			name: "integer",
			eth:  NewETH64(123),
		},
		{
			name: "very small",
			eth:  NewETH64(0.000000000000000001),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := tt.eth.MarshalText()
			require.NoError(t, err)

			// Unmarshal
			eth2 := NewETH(nil)
			err = eth2.UnmarshalText(data)
			require.NoError(t, err)

			// Compare
			require.Equal(t, tt.eth.String(), eth2.String())
		})
	}
}

func TestETH_MarshalUnmarshalJSON(t *testing.T) {
	type TestStruct struct {
		Amount *ETH `json:"amount"`
	}

	t.Run("zero", func(t *testing.T) {
		s := TestStruct{Amount: NewETH64(0)}
		data, err := json.Marshal(s)
		require.NoError(t, err)
		require.JSONEq(t, `{"amount":"0.000000000000000000"}`, string(data))

		var s2 TestStruct
		err = json.Unmarshal(data, &s2)
		require.NoError(t, err)
		require.Equal(t, s.Amount.String(), s2.Amount.String())
	})

	t.Run("integer", func(t *testing.T) {
		s := TestStruct{Amount: NewETH64(100)}
		data, err := json.Marshal(s)
		require.NoError(t, err)

		var s2 TestStruct
		err = json.Unmarshal(data, &s2)
		require.NoError(t, err)
		require.Equal(t, s.Amount.String(), s2.Amount.String())
	})
}

func TestETH_WeiComparison(t *testing.T) {
	tests := []struct {
		name     string
		a        *ETH
		b        *ETH
		expected int
	}{
		{
			name:     "equal",
			a:        NewETH64(10),
			b:        NewETH64(10),
			expected: 0,
		},
		{
			name:     "less than",
			a:        NewETH64(5),
			b:        NewETH64(10),
			expected: -1,
		},
		{
			name:     "greater than",
			a:        NewETH64(10),
			b:        NewETH64(5),
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.a.Wei().Cmp(tt.b.Wei())
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestETH_PrecisionPreservation(t *testing.T) {
	t.Run("division precision", func(t *testing.T) {
		// 16000000 * 0.15 / 12 should be very close to 200000
		supply := NewETH64(16000000)
		percentage := NewETH64(0.15)
		months := NewETH64(12)

		annualCap := NewETH(nil).Mul(supply, percentage)
		monthlyCap := NewETH(nil).Quo(annualCap, months)

		// Should be very close to 200000
		expected := NewETH64(200000)
		diff := NewETH(nil).Sub(expected, monthlyCap)
		absDiff := NewETH(nil).SetWei(diff.Wei())
		if diff.Wei().Sign() < 0 {
			absDiff.SetWei(new(big.Int).Neg(diff.Wei()))
		}

		// Allow tiny difference due to floating point
		tolerance := NewETH64(0.000001)
		require.True(t, absDiff.Wei().Cmp(tolerance.Wei()) <= 0,
			"Expected ~200000, got %s (diff: %s)", monthlyCap.String(), absDiff.String())
	})

	t.Run("wei roundtrip for integers", func(t *testing.T) {
		// Test with exact values that don't have floating point issues
		original := NewETH64(123)
		wei := original.Wei()
		recovered := NewETH(nil).SetWei(wei)

		// Should maintain exact precision for integers
		require.Equal(t, original.String(), recovered.String())
	})

	t.Run("gwei roundtrip", func(t *testing.T) {
		// Test Gwei conversion roundtrip
		original := big.NewInt(32000000000) // 32 ETH in Gwei
		eth := NewETH(nil).SetGwei(original)
		gwei := eth.Gwei()

		require.Equal(t, 0, original.Cmp(gwei), "Gwei roundtrip failed")
	})
}

func TestETH_Gwei(t *testing.T) {
	tests := []struct {
		name     string
		eth      *ETH
		expected *big.Int
	}{
		{
			name:     "zero",
			eth:      NewETH64(0),
			expected: big.NewInt(0),
		},
		{
			name:     "one ETH",
			eth:      NewETH64(1),
			expected: big.NewInt(1e9),
		},
		{
			name:     "32 ETH (validator balance)",
			eth:      NewETH64(32),
			expected: big.NewInt(32e9),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gwei := tt.eth.Gwei()
			require.Equal(t, 0, tt.expected.Cmp(gwei), "expected %s, got %s", tt.expected.String(), gwei.String())
		})
	}
}

func TestETH_EdgeCases(t *testing.T) {
	t.Run("negative values", func(t *testing.T) {
		eth := NewETH64(-100)
		require.Equal(t, "-100.000000000000000000", eth.String())

		wei := eth.Wei()
		require.True(t, wei.Sign() < 0, "Wei should be negative")
	})

	t.Run("very large values", func(t *testing.T) {
		// Test with a very large value
		largeWei := new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil) // 10^30 Wei
		eth := NewETH(nil).SetWei(largeWei)
		recoveredWei := eth.Wei()

		require.Equal(t, 0, largeWei.Cmp(recoveredWei), "Large value roundtrip failed")
	})
}

func TestETH_ETH(t *testing.T) {
	tests := []struct {
		name     string
		eth      *ETH
		expected int64
	}{
		{
			name:     "zero",
			eth:      NewETH64(0),
			expected: 0,
		},
		{
			name:     "one ETH",
			eth:      NewETH64(1),
			expected: 1,
		},
		{
			name:     "32 ETH (validator balance)",
			eth:      NewETH64(32),
			expected: 32,
		},
		{
			name:     "32.5 ETH (truncates decimals)",
			eth:      NewETH64(32.5),
			expected: 32,
		},
		{
			name:     "32.9 ETH (truncates decimals)",
			eth:      NewETH64(32.9),
			expected: 32,
		},
		{
			name:     "100 ETH",
			eth:      NewETH64(100),
			expected: 100,
		},
		{
			name:     "from Gwei - 32 ETH",
			eth:      NewETH(nil).SetGwei(big.NewInt(32_000_000_000)),
			expected: 32,
		},
		{
			name:     "negative ETH",
			eth:      NewETH64(-10),
			expected: -10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.eth.ETH()
			require.Equal(t, tt.expected, result, "expected %d ETH, got %d", tt.expected, result)
		})
	}
}
