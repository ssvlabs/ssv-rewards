package gnosis

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestGnosisSafe(t *testing.T) {
	var tests = []struct {
		name      string
		addr      string
		threshold int
		version   string
		err       error
	}{
		{"valid", "0x19B3Eb3Af5D93b77a5619b047De0EED7115A19e7", 3, "1.3.0", nil},
		{"invalid", "0x39aa39c021dfbae8fac545936693ac917d5e7564", 0, "", ErrNotFound},
		{"invalid", "0x39aa39c021dfbae8fac545936693ac917d5e7563", 0, "", ErrNotFound},
	}

	client := New("https://safe-transaction-mainnet.safe.global", 0.1)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := common.HexToAddress(tt.addr)
			safe, err := client.Safe(context.Background(), addr)
			t.Logf("addr: %s, err: %v", addr, err)
			if tt.err != nil {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.threshold, safe.Threshold)
			require.Equal(t, tt.version, safe.Version)
		})
	}
}
