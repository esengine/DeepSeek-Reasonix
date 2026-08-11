package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/config"
)

const (
	windowRestoreStateVersion = 1
	windowRestoreTimeout      = 12 * time.Second
	windowRestorePollInterval = 100 * time.Millisecond
)

type windowRestoreState struct {
	SchemaVersion   int    `json:"schemaVersion"`
	PID             int    `json:"pid"`
	AttemptID       uint64 `json:"attemptId,omitempty"`
	Source          string `json:"source"`
	StartedAt       string `json:"startedAt"`
	TimeoutReported bool   `json:"timeoutReported,omitempty"`
}

var (
	windowRestoreMu       sync.Mutex
	windowRestoreSequence atomic.Uint64
)

func windowRestoreStatePath() string {
	return filepath.Join(config.MemoryUserDir(), "repair", "window-restore-state.json")
}

func writeWindowRestoreState(state windowRestoreState) bool {
	body, err := json.Marshal(state)
	if err != nil {
		return false
	}
	path := windowRestoreStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false
	}
	// This journal is best-effort diagnostics, not user data. Avoid the
	// fsync-and-retry path here because it runs immediately before WindowShow
	// and Windows antivirus locks can otherwise delay restoration noticeably.
	return os.WriteFile(path, body, 0o600) == nil
}

func readWindowRestoreState() (windowRestoreState, error) {
	body, err := os.ReadFile(windowRestoreStatePath())
	if err != nil {
		return windowRestoreState{}, err
	}
	var state windowRestoreState
	if err := json.Unmarshal(body, &state); err != nil {
		return windowRestoreState{}, err
	}
	return state, nil
}

func (a *App) observeIncompleteWindowRestore() {
	if !windowRestoreDiagnosticsSupported() {
		return
	}
	windowRestoreMu.Lock()
	state, err := readWindowRestoreState()
	if err != nil {
		windowRestoreMu.Unlock()
		return
	}
	if state.PID > 0 && windowRestoreOwnerAlive(state.PID) {
		windowRestoreMu.Unlock()
		return
	}
	_ = os.Remove(windowRestoreStatePath())
	windowRestoreMu.Unlock()
	if state.TimeoutReported {
		return
	}
	_ = writePendingReport(windowRestoreFailureReport("incomplete", state.Source, state.StartedAt), true)
	a.recordDiagnosticMetric("desktop_restore", "incomplete")
}

func (a *App) showMainWindowFrom(source string) {
	if a.ctx == nil {
		return
	}
	if !windowRestoreDiagnosticsSupported() {
		showFromBackground(a.ctx, a.backgroundMaximised.Swap(false))
		a.kickDeferredRebuildRetry()
		return
	}

	windowRestoreMu.Lock()
	attemptID := windowRestoreSequence.Add(1)
	state := windowRestoreState{
		SchemaVersion: windowRestoreStateVersion,
		PID:           os.Getpid(),
		AttemptID:     attemptID,
		Source:        metricBucket(source),
		StartedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	_ = writeWindowRestoreState(state)
	windowRestoreMu.Unlock()

	showFromBackground(a.ctx, a.backgroundMaximised.Swap(false))
	a.goSafe("windowRestoreMonitor", func() {
		restored := waitForWindowRestoreConfirmation()
		a.completeWindowRestoreAttempt(attemptID, state, restored)
	})
	a.kickDeferredRebuildRetry()
}

func waitForWindowRestoreConfirmation() bool {
	ticker := time.NewTicker(windowRestorePollInterval)
	defer ticker.Stop()
	timer := time.NewTimer(windowRestoreTimeout)
	defer timer.Stop()
	return awaitWindowRestoreConfirmation(windowRestoreConfirmed, ticker.C, timer.C)
}

func awaitWindowRestoreConfirmation(confirmed func() bool, ticks, deadline <-chan time.Time) bool {
	if confirmed() {
		return true
	}
	for {
		select {
		case <-ticks:
			if confirmed() {
				return true
			}
		case <-deadline:
			return confirmed()
		}
	}
}

func (a *App) completeWindowRestoreAttempt(attemptID uint64, state windowRestoreState, restored bool) {
	metric := "success"
	windowRestoreMu.Lock()
	if windowRestoreSequence.Load() != attemptID {
		windowRestoreMu.Unlock()
		return
	}
	if restored {
		_ = os.Remove(windowRestoreStatePath())
	} else {
		metric = "timeout"
		if writePendingReport(windowRestoreFailureReport("timeout", state.Source, state.StartedAt), true) {
			state.TimeoutReported = true
			_ = writeWindowRestoreState(state)
		}
	}
	windowRestoreMu.Unlock()
	a.recordDiagnosticMetric("desktop_restore", metric)
}

func windowRestoreFailureReport(kind, source, startedAt string) crashReport {
	kind = metricBucket(kind)
	source = metricBucket(source)
	report := baseCrashReport("performance")
	report.SchemaVersion = 2
	report.Source = "native.window"
	report.Label = "windows.window_restore." + kind
	report.ErrorType = "WindowsWindowRestoreFailure"
	report.ErrorMessage = sanitizeCrashText("Windows window restoration did not complete normally.", maxCrashFieldBytes)
	report.TopFrame = "windows.window_restore." + source
	report.FingerprintHint = "windows.window_restore." + kind + "." + source
	report.OccurredAt = time.Now().UTC().Format(time.RFC3339)
	report.Message = sanitizeCrashText(fmt.Sprintf(`[windows.window_restore.%s]

Reasonix could not confirm that the hidden window was restored.

source: %s
attempt started at: %s
timeout: %s`, kind, source, sanitizeCrashField(startedAt, 64), windowRestoreTimeout), maxCrashDetailBytes)
	return report
}
