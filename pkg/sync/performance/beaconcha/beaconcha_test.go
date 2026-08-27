package beaconcha

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-rewards/pkg/beacon"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRetainWindow(t *testing.T) {
	dayKey := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	at := func(offsetDays int) time.Time { return dayKey.AddDate(0, 0, offsetDays) }
	// Real Beaconcha day starts are not midnight-aligned (e.g. 12:00:23 UTC), so
	// fixtures carry an intra-day offset to exercise the key truncation.
	daily := func(offsetDays, missed int) dailyData {
		return dailyData{DayStart: at(offsetDays).Add(12*time.Hour + 23*time.Second), MissedAttestations: missed}
	}
	fullWindow := dayKey.AddDate(0, 0, memCacheRetainDays)

	for _, tc := range []struct {
		name      string
		data      []dailyData
		windowEnd time.Time
		wantByDay map[time.Time]dailyData
		wantFound bool
		wantDay   dailyData
	}{
		{
			name:      "dayKey itself is retained and returned",
			data:      []dailyData{daily(0, 1)},
			windowEnd: fullWindow,
			wantByDay: map[time.Time]dailyData{at(0): daily(0, 1)},
			wantFound: true,
			wantDay:   daily(0, 1),
		},
		{
			name:      "last day inside the window (+44) is retained",
			data:      []dailyData{daily(0, 1), daily(memCacheRetainDays-1, 2)},
			windowEnd: fullWindow,
			wantByDay: map[time.Time]dailyData{
				at(0):                      daily(0, 1),
				at(memCacheRetainDays - 1): daily(memCacheRetainDays-1, 2),
			},
			wantFound: true,
			wantDay:   daily(0, 1),
		},
		{
			name:      "first day past the window (+45) is dropped",
			data:      []dailyData{daily(0, 1), daily(memCacheRetainDays, 2)},
			windowEnd: fullWindow,
			wantByDay: map[time.Time]dailyData{at(0): daily(0, 1)},
			wantFound: true,
			wantDay:   daily(0, 1),
		},
		{
			name:      "day before dayKey (-1) is dropped",
			data:      []dailyData{daily(-1, 9), daily(0, 1)},
			windowEnd: fullWindow,
			wantByDay: map[time.Time]dailyData{at(0): daily(0, 1)},
			wantFound: true,
			wantDay:   daily(0, 1),
		},
		{
			name:      "duplicate day: last entry wins",
			data:      []dailyData{daily(0, 1), daily(0, 7)},
			windowEnd: fullWindow,
			wantByDay: map[time.Time]dailyData{at(0): daily(0, 7)},
			wantFound: true,
			wantDay:   daily(0, 7),
		},
		{
			name:      "dayKey absent: found=false, later days still retained",
			data:      []dailyData{daily(1, 2), daily(2, 3)},
			windowEnd: fullWindow,
			wantByDay: map[time.Time]dailyData{at(1): daily(1, 2), at(2): daily(2, 3)},
			wantFound: false,
		},
		{
			name:      "clamped windowEnd drops unsettled tail days",
			data:      []dailyData{daily(0, 1), daily(1, 2), daily(2, 3)},
			windowEnd: at(2), // e.g. cachedItem.Time - 48h
			wantByDay: map[time.Time]dailyData{at(0): daily(0, 1), at(1): daily(1, 2)},
			wantFound: true,
			wantDay:   daily(0, 1),
		},
		{
			name:      "empty data: nothing retained, not found",
			data:      nil,
			windowEnd: fullWindow,
			wantByDay: map[time.Time]dailyData{},
			wantFound: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			byDay, day, found := retainWindow(tc.data, dayKey, tc.windowEnd)

			require.Equal(t, tc.wantFound, found)
			if tc.wantFound {
				require.Equal(t, tc.wantDay, day)
			}
			require.Equal(t, tc.wantByDay, byDay)
		})
	}
}

// TestValidatorPerformanceClampsUnsettledDays exercises the settled-time clamp
// through the file-cache path of ValidatorPerformance: days within 48h of the
// cache file's fetch time must not be loaded into the memory cache, so a later
// sync day falls through to a fresh fetch instead of being served stale data.
func TestValidatorPerformanceClampsUnsettledDays(t *testing.T) {
	dayKey := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	dayStart := func(offsetDays int) time.Time {
		return dayKey.AddDate(0, 0, offsetDays).Add(12*time.Hour + 23*time.Second)
	}

	// Black-holed endpoint: any fetch attempt fails fast (404 is not retried).
	var fetches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		http.Error(w, "black hole", http.StatusNotFound)
	}))
	defer srv.Close()

	client, err := New(srv.URL, "", 60_000, t.TempDir())
	require.NoError(t, err)

	const index = phase0.ValidatorIndex(42)
	// Cache file fetched at dayKey+49h: dayKey is settled at fetch time, but
	// dayKey+1d is within 48h of it and may still change upstream.
	require.NoError(t, client.saveCache(index, cacheItem{
		Time: dayKey.Add(49 * time.Hour),
		Data: []dailyData{
			{DayStart: dayStart(0), MissedAttestations: 1},
			{DayStart: dayStart(1), MissedAttestations: 2},
		},
	}))

	spec := beacon.Spec{FarFutureEpoch: math.MaxUint64}
	logger := zap.NewNop()

	// dayKey is settled: served from the cache file, no fetch.
	perf, err := client.ValidatorPerformance(context.Background(), logger, spec, dayKey,
		0, 0, 0, phase0.Epoch(math.MaxUint64), index)
	require.NoError(t, err)
	require.NotNil(t, perf)
	require.Equal(t, int32(0), fetches.Load())

	// dayKey+1d was unsettled at the file's fetch time: it must not be served
	// from the memory cache loaded by the previous call. The 48h freshness
	// guard then rejects the file too, forcing a fetch against the black-holed
	// endpoint, which errors.
	_, err = client.ValidatorPerformance(context.Background(), logger, spec, dayKey.AddDate(0, 0, 1),
		0, 0, 0, phase0.Epoch(math.MaxUint64), index)
	require.Error(t, err)
	require.Positive(t, fetches.Load())
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
