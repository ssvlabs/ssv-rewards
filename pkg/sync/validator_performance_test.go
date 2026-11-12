package sync

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv-rewards/pkg/beacon"
)

var testSpec = beacon.Spec{Network: "mainnet"}

// failServer fails the test if the SSV API is called at all.
func failServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected SSV API request: %s", r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchDecideds_UsesFileCacheWhenSettled(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC)
	want := map[string]int{"aa": 225, "bb": 0}
	require.NoError(t, saveSSVCache(dir, "2026-03-26", ssvCacheItem{
		Time:       day.Add(49 * time.Hour),
		Validators: want,
	}))

	got, err := fetchDecideds(context.Background(), zap.NewNop(), testSpec, failServer(t).URL, dir, day, 1, 2)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestFetchDecideds_RefetchesWhenCacheNotSettled(t *testing.T) {
	for name, fetchedAt := range map[string]time.Duration{
		"exactly 48h": 48 * time.Hour,
		"within 48h":  47 * time.Hour,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			day := time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC)
			require.NoError(t, saveSSVCache(dir, "2026-03-26", ssvCacheItem{
				Time:       day.Add(fetchedAt),
				Validators: map[string]int{"aa": 1},
			}))

			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, "/api/v4/mainnet/validators/duty_counts/436725/436949", r.URL.Path)
				fmt.Fprint(w, `{"validators":{"aa":{"duties":225},"cc":{"duties":7}}}`)
			}))
			defer srv.Close()

			got, err := fetchDecideds(context.Background(), zap.NewNop(), testSpec, srv.URL+"/api/v4", dir, day, 436725, 436949)
			require.NoError(t, err)
			require.Equal(t, map[string]int{"aa": 225, "cc": 7}, got)
			require.EqualValues(t, 1, calls.Load())

			// Fresh response is written through to the file cache.
			cached, err := loadSSVCache(dir, "2026-03-26")
			require.NoError(t, err)
			require.Equal(t, got, cached.Validators)
			require.True(t, cached.Time.After(day.Add(48*time.Hour)))
		})
	}
}

func TestFetchDecideds_FetchesWhenNoCache(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Endpoint with a trailing slash must not produce a double slash.
		require.Equal(t, "/api/v4/mainnet/validators/duty_counts/10/20", r.URL.Path)
		fmt.Fprint(w, `{"validators":{"aa":{"duties":3}}}`)
	}))
	defer srv.Close()

	got, err := fetchDecideds(context.Background(), zap.NewNop(), testSpec, srv.URL+"/api/v4/", dir, day, 10, 20)
	require.NoError(t, err)
	require.Equal(t, map[string]int{"aa": 3}, got)
}

func TestFetchDecideds_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"error":"boom"}`)
	}))
	defer srv.Close()

	_, err := fetchDecideds(context.Background(), zap.NewNop(), testSpec, srv.URL, t.TempDir(),
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), 10, 20)
	require.ErrorContains(t, err, "ssv api returned error: boom")
}

// TestFetchDecideds_DoesNotRetainAcrossDays replays the day-by-day sync pattern
// of a fresh resync served from the on-disk cache (--fresh --keep-cache) and
// asserts that heap stays flat. Previously every day's decideds map was kept in
// an in-memory cache for the whole run (~150 B per validator per day, ~8 GiB by
// 2026-03 on mainnet), which OOM-killed full resyncs.
func TestFetchDecideds_DoesNotRetainAcrossDays(t *testing.T) {
	const (
		numValidators = 20_000
		numDays       = 40
	)
	dir := t.TempDir()
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	validators := make(map[string]int, numValidators)
	for i := 0; i < numValidators; i++ {
		validators[fmt.Sprintf("%096x", i)] = 225
	}
	for d := 0; d < numDays; d++ {
		day := start.AddDate(0, 0, d)
		require.NoError(t, saveSSVCache(dir, day.Format("2006-01-02"), ssvCacheItem{
			Time:       time.Now(),
			Validators: validators,
		}))
	}
	validators = nil

	heap := func() uint64 {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}

	srvURL := failServer(t).URL
	var oneDay, total uint64
	base := heap()
	for d := 0; d < numDays; d++ {
		got, err := fetchDecideds(context.Background(), zap.NewNop(), testSpec, srvURL, dir, start.AddDate(0, 0, d), 1, 2)
		require.NoError(t, err)
		require.Len(t, got, numValidators)
		if d == 0 {
			oneDay = heap() - base
		}
		runtime.KeepAlive(got)
	}
	if h := heap(); h > base {
		total = h - base
	}
	t.Logf("one day's decideds: %d KiB, heap growth after %d days: %d KiB", oneDay>>10, numDays, total>>10)
	// Retaining every day would grow by ~numDays * oneDay; allow at most a
	// few days' worth of slack for allocator noise.
	require.Less(t, total, 4*oneDay, "decideds are being retained across days")
}
