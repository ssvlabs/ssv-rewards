// Package memprofile writes heap profiles and memory stats to disk for long-running
// sync jobs. Enable with MEMPROFILE_DIR; no HTTP server or manual sampling required.
package memprofile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type contextKey struct{}

// Sampler writes heap snapshots and a TSV log under dir. A zero-value Sampler is disabled.
type Sampler struct {
	dir          string
	intervalDays int
	gcBefore     bool
	logger       *zap.Logger
	mu           sync.Mutex
	seq          int
}

// DefaultDir returns the recommended profile directory for a network. It lives outside
// DATA_DIR so --fresh / --keep-cache wipes do not remove heap snapshots.
func DefaultDir(network string) string {
	return filepath.Join(".", "memprofile", network)
}

// Start enables profiling when MEMPROFILE_DIR is set and returns ctx plus a phase
// callback for named snapshots (no-op when disabled).
func Start(logger *zap.Logger) (context.Context, func(string)) {
	s := FromEnv(logger)
	ctx := WithContext(context.Background(), s)
	if !s.Enabled() {
		return ctx, func(string) {}
	}
	s.Snapshot("sync-start")
	return ctx, s.Snapshot
}

// FromEnv returns a sampler when MEMPROFILE_DIR is set; otherwise a disabled sampler.
func FromEnv(logger *zap.Logger) *Sampler {
	dir := strings.TrimSpace(os.Getenv("MEMPROFILE_DIR"))
	if dir == "" {
		return &Sampler{}
	}
	intervalDays := 30
	if v := strings.TrimSpace(os.Getenv("MEMPROFILE_INTERVAL_DAYS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			logger.Warn("memprofile: invalid MEMPROFILE_INTERVAL_DAYS, using 30", zap.String("value", v))
		} else {
			intervalDays = n
		}
	}
	gcBefore := false
	if v := strings.TrimSpace(os.Getenv("MEMPROFILE_GC")); v != "" {
		gcBefore, _ = strconv.ParseBool(v)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Warn("memprofile: failed to create directory", zap.String("dir", dir), zap.Error(err))
		return &Sampler{}
	}
	logger.Info("memprofile enabled",
		zap.String("dir", dir),
		zap.Int("interval_days", intervalDays),
		zap.Bool("gc_before_snapshot", gcBefore),
	)
	return &Sampler{
		dir:          dir,
		intervalDays: intervalDays,
		gcBefore:     gcBefore,
		logger:       logger,
	}
}

// Enabled reports whether snapshots will be written.
func (s *Sampler) Enabled() bool {
	return s != nil && s.dir != ""
}

// WithContext attaches s to ctx for pkg/sync call sites.
func WithContext(ctx context.Context, s *Sampler) context.Context {
	if s == nil || !s.Enabled() {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, s)
}

// FromContext returns the sampler from ctx, or nil.
func FromContext(ctx context.Context) *Sampler {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(contextKey{}).(*Sampler)
	return s
}

// Snapshot writes a heap profile and appends a line to memprofile.tsv in dir.
func (s *Sampler) Snapshot(label string) {
	if !s.Enabled() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	stamp := time.Now().UTC()
	safe := sanitizeLabel(label)
	base := fmt.Sprintf("%03d_%s_%s", s.seq, stamp.Format("20060102T150405Z"), safe)

	if s.gcBefore {
		runtime.GC()
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	heapPath := filepath.Join(s.dir, base+".heap.pb.gz")
	if f, err := os.Create(heapPath); err != nil {
		s.logger.Warn("memprofile: create heap file", zap.String("path", heapPath), zap.Error(err))
	} else {
		err = pprof.WriteHeapProfile(f)
		closeErr := f.Close()
		if err != nil {
			s.logger.Warn("memprofile: write heap", zap.String("path", heapPath), zap.Error(err))
		} else if closeErr != nil {
			s.logger.Warn("memprofile: close heap", zap.String("path", heapPath), zap.Error(closeErr))
		} else {
			s.logger.Info("memprofile snapshot",
				zap.String("label", label),
				zap.String("heap", heapPath),
				zap.Uint64("heap_inuse_mb", ms.HeapInuse/1024/1024),
				zap.Uint64("heap_alloc_mb", ms.HeapAlloc/1024/1024),
				zap.Uint64("sys_mb", ms.Sys/1024/1024),
				zap.Uint32("goroutines", uint32(runtime.NumGoroutine())),
			)
		}
	}

	logLine := fmt.Sprintf("%s\t%s\t%s\theap_inuse_mb=%d\theap_alloc_mb=%d\tsys_mb=%d\tgoroutines=%d\n",
		stamp.Format(time.RFC3339),
		label,
		base,
		ms.HeapInuse/1024/1024,
		ms.HeapAlloc/1024/1024,
		ms.Sys/1024/1024,
		runtime.NumGoroutine(),
	)
	logPath := filepath.Join(s.dir, "memprofile.tsv")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		s.logger.Warn("memprofile: append log", zap.Error(err))
		return
	}
	_, _ = f.WriteString(logLine)
	_ = f.Close()
}

// PerformanceDay snapshots on the first day and every intervalDays thereafter (1-based index).
func PerformanceDay(ctx context.Context, day time.Time, dayIndex int) {
	s := FromContext(ctx)
	if s == nil || !s.Enabled() {
		return
	}
	if dayIndex == 0 || (dayIndex+1)%s.intervalDays == 0 {
		s.Snapshot(fmt.Sprintf("performance-day-%04d-%s", dayIndex, day.Format("2006-01-02")))
	}
}

func sanitizeLabel(label string) string {
	const maxLen = 80
	var b strings.Builder
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
		if b.Len() >= maxLen {
			break
		}
	}
	if b.Len() == 0 {
		return "snapshot"
	}
	return b.String()
}
