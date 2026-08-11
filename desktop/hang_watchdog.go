package main

import (
	"context"
	"fmt"
	"log/slog"
	goruntime "runtime"
	"sync/atomic"
	"time"
)

const (
	mainThreadHeartbeatInterval = time.Second
	mainThreadHangThreshold     = 12 * time.Second
	mainThreadHangCheckInterval = 2 * time.Second
	mainThreadSleepSkip         = 30 * time.Second
)

var (
	mainThreadClockBase            = time.Now()
	mainThreadLastHeartbeatElapsed atomic.Int64
	mainThreadLastHeartbeatWall    atomic.Int64
	mainThreadHangReported         atomic.Bool
)

func recordMainThreadHeartbeat(t time.Time) {
	elapsed := max(t.Sub(mainThreadClockBase), 0)
	mainThreadLastHeartbeatElapsed.Store(int64(elapsed))
	mainThreadLastHeartbeatWall.Store(t.UnixNano())
}

func mainThreadHeartbeatAge(now time.Time) (time.Duration, time.Time, bool) {
	lastElapsed := time.Duration(mainThreadLastHeartbeatElapsed.Load())
	lastWall := mainThreadLastHeartbeatWall.Load()
	if lastWall <= 0 {
		return 0, time.Time{}, false
	}
	age := max(now.Sub(mainThreadClockBase)-lastElapsed, 0)
	return age, time.Unix(0, lastWall), true
}

func resetMainThreadHeartbeatAfterSleep(lastCheck, now time.Time) bool {
	if now.Sub(lastCheck) <= mainThreadSleepSkip {
		return false
	}
	// The native UI heartbeat and this Go ticker are both suspended while the
	// machine sleeps. Treat wake as a fresh observation epoch so the sleep gap
	// cannot be reported as a multi-minute UI-thread hang on the next tick.
	recordMainThreadHeartbeat(now)
	return true
}

func (a *App) startMainThreadWatchdog() {
	if !mainThreadWatchdogSupported() {
		return
	}
	a.hangWatchdogMu.Lock()
	if a.hangWatchdogCancel != nil {
		a.hangWatchdogMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.hangWatchdogCancel = cancel
	mainThreadHangReported.Store(false)
	recordMainThreadHeartbeat(time.Now())
	startNativeMainThreadHeartbeat(uint64(mainThreadHeartbeatInterval / time.Millisecond))
	a.hangWatchdogMu.Unlock()

	a.goSafe("mainThreadHangWatchdog", func() {
		a.watchMainThreadHeartbeat(ctx)
	})
}

func (a *App) stopMainThreadWatchdog() {
	if !mainThreadWatchdogSupported() {
		return
	}
	a.hangWatchdogMu.Lock()
	cancel := a.hangWatchdogCancel
	a.hangWatchdogCancel = nil
	a.hangWatchdogMu.Unlock()
	if cancel != nil {
		cancel()
	}
	stopNativeMainThreadHeartbeat()
}

func (a *App) watchMainThreadHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(mainThreadHangCheckInterval)
	defer ticker.Stop()
	lastCheck := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if resetMainThreadHeartbeatAfterSleep(lastCheck, now) {
				lastCheck = now
				continue
			}
			lastCheck = now
			age, last, ok := mainThreadHeartbeatAge(now)
			if !ok {
				continue
			}
			if age < mainThreadHangThreshold {
				continue
			}
			if mainThreadHangReported.CompareAndSwap(false, true) {
				a.recordMainThreadHang(age, last, now)
			}
		}
	}
}

func (a *App) recordMainThreadHang(age time.Duration, lastHeartbeat, observedAt time.Time) {
	report := mainThreadHangReport(age, lastHeartbeat, observedAt)
	wrote := writePendingReport(report, true)
	if m := a.metrics.Load(); m != nil {
		m.inc("desktop_hang", mainThreadMetricBucket())
		m.inc("desktop_hang_age", hangAgeBucket(age))
		m.persist()
	}
	slog.Warn("desktop: native UI thread heartbeat stalled",
		"age", age.Round(time.Millisecond).String(),
		"lastHeartbeat", lastHeartbeat.Format(time.RFC3339),
		"pendingReport", wrote,
	)
}

func mainThreadHangReport(age time.Duration, lastHeartbeat, observedAt time.Time) crashReport {
	label, errorType, platformName, topFrame := mainThreadDiagnosticIdentity()
	age = age.Round(time.Second)
	message := fmt.Sprintf(`[%s]

Reasonix detected that the %s UI-thread heartbeat stopped for %s.

--- watchdog context ---
last heartbeat: %s
observed at: %s
threshold: %s
bucket: %s

--- native runtime context ---
%s`,
		label,
		platformName,
		age,
		lastHeartbeat.UTC().Format(time.RFC3339),
		observedAt.UTC().Format(time.RFC3339),
		mainThreadHangThreshold,
		hangAgeBucket(age),
		nativeResourceContext(),
	)
	report := baseCrashReport("performance")
	report.SchemaVersion = 2
	report.Source = "native.watchdog"
	report.Label = label
	report.ErrorType = errorType
	report.ErrorMessage = sanitizeCrashText(platformName+" UI thread heartbeat stopped; the native/Wails message loop may be blocked.", maxCrashFieldBytes)
	report.TopFrame = topFrame
	report.OccurredAt = observedAt.UTC().Format(time.RFC3339)
	report.Message = sanitizeCrashText(message, maxCrashDetailBytes)
	return report
}

func mainThreadDiagnosticIdentity() (label, errorType, platformName, topFrame string) {
	if goruntime.GOOS == "windows" {
		return "windows.ui_thread.hang", "WindowsUIThreadHang", "Windows", "windows.ui_thread.heartbeat"
	}
	return "mac.main_thread.hang", "MacMainThreadHang", "macOS", "mac.main_thread.heartbeat"
}

func mainThreadMetricBucket() string {
	if goruntime.GOOS == "windows" {
		return "windows_ui_thread"
	}
	return "main_thread"
}

func hangAgeBucket(age time.Duration) string {
	seconds := age.Seconds()
	switch {
	case seconds < 15:
		return "s_10_15"
	case seconds < 30:
		return "s_15_30"
	case seconds < 60:
		return "s_30_60"
	case seconds < 300:
		return "m_1_5"
	default:
		return "m_5_plus"
	}
}
