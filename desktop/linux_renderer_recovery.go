package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

const (
	linuxRendererCompatibilityEnv    = "REASONIX_WEBKIT_COMPATIBILITY_RESTART"
	linuxRendererRecoveryParentEnv   = "REASONIX_WEBKIT_RECOVERY_PARENT_PID"
	linuxDisableCompositingEnv       = "WEBKIT_DISABLE_COMPOSITING_MODE"
	linuxRendererRecoveryWindow      = 5 * time.Minute
	linuxRendererHeartbeatTimeout    = 10 * time.Second
	linuxRendererRecoveryStateSchema = 1
)

type linuxRendererRecoveryJournal struct {
	SchemaVersion int    `json:"schemaVersion"`
	Version       string `json:"version"`
	AttemptedAt   string `json:"attemptedAt"`
	Source        string `json:"source"`
	Outcome       string `json:"outcome,omitempty"`
}

var linuxRendererRecoveryJournalMu sync.Mutex

type linuxWebKitRecoveryCoordinator struct {
	app *App

	mu                 sync.Mutex
	pendingTimer       *time.Timer
	pendingEvent       *webKitNativeEvent
	pendingTelemetry   bool
	restartRequested   bool
	failureDialogShown bool
	healthyRecorded    bool
	stopped            bool
}

func newLinuxWebKitRecoveryCoordinator(app *App) *linuxWebKitRecoveryCoordinator {
	return &linuxWebKitRecoveryCoordinator{app: app}
}

func linuxRendererCompatibilityMode() bool {
	return goruntime.GOOS == "linux" && os.Getenv(linuxRendererCompatibilityEnv) == "1"
}

func prepareLinuxRendererCompatibilityEnvironment() {
	waitForLinuxRendererRecoveryParent()
	if linuxRendererCompatibilityMode() {
		_ = os.Setenv(linuxDisableCompositingEnv, "1")
	}
}

func waitForLinuxRendererRecoveryParent() {
	if goruntime.GOOS != "linux" {
		return
	}
	raw := os.Getenv(linuxRendererRecoveryParentEnv)
	_ = os.Unsetenv(linuxRendererRecoveryParentEnv)
	pid, err := strconv.Atoi(raw)
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for desktopProcessAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
}

func linuxRendererRecoveryJournalPath() string {
	return filepath.Join(config.MemoryUserDir(), "repair", "linux-renderer-recovery.json")
}

func readLinuxRendererRecoveryJournal() (linuxRendererRecoveryJournal, error) {
	body, err := os.ReadFile(linuxRendererRecoveryJournalPath())
	if err != nil {
		return linuxRendererRecoveryJournal{}, err
	}
	var state linuxRendererRecoveryJournal
	if err := json.Unmarshal(body, &state); err != nil {
		return linuxRendererRecoveryJournal{}, err
	}
	return state, nil
}

func writeLinuxRendererRecoveryJournal(state linuxRendererRecoveryJournal) error {
	body, err := json.Marshal(state)
	if err != nil {
		return err
	}
	path := linuxRendererRecoveryJournalPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(path, body, 0o600)
}

func claimLinuxRendererCompatibilityRestart(now time.Time, source string) bool {
	linuxRendererRecoveryJournalMu.Lock()
	defer linuxRendererRecoveryJournalMu.Unlock()
	if linuxRendererCompatibilityMode() {
		return false
	}
	if previous, err := readLinuxRendererRecoveryJournal(); err == nil && previous.Version == version {
		if attemptedAt, err := time.Parse(time.RFC3339Nano, previous.AttemptedAt); err == nil && now.Sub(attemptedAt) < linuxRendererRecoveryWindow {
			return false
		}
	}
	state := linuxRendererRecoveryJournal{
		SchemaVersion: linuxRendererRecoveryStateSchema,
		Version:       version,
		AttemptedAt:   now.UTC().Format(time.RFC3339Nano),
		Source:        metricBucket(source),
		Outcome:       "restarting",
	}
	return writeLinuxRendererRecoveryJournal(state) == nil
}

func recordLinuxRendererRecoveryOutcome(outcome string) {
	linuxRendererRecoveryJournalMu.Lock()
	defer linuxRendererRecoveryJournalMu.Unlock()
	state, err := readLinuxRendererRecoveryJournal()
	if err != nil || state.Version != version {
		return
	}
	state.Outcome = metricBucket(outcome)
	_ = writeLinuxRendererRecoveryJournal(state)
}

func (a *App) handleDesktopFrontendTimeout(source string) {
	if a == nil || goruntime.GOOS != "linux" || a.desktopShell.linuxRecovery == nil {
		return
	}
	a.desktopShell.linuxRecovery.requestCompatibilityRestart(source)
}

func (a *App) reportLinuxWebKitFrontendReady() {
	if a == nil || a.desktopShell.linuxRecovery == nil {
		return
	}
	a.desktopShell.linuxRecovery.frontendReady()
}

func (c *linuxWebKitRecoveryCoordinator) requestCompatibilityRestart(source string) {
	if c == nil || c.app == nil || goruntime.GOOS != "linux" {
		return
	}
	c.mu.Lock()
	if c.stopped || c.restartRequested || c.app.shuttingDown.Load() || c.app.forceQuit.Load() {
		c.mu.Unlock()
		return
	}
	c.restartRequested = true
	c.mu.Unlock()
	if !c.canRun() {
		return
	}

	// Present before any journal, relaunch or dialog work. A failed relaunch
	// must still leave a native recovery surface instead of a headless process.
	if c.app.desktopShell.coordinator != nil {
		c.app.desktopShell.coordinator.Present("renderer_" + metricBucket(source))
	}
	if claimLinuxRendererCompatibilityRestart(time.Now(), source) {
		if c.app.desktopShell.coordinator != nil {
			c.app.desktopShell.coordinator.markCompatibilityRestart()
		}
		c.app.saveWindowStateSync()
		c.app.snapshotAllTabs()
		if !c.canRun() {
			return
		}
		err := relaunchThroughLauncherWithEnv(map[string]string{
			linuxRendererCompatibilityEnv:  "1",
			linuxRendererRecoveryParentEnv: strconv.Itoa(os.Getpid()),
			linuxDisableCompositingEnv:     "1",
		})
		if err == nil {
			c.app.forceQuit.Store(true)
			wailsruntime.Quit(c.app.ctx)
			return
		}
		slog.Error("desktop: renderer compatibility relaunch failed", "err", err)
		recordLinuxRendererRecoveryOutcome("relaunch_failed")
	}
	c.showCompatibilityFailure()
}

func (c *linuxWebKitRecoveryCoordinator) showCompatibilityFailure() {
	if c == nil || c.app == nil || c.app.ctx == nil {
		return
	}
	c.mu.Lock()
	if c.stopped || c.failureDialogShown {
		c.mu.Unlock()
		return
	}
	c.failureDialogShown = true
	c.mu.Unlock()
	if c.app.desktopShell.coordinator != nil {
		c.app.desktopShell.coordinator.markFailed()
	}
	recordLinuxRendererRecoveryOutcome("failed")
	result, err := wailsruntime.MessageDialog(c.app.ctx, wailsruntime.MessageDialogOptions{
		Type:  wailsruntime.WarningDialog,
		Title: "Reasonix renderer unavailable / Reasonix 渲染器不可用",
		Message: "Reasonix could not confirm that the desktop interface is responsive, including in software-rendering compatibility mode. You can keep waiting or exit safely.\n\n" +
			"Reasonix 无法确认桌面界面已恢复响应，软件渲染兼容模式也未通过检查。你可以继续等待，或安全退出。",
		Buttons:       []string{"Keep waiting / 继续等待", "Exit / 退出"},
		DefaultButton: "Keep waiting / 继续等待",
		CancelButton:  "Keep waiting / 继续等待",
	})
	if err == nil && result == "Exit / 退出" {
		c.app.forceQuit.Store(true)
		wailsruntime.Quit(c.app.ctx)
	}
}

func (c *linuxWebKitRecoveryCoordinator) nativeEvent(event webKitNativeEvent, telemetry bool) {
	if c == nil || c.app == nil {
		return
	}
	c.mu.Lock()
	stopped := c.stopped
	c.mu.Unlock()
	if stopped {
		return
	}
	if event.recovery != webKitRecoverySucceeded {
		if telemetry {
			c.app.recordWebKitNativeDiagnostic(event)
		}
		if event.recovery == webKitRecoveryFailed {
			c.requestCompatibilityRestart("webkit_reload_failed")
		}
		return
	}

	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	if c.pendingTimer != nil {
		c.pendingTimer.Stop()
	}
	pending := event
	c.pendingEvent = &pending
	c.pendingTelemetry = telemetry
	c.pendingTimer = time.AfterFunc(linuxRendererHeartbeatTimeout, func() {
		c.mu.Lock()
		if c.pendingEvent == nil || c.pendingEvent.generation != event.generation {
			c.mu.Unlock()
			return
		}
		failed := *c.pendingEvent
		failed.recovery = webKitRecoveryFailed
		report := c.pendingTelemetry
		c.pendingEvent = nil
		c.pendingTelemetry = false
		c.pendingTimer = nil
		c.mu.Unlock()
		if report {
			c.app.recordWebKitNativeDiagnostic(failed)
		}
		c.requestCompatibilityRestart("webkit_heartbeat_timeout")
	})
	c.mu.Unlock()
}

func (c *linuxWebKitRecoveryCoordinator) frontendReady() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	pending := c.pendingEvent
	report := c.pendingTelemetry
	if c.pendingTimer != nil {
		c.pendingTimer.Stop()
	}
	c.pendingTimer = nil
	c.pendingEvent = nil
	c.pendingTelemetry = false
	c.mu.Unlock()
	if pending != nil && report {
		c.app.recordWebKitNativeDiagnostic(*pending)
	}
}

func (c *linuxWebKitRecoveryCoordinator) frontendHealthy() {
	if c == nil || !linuxRendererCompatibilityMode() {
		return
	}
	c.mu.Lock()
	if c.stopped || c.healthyRecorded {
		c.mu.Unlock()
		return
	}
	c.healthyRecorded = true
	c.mu.Unlock()
	recordLinuxRendererRecoveryOutcome("healthy")
}

func (c *linuxWebKitRecoveryCoordinator) stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.stopped = true
	c.restartRequested = true
	if c.pendingTimer != nil {
		c.pendingTimer.Stop()
	}
	c.pendingTimer = nil
	c.pendingEvent = nil
	c.pendingTelemetry = false
	c.mu.Unlock()
}

func (c *linuxWebKitRecoveryCoordinator) canRun() bool {
	if c == nil || c.app == nil || c.app.shuttingDown.Load() || c.app.forceQuit.Load() {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.stopped
}

func (a *App) recordWebKitNativeDiagnostic(event webKitNativeEvent) {
	if a == nil {
		return
	}
	report, outcome, failureBucket := webKitNativeFailureReport(event)
	_ = writePendingReport(report, true)
	a.recordDiagnosticMetric("desktop_web_runtime_failure", failureBucket)
	a.recordDiagnosticMetric("desktop_web_runtime_outcome", outcome)
}
