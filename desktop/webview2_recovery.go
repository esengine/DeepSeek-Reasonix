package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/config"
)

const (
	webView2RecoveryJournalSchema = 1
	webView2ReadyTimeout          = 10 * time.Second
	webView2RestartGuardWindow    = 5 * time.Minute
	webView2GuidanceRetention     = 24 * time.Hour
)

type webView2RecoveryPhase string

const (
	webView2RecoveryIdle          webView2RecoveryPhase = "idle"
	webView2RecoveryAwaitingReady webView2RecoveryPhase = "awaiting_ready"
	webView2RecoveryRestarting    webView2RecoveryPhase = "restarting"
	webView2RecoveryGuarded       webView2RecoveryPhase = "guarded"
)

type webView2RecoveryAction string

const (
	webView2RecoveryNoAction      webView2RecoveryAction = "none"
	webView2RecoveryWaitForReady  webView2RecoveryAction = "wait_for_ready"
	webView2RecoveryMarkRecovered webView2RecoveryAction = "mark_recovered"
	webView2RecoveryRestart       webView2RecoveryAction = "restart"
)

type webView2RecoveryState struct {
	Phase    webView2RecoveryPhase
	Deadline time.Time
	Reason   string
}

func (s webView2RecoveryState) nativeFailure(event webView2NativeEvent, now time.Time) (webView2RecoveryState, webView2RecoveryAction) {
	reason := webView2ProcessKindBucket(event.Kind) + "." + normalizeWebView2Recovery(event.Recovery)
	switch event.Kind {
	case 0: // browser process: the controller cannot be recreated in place.
		s.Phase = webView2RecoveryRestarting
		s.Reason = reason
		return s, webView2RecoveryRestart
	case 1, 2: // main renderer exited or became unresponsive.
		switch event.Recovery {
		case "reload_navigation_succeeded", webView2RecoverySucceeded:
			s.Phase = webView2RecoveryAwaitingReady
			s.Deadline = now.Add(webView2ReadyTimeout)
			s.Reason = reason
			return s, webView2RecoveryWaitForReady
		case webView2RecoveryFailed, "reload_suppressed":
			s.Phase = webView2RecoveryRestarting
			s.Reason = reason
			return s, webView2RecoveryRestart
		default:
			// A main-renderer event without a confirmed reload is not healthy.
			s.Phase = webView2RecoveryRestarting
			s.Reason = reason
			return s, webView2RecoveryRestart
		}
	default:
		// Frame/GPU/utility process failures remain diagnostic-only. WebView2
		// normally recreates these processes; renderer heartbeat decides liveness.
		return s, webView2RecoveryNoAction
	}
}

func (s webView2RecoveryState) ready(now time.Time) (webView2RecoveryState, webView2RecoveryAction) {
	if s.Phase != webView2RecoveryAwaitingReady || now.After(s.Deadline) {
		return s, webView2RecoveryNoAction
	}
	s.Phase = webView2RecoveryIdle
	s.Deadline = time.Time{}
	s.Reason = ""
	return s, webView2RecoveryMarkRecovered
}

func (s webView2RecoveryState) tick(now time.Time) (webView2RecoveryState, webView2RecoveryAction) {
	if s.Phase != webView2RecoveryAwaitingReady || now.Before(s.Deadline) {
		return s, webView2RecoveryNoAction
	}
	s.Phase = webView2RecoveryRestarting
	s.Reason = "renderer_ready_timeout"
	return s, webView2RecoveryRestart
}

type webView2RecoveryJournalRecord struct {
	SchemaVersion int    `json:"schemaVersion"`
	At            string `json:"at"`
	Stage         string `json:"stage"`
	Reason        string `json:"reason,omitempty"`
	Version       string `json:"version,omitempty"`
}

type webView2RecoveryJournal struct {
	path string
	now  func() time.Time
}

func newWebView2RecoveryJournal() webView2RecoveryJournal {
	return webView2RecoveryJournal{
		path: filepath.Join(config.MemoryUserDir(), "diagnostics", "webview2-recovery.jsonl"),
		now:  func() time.Time { return time.Now().UTC() },
	}
}

func (j webView2RecoveryJournal) append(stage, reason string) error {
	if strings.TrimSpace(j.path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
		return err
	}
	record := webView2RecoveryJournalRecord{
		SchemaVersion: webView2RecoveryJournalSchema,
		At:            j.now().UTC().Format(time.RFC3339Nano),
		Stage:         stage,
		Reason:        sanitizeCrashField(reason, 255),
		Version:       sanitizeCrashField(version, 64),
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}

func (j webView2RecoveryJournal) records() ([]webView2RecoveryJournalRecord, error) {
	file, err := os.Open(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	records := make([]webView2RecoveryJournalRecord, 0, 16)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 64*1024)
	for scanner.Scan() {
		var record webView2RecoveryJournalRecord
		if json.Unmarshal(scanner.Bytes(), &record) != nil || record.SchemaVersion != webView2RecoveryJournalSchema {
			continue
		}
		records = append(records, record)
		if len(records) > 256 {
			records = append([]webView2RecoveryJournalRecord(nil), records[len(records)-128:]...)
		}
	}
	return records, scanner.Err()
}

func (j webView2RecoveryJournal) automaticRestartAllowed(now time.Time) (bool, error) {
	records, err := j.records()
	if err != nil {
		return false, err
	}
	for _, record := range slices.Backward(records) {
		if record.Stage != "auto_restart" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, record.At)
		if err != nil {
			continue
		}
		return now.Sub(at) >= webView2RestartGuardWindow, nil
	}
	return true, nil
}

func (j webView2RecoveryJournal) pendingGuidance(now time.Time) (bool, error) {
	records, err := j.records()
	if err != nil {
		return false, err
	}
	for _, record := range slices.Backward(records) {
		switch record.Stage {
		case "guidance_shown":
			return false, nil
		case "restart_blocked", "restart_launch_failed":
			at, err := time.Parse(time.RFC3339Nano, record.At)
			return err == nil && now.Sub(at) <= webView2GuidanceRetention, nil
		}
	}
	return false, nil
}

type webView2RecoveryCoordinator struct {
	mu      sync.Mutex
	state   webView2RecoveryState
	timer   *time.Timer
	app     *App
	journal webView2RecoveryJournal
	now     func() time.Time

	//lint:ignore U1000 Used by the Windows-only native failure path.
	restartOnce sync.Once
}

func newWebView2RecoveryCoordinator(app *App) *webView2RecoveryCoordinator {
	return &webView2RecoveryCoordinator{
		app:     app,
		journal: newWebView2RecoveryJournal(),
		now:     time.Now,
	}
}

//lint:ignore U1000 Used by webview2_diagnostics_windows.go.
func (c *webView2RecoveryCoordinator) nativeFailure(event webView2NativeEvent) {
	if c == nil {
		return
	}
	c.mu.Lock()
	next, action := c.state.nativeFailure(event, c.now())
	c.state = next
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if action == webView2RecoveryWaitForReady {
		deadline := next.Deadline
		c.timer = time.AfterFunc(time.Until(deadline), c.readyTimeout)
	}
	reason := next.Reason
	c.mu.Unlock()

	if action == webView2RecoveryRestart {
		c.requestRestart(reason)
	}
}

func (c *webView2RecoveryCoordinator) reportReady() {
	if c == nil {
		return
	}
	c.mu.Lock()
	next, action := c.state.ready(c.now())
	c.state = next
	if action == webView2RecoveryMarkRecovered && c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
	if action == webView2RecoveryMarkRecovered {
		_ = c.journal.append("reload_recovered", "frontend_ready")
	}
}

//lint:ignore U1000 Scheduled by nativeFailure on Windows.
func (c *webView2RecoveryCoordinator) readyTimeout() {
	c.mu.Lock()
	next, action := c.state.tick(c.now())
	c.state = next
	c.timer = nil
	reason := next.Reason
	c.mu.Unlock()
	if action == webView2RecoveryRestart {
		c.requestRestart(reason)
	}
}

//lint:ignore U1000 Invoked by the Windows-only recovery path.
func (c *webView2RecoveryCoordinator) requestRestart(reason string) {
	c.restartOnce.Do(func() {
		now := c.now()
		allowed, err := c.journal.automaticRestartAllowed(now)
		if err != nil {
			slog.Warn("desktop: read WebView2 restart guard", "err", err)
			allowed = false
		}
		if !allowed {
			_ = c.journal.append("restart_blocked", reason)
			c.mu.Lock()
			c.state.Phase = webView2RecoveryGuarded
			c.mu.Unlock()
			return
		}

		if c.app != nil {
			c.app.saveWindowStateSync()
			c.app.snapshotAllTabs()
			c.app.lifecycle.tracker.markAsync("webview2_restarting")
		}
		if err := c.journal.append("auto_restart", reason); err != nil {
			slog.Warn("desktop: append WebView2 restart journal", "err", err)
		}
		if err := relaunchThroughLauncher(); err != nil {
			_ = c.journal.append("restart_launch_failed", fmt.Sprintf("%s: %v", reason, err))
			slog.Error("desktop: relaunch after WebView2 failure", "err", err)
			return
		}
		if c.app != nil && c.app.ctx != nil {
			runtime.Quit(c.app.ctx)
		}
	})
}

func (c *webView2RecoveryCoordinator) startGuidance(ctx context.Context) {
	if c == nil || ctx == nil {
		return
	}
	pending, err := c.journal.pendingGuidance(c.now())
	if err != nil || !pending {
		return
	}
	go func() {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		showWindowsWebView2RecoveryGuidance(ctx)
		_ = c.journal.append("guidance_shown", "crash_loop_guard")
	}()
}
