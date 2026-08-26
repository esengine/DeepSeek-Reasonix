package main

import (
	"context"
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
	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
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
	currentCrashSchema   = 3
	crashLedgerFile      = "crash-upload-ledger-v1.json"
	maxCrashLedger       = 512
	crashLedgerRetention = 180 * 24 * time.Hour
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
	if ensureCrashIdentity(&report) != nil {
		return false
	}
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
	return prunePendingCrashQueue(path)
}

func prunePendingCrashQueue(writtenPath string) bool {
	paths := pendingCrashQueuePaths()
	for len(paths) > maxPendingCrashes {
		victim := -1
		for index, candidate := range paths {
			body, err := os.ReadFile(candidate)
			var header struct {
				SchemaVersion int `json:"schemaVersion"`
			}
			if err != nil || json.Unmarshal(body, &header) != nil || header.SchemaVersion <= currentCrashSchema {
				victim = index
				break
			}
		}
		if victim < 0 {
			break
		}
		_ = os.Remove(paths[victim])
		paths = append(paths[:victim], paths[victim+1:]...)
	}
	_, err := os.Stat(writtenPath)
	return err == nil
}

type crashUploadLedger struct {
	Version int               `json:"version"`
	Entries map[string]string `json:"entries"`
}

func crashLedgerPath() string {
	return filepath.Join(config.MemoryUserDir(), "diagnostics", crashLedgerFile)
}

func crashLedgerKey(report crashReport) string {
	return report.Version + ":" + report.DedupKey
}

func loadCrashLedger(path string, now time.Time) crashUploadLedger {
	ledger := crashUploadLedger{Version: 1, Entries: map[string]string{}}
	body, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(body, &ledger) != nil || ledger.Version != 1 || ledger.Entries == nil {
		return crashUploadLedger{Version: 1, Entries: map[string]string{}}
	}
	cutoff := now.Add(-crashLedgerRetention)
	for key, value := range ledger.Entries {
		at, err := time.Parse(time.RFC3339Nano, value)
		if err != nil || at.Before(cutoff) {
			delete(ledger.Entries, key)
		}
	}
	return ledger
}

func saveCrashLedger(path string, ledger crashUploadLedger) error {
	if len(ledger.Entries) > maxCrashLedger {
		type row struct{ key, at string }
		rows := make([]row, 0, len(ledger.Entries))
		for key, at := range ledger.Entries {
			rows = append(rows, row{key: key, at: at})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].at > rows[j].at })
		for _, item := range rows[maxCrashLedger:] {
			delete(ledger.Entries, item.key)
		}
	}
	body, err := json.Marshal(ledger)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, body, 0o600)
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
	_ = os.Remove(crashLedgerPath())
	_ = os.Remove(crashLedgerPath() + ".lock")
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
	lockContext, cancel := context.WithTimeout(a.bootContext(), 3*time.Second)
	defer cancel()
	ledgerPath := crashLedgerPath()
	if os.MkdirAll(filepath.Dir(ledgerPath), 0o700) != nil {
		return
	}
	release, err := filelock.AcquireWithExternalTimeout(lockContext, ledgerPath+".lock", 2*time.Second)
	if err != nil {
		return
	}
	defer release()
	ledger := loadCrashLedger(ledgerPath, time.Now().UTC())
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
		if r.SchemaVersion > currentCrashSchema {
			continue
		}
		identityChanged := r.EventID == "" || r.DedupKey == ""
		if ensureCrashIdentity(&r) != nil {
			break
		}
		if identityChanged {
			updated, marshalErr := json.Marshal(r)
			if marshalErr != nil || fileutil.AtomicWriteFile(path, updated, 0o600) != nil {
				break
			}
		}
		key := crashLedgerKey(r)
		if _, alreadySent := ledger.Entries[key]; alreadySent {
			_ = os.Remove(path)
			continue
		}
		if postCrashReport(a.bootContext(), c, crashEndpoint, r) != nil {
			break
		}
		ledger.Entries[key] = time.Now().UTC().Format(time.RFC3339Nano)
		if saveCrashLedger(ledgerPath, ledger) != nil {
			break
		}
		_ = os.Remove(path)
	}
	_ = os.Remove(pendingCrashDir())
}
