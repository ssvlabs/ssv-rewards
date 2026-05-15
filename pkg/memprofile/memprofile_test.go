package memprofile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestSanitizeLabel(t *testing.T) {
	t.Parallel()
	got := sanitizeLabel("performance-day-0042/2024-01-01")
	if got != "performance-day-0042_2024-01-01" {
		t.Fatalf("sanitizeLabel() = %q", got)
	}
}

func TestPerformanceDayInterval(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMPROFILE_DIR", dir)
	t.Setenv("MEMPROFILE_INTERVAL_DAYS", "3")

	s := FromEnv(zap.NewNop())
	ctx := WithContext(context.Background(), s)

	for i := 0; i < 10; i++ {
		PerformanceDay(ctx, mustParseDay(t, "2024-01-01").AddDate(0, 0, i), i)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var heaps []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".heap.pb.gz") {
			heaps = append(heaps, e.Name())
		}
	}
	// dayIndex 0, 2, 5, 8 => 4 snapshots
	if len(heaps) != 4 {
		t.Fatalf("expected 4 heap files, got %d: %v", len(heaps), heaps)
	}
	log, err := os.ReadFile(filepath.Join(dir, "memprofile.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "performance-day-0000") {
		t.Fatalf("log missing first day:\n%s", log)
	}
}

func TestDisabledSampler(t *testing.T) {
	t.Parallel()
	s := &Sampler{}
	if s.Enabled() {
		t.Fatal("zero sampler should be disabled")
	}
	s.Snapshot("noop") // must not panic
	PerformanceDay(context.Background(), mustParseDay(t, "2024-01-01"), 0)
}

func mustParseDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
