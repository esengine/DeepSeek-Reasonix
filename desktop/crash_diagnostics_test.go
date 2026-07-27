package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/repair"
)

func TestParseWebView2ProcessFailure(t *testing.T) {
	kind, ok := parseWebView2ProcessFailure("windows | WebVie2wProcess failed with kind 6")
	if !ok || kind != 6 {
		t.Fatalf("kind=%d ok=%v", kind, ok)
	}
	if _, ok := parseWebView2ProcessFailure("unrelated failure"); ok {
		t.Fatal("unrelated log message matched WebView2 failure")
	}
}

func TestWebView2ProcessFailureReportIsStructured(t *testing.T) {
	report := webView2ProcessFailureReport(2)
	if report.Source != "webview2.process" || report.Label != "windows.webview2.process_failed" {
		t.Fatalf("report = %+v", report)
	}
	if report.FingerprintHint != "windows.webview2.render_process_unresponsive" {
		t.Fatalf("fingerprint hint = %q", report.FingerprintHint)
	}
}

func TestPreviousRunReportUsesOnlyBoundedLifecycleContext(t *testing.T) {
	report := previousRunReport(repair.PreviousRunObservation{
		Abnormal:       true,
		Phase:          "healthy",
		Version:        "v2",
		InstallProfile: "installer",
		UpdateFrom:     "v1",
		UpdateTo:       "v2",
		UptimeBucket:   "m_2_10",
	})
	if report.Source != "native.lifecycle" || report.Label != "desktop.abnormal_exit" {
		t.Fatalf("report = %+v", report)
	}
	if !strings.Contains(report.Message, "uptime bucket: m_2_10") {
		t.Fatalf("message missing bounded uptime: %q", report.Message)
	}
}

func TestCapturePreviousFatalCrashQueuesAndRemovesRawDump(t *testing.T) {
	resetFatalCrashArtifacts(t)
	const crashedPID = 424242
	raw := "fatal error: concurrent map writes\n\ngoroutine 1 [running]:\nmain.run()\n\t/home/alice/project/main.go:12\n"
	path := fatalCrashPathForPID(crashedPID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	capturePreviousFatalCrash()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("raw fatal dump was not removed: %v", err)
	}
	report, ok := readPending(t)
	if !ok || report.Label != "go.fatal" || report.Source != "go.runtime" {
		t.Fatalf("queued report = %+v ok=%v", report, ok)
	}
	if strings.Contains(report.Stack, "/home/alice") {
		t.Fatalf("fatal stack leaked home path: %q", report.Stack)
	}
}

func TestSanitizeFatalRuntimeDumpRemovesPanicValue(t *testing.T) {
	got := sanitizeFatalRuntimeDump("panic: private prompt text\n\ngoroutine 1 [running]:\nmain.run()\n\t/home/alice/project/main.go:12\n")
	if strings.Contains(got, "private prompt text") || strings.Contains(got, "/home/alice") {
		t.Fatalf("fatal dump leaked user-controlled text: %q", got)
	}
	if !strings.Contains(got, "panic: [redacted panic value]") || !strings.Contains(got, "main.go:12") {
		t.Fatalf("fatal dump lost diagnostic structure: %q", got)
	}
}

func TestCapturePreviousFatalCrashSkipsRuntimeDuplicateOfStructuredPanic(t *testing.T) {
	resetFatalCrashArtifacts(t)
	const crashedPID = 424243
	path := fatalCrashPathForPID(crashedPID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("panic: duplicate\n\ngoroutine 1 [running]:\nmain.run()\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	markFatalCrashCoveredForPID(crashedPID)
	capturePreviousFatalCrash()
	if _, ok := readPending(t); ok {
		t.Fatal("runtime duplicate was queued despite structured panic marker")
	}
}

func TestCapturePreviousFatalCrashDoesNotTouchLiveOwner(t *testing.T) {
	resetFatalCrashArtifacts(t)
	const livePID = 424244
	path := fatalCrashPathForPID(livePID)
	raw := []byte("fatal error: still being written\n\ngoroutine 1 [running]:\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	oldProcessAlive := fatalCrashProcessAlive
	fatalCrashProcessAlive = func(pid int) bool { return pid == livePID }
	t.Cleanup(func() { fatalCrashProcessAlive = oldProcessAlive })

	capturePreviousFatalCrash()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("live owner's fatal file was removed: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("live owner's fatal file changed: got %q want %q", got, raw)
	}
	if _, ok := readPending(t); ok {
		t.Fatal("live owner's partial fatal output was queued")
	}
}

func TestCapturePreviousFatalCrashKeepsEmptyLegacyFile(t *testing.T) {
	resetFatalCrashArtifacts(t)
	if err := os.MkdirAll(filepath.Dir(legacyFatalCrashPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyFatalCrashPath(), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	capturePreviousFatalCrash()

	if _, err := os.Stat(legacyFatalCrashPath()); err != nil {
		t.Fatalf("empty legacy fatal file may belong to a live older process: %v", err)
	}
}

func TestCapturePreviousFatalCrashMigratesLegacyDump(t *testing.T) {
	resetFatalCrashArtifacts(t)
	raw := "fatal error: legacy crash\n\ngoroutine 1 [running]:\nmain.run()\n\t/home/alice/project/main.go:12\n"
	if err := os.MkdirAll(filepath.Dir(legacyFatalCrashPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyFatalCrashPath(), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	capturePreviousFatalCrash()

	if _, err := os.Stat(legacyFatalCrashPath()); !os.IsNotExist(err) {
		t.Fatalf("captured legacy fatal dump was not removed: %v", err)
	}
	report, ok := readPending(t)
	if !ok || report.Label != "go.fatal" || report.Source != "go.runtime" {
		t.Fatalf("legacy fatal dump was not migrated: report=%+v ok=%v", report, ok)
	}
}

func TestAwaitWindowRestoreRequiresNativeConfirmation(t *testing.T) {
	ticks := make(chan time.Time)
	deadline := make(chan time.Time, 1)
	deadline <- time.Now()

	if awaitWindowRestoreConfirmation(func() bool { return false }, ticks, deadline) {
		t.Fatal("an enqueued show request without native confirmation was treated as restored")
	}
}

func TestAwaitWindowRestoreAcceptsConfirmationAfterTick(t *testing.T) {
	ticks := make(chan time.Time, 1)
	deadline := make(chan time.Time)
	var confirmed atomic.Bool
	result := make(chan bool, 1)
	go func() {
		result <- awaitWindowRestoreConfirmation(confirmed.Load, ticks, deadline)
	}()

	confirmed.Store(true)
	ticks <- time.Now()
	if !<-result {
		t.Fatal("native window confirmation was not accepted")
	}
}

func TestSupersededWindowRestoreDoesNotRemoveLatestJournal(t *testing.T) {
	path := windowRestoreStatePath()
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
	oldSequence := windowRestoreSequence.Load()
	t.Cleanup(func() { windowRestoreSequence.Store(oldSequence) })

	latest := windowRestoreState{
		SchemaVersion: windowRestoreStateVersion,
		PID:           os.Getpid(),
		AttemptID:     2,
		Source:        "tray",
		StartedAt:     "2026-07-24T08:01:00Z",
	}
	if !writeWindowRestoreState(latest) {
		t.Fatal("write latest window restore state")
	}
	windowRestoreSequence.Store(latest.AttemptID)

	NewApp().completeWindowRestoreAttempt(1, windowRestoreState{AttemptID: 1}, true)

	got, err := readWindowRestoreState()
	if err != nil {
		t.Fatalf("latest journal was removed by superseded attempt: %v", err)
	}
	if got != latest {
		t.Fatalf("latest journal changed: got %+v want %+v", got, latest)
	}
}

func resetFatalCrashArtifacts(t *testing.T) {
	t.Helper()
	oldProcessAlive := fatalCrashProcessAlive
	fatalCrashProcessAlive = func(int) bool { return false }
	removeAllPendingCrashes()
	_ = os.Remove(legacyFatalCrashPath())
	_ = os.Remove(legacyFatalCrashCoveredPath())
	_ = os.RemoveAll(fatalCrashDir())
	t.Cleanup(func() {
		removeAllPendingCrashes()
		_ = os.Remove(legacyFatalCrashPath())
		_ = os.Remove(legacyFatalCrashCoveredPath())
		_ = os.RemoveAll(fatalCrashDir())
		fatalCrashProcessAlive = oldProcessAlive
	})
}

func TestWindowRestoreFailureReportSeparatesTimeoutAndSource(t *testing.T) {
	report := windowRestoreFailureReport("timeout", "second_instance", "2026-07-24T08:00:00Z")
	if report.Label != "windows.window_restore.timeout" || report.TopFrame != "windows.window_restore.second_instance" {
		t.Fatalf("report = %+v", report)
	}
}

func TestWriteWindowRestoreStateCreatesReadableJournal(t *testing.T) {
	path := windowRestoreStatePath()
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })

	want := windowRestoreState{
		SchemaVersion: windowRestoreStateVersion,
		PID:           os.Getpid(),
		Source:        "tray",
		StartedAt:     "2026-07-24T08:00:00Z",
	}
	if !writeWindowRestoreState(want) {
		t.Fatal("writeWindowRestoreState returned false")
	}
	got, err := readWindowRestoreState()
	if err != nil {
		t.Fatalf("readWindowRestoreState: %v", err)
	}
	if got != want {
		t.Fatalf("journal = %+v, want %+v", got, want)
	}
}
