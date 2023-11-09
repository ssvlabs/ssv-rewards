package beacon

import (
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestSpec_FirstSlot(t *testing.T) {
	spec := Spec{SlotsPerEpoch: 5}
	require.Equal(t, phase0.Slot(10), spec.FirstSlot(2))
}

func TestSpec_LastSlot(t *testing.T) {
	spec := Spec{SlotsPerEpoch: 5}
	require.Equal(t, phase0.Slot(14), spec.LastSlot(2))
}

func TestSpec_EpochAt(t *testing.T) {
	spec := Spec{SlotsPerEpoch: 5}
	require.Equal(t, phase0.Epoch(2), spec.EpochAt(10))
}

func TestSpec_SlotAt(t *testing.T) {
	spec := Spec{GenesisTime: time.Unix(0, 0), SlotDuration: time.Second}
	require.Equal(t, phase0.Slot(5), spec.SlotAt(time.Unix(5, 0)))
}

func TestSpec_TimeAt(t *testing.T) {
	spec := Spec{GenesisTime: time.Unix(0, 0), SlotDuration: time.Second}
	require.Equal(t, time.Unix(5, 0), spec.TimeAt(5))
}
