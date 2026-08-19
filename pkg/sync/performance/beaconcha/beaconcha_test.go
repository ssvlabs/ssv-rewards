package beaconcha

import (
	"math"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-rewards/pkg/beacon"
	"github.com/stretchr/testify/require"
)

func TestRetainWindow(t *testing.T) {
	dayKey := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	at := func(offsetDays int) time.Time { return dayKey.AddDate(0, 0, offsetDays) }
	daily := func(offsetDays, missed int) dailyData {
		return dailyData{DayStart: at(offsetDays), MissedAttestations: missed}
	}
	fullWindow := dayKey.AddDate(0, 0, memCacheRetainDays)

	for _, tc := range []struct {
		name      string
		data      []dailyData
		windowEnd time.Time
		wantDays  []time.Time
		wantFound bool
		wantDay   dailyData
	}{
		{
			name:      "dayKey itself is retained and returned",
			data:      []dailyData{daily(0, 1)},
			windowEnd: fullWindow,
			wantDays:  []time.Time{at(0)},
			wantFound: true,
			wantDay:   daily(0, 1),
		},
		{
			name:      "last day inside the window (+44) is retained",
			data:      []dailyData{daily(0, 1), daily(memCacheRetainDays-1, 2)},
			windowEnd: fullWindow,
			wantDays:  []time.Time{at(0), at(memCacheRetainDays - 1)},
			wantFound: true,
			wantDay:   daily(0, 1),
		},
		{
			name:      "first day past the window (+45) is dropped",
			data:      []dailyData{daily(0, 1), daily(memCacheRetainDays, 2)},
			windowEnd: fullWindow,
			wantDays:  []time.Time{at(0)},
			wantFound: true,
			wantDay:   daily(0, 1),
		},
		{
			name:      "day before dayKey (-1) is dropped",
			data:      []dailyData{daily(-1, 9), daily(0, 1)},
			windowEnd: fullWindow,
			wantDays:  []time.Time{at(0)},
			wantFound: true,
			wantDay:   daily(0, 1),
		},
		{
			name:      "duplicate day: last entry wins",
			data:      []dailyData{daily(0, 1), daily(0, 7)},
			windowEnd: fullWindow,
			wantDays:  []time.Time{at(0)},
			wantFound: true,
			wantDay:   daily(0, 7),
		},
		{
			name:      "dayKey absent: found=false, later days still retained",
			data:      []dailyData{daily(1, 2), daily(2, 3)},
			windowEnd: fullWindow,
			wantDays:  []time.Time{at(1), at(2)},
			wantFound: false,
		},
		{
			name:      "clamped windowEnd drops unsettled tail days",
			data:      []dailyData{daily(0, 1), daily(1, 2), daily(2, 3)},
			windowEnd: at(2), // e.g. cachedItem.Time - 48h
			wantDays:  []time.Time{at(0), at(1)},
			wantFound: true,
			wantDay:   daily(0, 1),
		},
		{
			name:      "empty data: nothing retained, not found",
			data:      nil,
			windowEnd: fullWindow,
			wantDays:  nil,
			wantFound: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			byDay, day, found := retainWindow(tc.data, dayKey, tc.windowEnd)

			require.Equal(t, tc.wantFound, found)
			if tc.wantFound {
				require.Equal(t, tc.wantDay, day)
			}
			require.Len(t, byDay, len(tc.wantDays))
			for _, k := range tc.wantDays {
				require.Contains(t, byDay, k)
			}
		})
	}
}

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
