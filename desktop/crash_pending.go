package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"reasonix/internal/config"
)

// crash_pending.go captures Go-side panics to disk and ships them on the next
// launch. Frontend crashes are click-to-send, but an unrecovered Go panic kills the
// process before the user can react, so the whole agent/provider/tool layer would
// otherwise never surface a single report. The resend is gated on the same
// desktop.telemetry opt-out as the launch ping.

const pendingCrashFile = "crash-pending.json"

func pendingCrashPath() string {
	return filepath.Join(filepath.Dir(config.UserConfigPath()), pendingCrashFile)
}

// recoverToPending records a panicking goroutine to the pending-crash file and
// re-raises, so the process still crashes exactly as before — the stack is now
// shipped next launch instead of lost.
func (a *App) recoverToPending(site string) {
	r := recover()
	if r == nil {
		return
	}
	writePendingCrash(site, r, debug.Stack())
	panic(r)
}

func writePendingCrash(site string, r any, stack []byte) {
	msg := scrubUserPaths(fmt.Sprintf("[go panic] %s: %v\n\n%s", site, r, stack))
	if len(msg) > maxCrashDetailBytes {
		msg = msg[:maxCrashDetailBytes]
	}
	body, err := json.Marshal(crashReport{
		Kind:    "crash",
		Version: version,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Message: msg,
		Device:  collectDeviceInfo(),
	})
	if err != nil {
		return
	}
	path := pendingCrashPath()
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	_ = os.WriteFile(path, body, 0o644)
}

// flushPendingCrash drains a Go panic captured on a prior run and POSTs it, then
// clears it. Runs at launch alongside the ping; honours the telemetry opt-out by
// dropping the file unsent.
func (a *App) flushPendingCrash() {
	if version == "dev" {
		return
	}
	path := pendingCrashPath()
	body, err := os.ReadFile(path)
	if err != nil {
		return
	}
	cfg, err := config.Load()
	if err != nil || !cfg.DesktopTelemetry() {
		_ = os.Remove(path)
		return
	}
	var r crashReport
	if json.Unmarshal(body, &r) != nil {
		_ = os.Remove(path)
		return
	}
	c, err := httpClient()
	if err != nil {
		return
	}
	if postCrashReport(a.bootContext(), c, crashEndpoint, r) == nil {
		_ = os.Remove(path)
	}
}
