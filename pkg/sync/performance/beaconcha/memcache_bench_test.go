package beaconcha

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv-rewards/pkg/beacon"
)

// Replays the sync access pattern (day-outer, validator-inner) against
// synthetic on-disk cache files shaped like real Beaconcha.in history
// (~1,120 days per validator since IMP start 2023-07-01), and reports
// peak in-memory cache heap. No network access: every request is served
// from the file cache (fetched-time = now, so the 48h freshness guard
// always passes for historical days).
//
// Each iteration uses a fresh client (cold cache), so results are valid at any
// -benchtime. Run with:
//
//	go test -bench MemCache -benchtime 1x -run xxx ./pkg/sync/performance/beaconcha/
func BenchmarkMemCacheSyncPattern(b *testing.B) {
	const (
		numValidators = 2000
		historyDays   = 1120 // ~2023-07-01 .. now
		syncDays      = 31   // one monthly round
	)

	histStart := time.Date(2023, 7, 1, 0, 0, 0, 0, time.UTC)
	syncFrom := histStart.AddDate(0, 0, historyDays-syncDays-2)

	dir := b.TempDir()
	seed, err := New("http://127.0.0.1:9", "", 1e9, dir) // black-holed endpoint; any fetch would fail loudly
	if err != nil {
		b.Fatal(err)
	}

	// Write synthetic cache files.
	for i := 0; i < numValidators; i++ {
		data := make([]dailyData, historyDays)
		for d := 0; d < historyDays; d++ {
			day := histStart.AddDate(0, 0, d)
			data[d] = dailyData{
				Day:                 941 + d,
				DayStart:            day,
				DayEnd:              day.AddDate(0, 0, 1),
				EndEffectiveBalance: 32_000_000_000,
				EndBalance:          32_100_000_000,
				MissedAttestations:  d % 3,
				ProposedBlocks:      d % 7 / 6,
				ParticipatedSync:    d % 11 / 10,
			}
		}
		if err := seed.saveCache(phase0.ValidatorIndex(i), cacheItem{Time: time.Now(), Data: data}); err != nil {
			b.Fatal(err)
		}
	}

	spec := beacon.Spec{
		Network:        "mainnet",
		GenesisTime:    time.Date(2020, 12, 1, 12, 0, 23, 0, time.UTC),
		SlotsPerEpoch:  32,
		SlotDuration:   12 * time.Second,
		FarFutureEpoch: 0xFFFFFFFFFFFFFFFF,
	}
	logger := zap.NewNop()
	ctx := context.Background()

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		// Fresh client per iteration so every iteration measures a cold cache;
		// reusing one client would report warm memcache hits from iteration 2 on.
		b.StopTimer()
		client, err := New("http://127.0.0.1:9", "", 1e9, dir)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		var peak uint64
		hits := 0
		for d := 0; d < syncDays; d++ {
			day := syncFrom.AddDate(0, 0, d)
			fromEpoch := spec.EpochAt(spec.SlotAt(day))
			toEpoch := spec.EpochAt(spec.SlotAt(day.AddDate(0, 0, 1))) - 1
			for i := 0; i < numValidators; i++ {
				p, err := client.ValidatorPerformance(
					ctx, logger, spec, day, fromEpoch, toEpoch,
					0, spec.FarFutureEpoch, phase0.ValidatorIndex(i),
				)
				if err != nil {
					b.Fatal(err)
				}
				if p != nil {
					hits++
				}
			}
			if d == 0 || d == syncDays-1 { // measure after first and last day
				runtime.GC()
				var ms runtime.MemStats
				runtime.ReadMemStats(&ms)
				if ms.HeapAlloc-min(before.HeapAlloc, ms.HeapAlloc) > peak {
					peak = ms.HeapAlloc - min(before.HeapAlloc, ms.HeapAlloc)
				}
			}
		}
		if hits != numValidators*syncDays {
			b.Fatalf("expected %d hits, got %d", numValidators*syncDays, hits)
		}

		perValidator := float64(peak) / numValidators
		b.ReportMetric(float64(peak)/(1<<20), "peak-MiB")
		b.ReportMetric(perValidator, "B/validator")
		b.ReportMetric(perValidator*250_000/(1<<30), "extrap-GiB@250k")
		b.Logf("peak cache heap: %.1f MiB for %d validators => %.0f B/validator => extrapolated %.1f GiB at 250k validators",
			float64(peak)/(1<<20), numValidators, perValidator, perValidator*250_000/(1<<30))
	}
}
