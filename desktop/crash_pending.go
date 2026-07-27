package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/config"
)

// crash_pending.go captures Go-side panics to disk and ships them on the next
// launch. Frontend crashes are click-to-send, but an unrecovered Go panic kills the
// process before the user can react, so the whole agent/provider/tool layer would
// otherwise never surface a single report. The resend is gated on the same
// desktop.telemetry opt-out as the launch ping.

const (
	pendingCrashFile     = "crash-pending.json" // legacy single-report path
	pendingCrashQueueDir = "crash-pending"
	maxPendingCrashes    = 10
)

var (
	pendingCrashMu       sync.Mutex
	pendingCrashSequence atomic.Uint64
)

func pendingCrashPath() string {
	return filepath.Join(config.MemoryUserDir(), pendingCrashFile)
}

func pendingCrashDir() string {
	return filepath.Join(config.MemoryUserDir(), pendingCrashQueueDir)
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
	stackText := string(stack)
	msg := sanitizeCrashText(fmt.Sprintf("[go panic] %s\n\n%s", site, stackText), maxCrashDetailBytes)
	report := baseCrashReport("crash")
	report.SchemaVersion = 2
	report.Source = "go"
	report.Label = sanitizeCrashField(site, 64)
	report.ErrorType = sanitizeCrashField(fmt.Sprintf("%T", r), 128)
	report.ErrorMessage = sanitizeCrashText("Go panic captured at "+site+".", maxCrashFieldBytes)
	report.Stack = sanitizeCrashText(stackText, maxCrashStackBytes)
	report.TopFrame = topFrameFromStack(report.Stack)
	report.Message = msg
	if writePendingReport(report, true) {
		markFatalCrashCovered()
	}
}

func writePendingReport(report crashReport, overwrite bool) bool {
	_ = overwrite // retained for source compatibility; the queue never overwrites.
	body, err := json.Marshal(report)
	if err != nil {
		return false
	}
	dir := pendingCrashDir()
	if os.MkdirAll(dir, 0o700) != nil {
		return false
	}
	pendingCrashMu.Lock()
	defer pendingCrashMu.Unlock()
	name := strconv.FormatInt(time.Now().UTC().UnixNano(), 10) + "-" +
		strconv.Itoa(os.Getpid()) + "-" +
		strconv.FormatUint(pendingCrashSequence.Add(1), 10) + ".json"
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false
	}
	n, err := f.Write(body)
	closeErr := f.Close()
	if err != nil || closeErr != nil || n != len(body) {
		_ = os.Remove(path)
		return false
	}
	paths := pendingCrashQueuePaths()
	for len(paths) > maxPendingCrashes {
		_ = os.Remove(paths[0])
		paths = paths[1:]
	}
	return true
}

func pendingCrashQueuePaths() []string {
	entries, err := os.ReadDir(pendingCrashDir())
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			paths = append(paths, filepath.Join(pendingCrashDir(), entry.Name()))
		}
	}
	sort.Strings(paths)
	return paths
}

func pendingCrashPaths() []string {
	paths := make([]string, 0, maxPendingCrashes+1)
	if _, err := os.Stat(pendingCrashPath()); err == nil {
		paths = append(paths, pendingCrashPath())
	}
	return append(paths, pendingCrashQueuePaths()...)
}

func removeAllPendingCrashes() {
	_ = os.Remove(pendingCrashPath())
	_ = os.RemoveAll(pendingCrashDir())
	_ = os.Remove(fatalCrashCoveredPath())
}

func (a *App) goSafe(site string, fn func()) {
	go func() {
		defer a.recoverToPending(site)
		fn()
	}()
}

// flushPendingCrash drains a Go panic captured on a prior run and POSTs it, then
// clears it. Runs at launch alongside the ping; honours the telemetry opt-out by
// dropping the file unsent.
func (a *App) flushPendingCrash() {
	if version == "dev" {
		return
	}
	// Safe Mode boots from built-in defaults and cannot read the user's real
	// telemetry preference, so it must neither send the pending report nor
	// consume it: leave the file for the next normal boot to decide.
	if config.SafeModeRequested() {
		return
	}
	paths := pendingCrashPaths()
	if len(paths) == 0 {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	if !cfg.DesktopTelemetry() {
		removeAllPendingCrashes()
		return
	}
	c, err := httpClient()
	if err != nil {
		return
	}
	for _, path := range paths {
		body, readErr := readFileUTF8(path)
		if readErr != nil {
			continue
		}
		var r crashReport
		if json.Unmarshal(body, &r) != nil {
			_ = os.Remove(path)
			continue
		}
		if postCrashReport(a.bootContext(), c, crashEndpoint, r) != nil {
			break
		}
		_ = os.Remove(path)
	}
	_ = os.Remove(pendingCrashDir())
}
