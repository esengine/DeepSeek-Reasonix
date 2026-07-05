package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
)

func TestHangAgeBucket(t *testing.T) {
	cases := []struct {
		age  time.Duration
		want string
	}{
		{12 * time.Second, "s_10_15"},
		{16 * time.Second, "s_15_30"},
		{45 * time.Second, "s_30_60"},
		{2 * time.Minute, "m_1_5"},
		{10 * time.Minute, "m_5_plus"},
	}
	for _, c := range cases {
		if got := hangAgeBucket(c.age); got != c.want {
			t.Errorf("hangAgeBucket(%s) = %q, want %q", c.age, got, c.want)
		}
	}
}

func TestMainThreadHangReportIsStructuredPerformanceReport(t *testing.T) {
	last := time.Date(2026, 7, 4, 9, 19, 11, 0, time.UTC)
	observed := last.Add(16 * time.Second)

	r := mainThreadHangReport(16*time.Second, last, observed)

	if r.Kind != "performance" || r.Source != "native.watchdog" || r.Label != "mac.main_thread.hang" {
		t.Fatalf("unexpected report identity: %+v", r)
	}
	if r.SchemaVersion != 2 || r.ErrorType != "MacMainThreadHang" || r.TopFrame == "" || r.OccurredAt == "" {
		t.Fatalf("structured fields missing: %+v", r)
	}
	for _, want := range []string{"Reasonix detected", "last heartbeat:", "bucket: s_15_30", "goroutines:"} {
		if !strings.Contains(r.Message, want) {
			t.Fatalf("hang report message missing %q:\n%s", want, r.Message)
		}
	}
}

func TestMainThreadHeartbeatAgeIgnoresWallClockJump(t *testing.T) {
	oldBase := mainThreadClockBase
	oldElapsed := mainThreadLastHeartbeatElapsed.Load()
	oldWall := mainThreadLastHeartbeatWall.Load()
	t.Cleanup(func() {
		mainThreadClockBase = oldBase
		mainThreadLastHeartbeatElapsed.Store(oldElapsed)
		mainThreadLastHeartbeatWall.Store(oldWall)
	})

	base := time.Now()
	mainThreadClockBase = base
	mainThreadLastHeartbeatElapsed.Store(int64(time.Second))
	mainThreadLastHeartbeatWall.Store(base.Add(-time.Hour).UnixNano())

	age, _, ok := mainThreadHeartbeatAge(base.Add(2 * time.Second))
	if !ok {
		t.Fatal("expected heartbeat age")
	}
	if age != time.Second {
		t.Fatalf("age = %s, want monotonic elapsed 1s despite wall-clock jump", age)
	}
}

func TestRecordMainThreadHangWritesPendingReportAndMetrics(t *testing.T) {
	t.Cleanup(func() {
		os.Remove(pendingCrashPath())
		os.Remove(filepath.Join(config.MemoryUserDir(), metricsPendingFile))
	})
	app := NewApp()
	app.metrics.Store(newMetricsAggregator(config.MemoryUserDir()))

	last := time.Now().Add(-20 * time.Second)
	app.recordMainThreadHang(20*time.Second, last, time.Now())

	r, ok := readPending(t)
	if !ok {
		t.Fatal("expected pending hang report")
	}
	if r.Kind != "performance" || r.Source != "native.watchdog" || r.Label != "mac.main_thread.hang" {
		t.Fatalf("pending report = %+v", r)
	}
	c := readCounters(filepath.Join(config.MemoryUserDir(), metricsPendingFile))
	if got := c["desktop_hang"]["main_thread"]; got != 1 {
		t.Fatalf("desktop_hang/main_thread = %d, want 1", got)
	}
	if got := c["desktop_hang_age"]["s_15_30"]; got != 1 {
		t.Fatalf("desktop_hang_age/s_15_30 = %d, want 1", got)
	}
}
