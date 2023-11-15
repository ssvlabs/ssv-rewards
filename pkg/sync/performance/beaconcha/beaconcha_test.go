package beaconcha

import (
	"math"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-rewards/pkg/beacon"
	"github.com/stretchr/testify/require"
)

func TestDeriveActiveEpochs(t *testing.T) {
	spec := beacon.Spec{
		FarFutureEpoch: math.MaxUint64,
	}

	// Activated at exactly the start of the period.
	require.Equal(t, phase0.Epoch(225), deriveActiveEpochs(
		spec,
		phase0.Epoch(400), phase0.Epoch(624), // from/to
		phase0.Epoch(400), phase0.Epoch(math.MaxUint64), // activation/exit
	))

	// Activated at exactly the end of the period.
	require.Equal(t, phase0.Epoch(1), deriveActiveEpochs(
		spec,
		phase0.Epoch(400), phase0.Epoch(624), // from/to
		phase0.Epoch(624), phase0.Epoch(math.MaxUint64), // activation/exit
	))

	// Activated before the period.
	require.Equal(t, phase0.Epoch(225), deriveActiveEpochs(
		spec,
		phase0.Epoch(400), phase0.Epoch(624), // from/to
		phase0.Epoch(320), phase0.Epoch(math.MaxUint64), // activation/exit
	))

	// Activated during the period.
	require.Equal(t, phase0.Epoch(200), deriveActiveEpochs(
		spec,
		phase0.Epoch(400), phase0.Epoch(624), // from/to
		phase0.Epoch(425), phase0.Epoch(math.MaxUint64), // activation/exit
	))

	// Activated after the period.
	require.Equal(t, phase0.Epoch(0), deriveActiveEpochs(
		spec,
		phase0.Epoch(400), phase0.Epoch(624), // from/to
		phase0.Epoch(700), phase0.Epoch(math.MaxUint64), // activation/exit
	))

	// Activated during the period, exited during the period.
	require.Equal(t, phase0.Epoch(175), deriveActiveEpochs(
		spec,
		phase0.Epoch(400), phase0.Epoch(624), // from/to
		phase0.Epoch(425), phase0.Epoch(600), // activation/exit
	))

	// Activated before the period, exited during the period.
	require.Equal(t, phase0.Epoch(200), deriveActiveEpochs(
		spec,
		phase0.Epoch(400), phase0.Epoch(624), // from/to
		phase0.Epoch(320), phase0.Epoch(600), // activation/exit
	))

	// Activated during the period, exited after the period.
	require.Equal(t, phase0.Epoch(200), deriveActiveEpochs(
		spec,
		phase0.Epoch(400), phase0.Epoch(624), // from/to
		phase0.Epoch(425), phase0.Epoch(700), // activation/exit
	))

	// Activated before the period, exited right after the period.
	require.Equal(t, phase0.Epoch(225), deriveActiveEpochs(
		spec,
		phase0.Epoch(400), phase0.Epoch(624), // from/to
		phase0.Epoch(320), phase0.Epoch(625), // activation/exit
	))

	// Activated before the period, exited long after the period.
	require.Equal(t, phase0.Epoch(225), deriveActiveEpochs(
		spec,
		phase0.Epoch(400), phase0.Epoch(624), // from/to
		phase0.Epoch(320), phase0.Epoch(700), // activation/exit
	))

	// Activated before the period, exited before the period.
	require.Equal(t, phase0.Epoch(0), deriveActiveEpochs(
		spec,
		phase0.Epoch(400), phase0.Epoch(624), // from/to
		phase0.Epoch(320), phase0.Epoch(350), // activation/exit
	))

	// Activated during the period, exited at exactly the end of the period.
	require.Equal(t, phase0.Epoch(199), deriveActiveEpochs(
		spec,
		phase0.Epoch(400), phase0.Epoch(624), // from/to
		phase0.Epoch(425), phase0.Epoch(624), // activation/exit
	))
}
