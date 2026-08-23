package main

import (
	"os"
	"path/filepath"
	"runtime"
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
	report := webView2ProcessFailureReportWithContext(2, 3, "132.0.2957.140")
	if report.Source != "web.runtime.fallback" || report.Label != "windows.webview2.process_failed" {
		t.Fatalf("report = %+v", report)
	}
	if report.FingerprintHint != "web.runtime.webview2.render_process_unresponsive.unknown.unknown" {
		t.Fatalf("fingerprint hint = %q", report.FingerprintHint)
	}
	for _, want := range []string{"runtime version: 132.0.2957.140", "same-process occurrence: 3", "exit code: unavailable"} {
		if !strings.Contains(report.Message, want) {
			t.Fatalf("report message missing %q: %s", want, report.Message)
		}
	}
}

func TestWebView2FailureTrackerDeduplicatesSessionBursts(t *testing.T) {
	var tracker webView2FailureTracker
	base := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	if occurrence, report := tracker.observe(2, base); occurrence != 1 || !report {
		t.Fatalf("first observation = (%d, %v)", occurrence, report)
	}
	if occurrence, report := tracker.observe(2, base.Add(time.Minute)); occurrence != 2 || report {
		t.Fatalf("burst observation = (%d, %v)", occurrence, report)
	}
	if occurrence, report := tracker.observe(2, base.Add(webView2FailureReportCooldown)); occurrence != 3 || !report {
		t.Fatalf("post-cooldown observation = (%d, %v)", occurrence, report)
	}
	if occurrence, report := tracker.observe(6, base.Add(time.Minute)); occurrence != 1 || !report {
		t.Fatalf("different kind observation = (%d, %v)", occurrence, report)
	}
}

func TestWebView2NativeFailureClassification(t *testing.T) {
	tests := []struct {
		name     string
		event    webView2NativeEvent
		kind     string
		outcome  string
		recovery string
	}{
		{name: "browser fatal", event: webView2NativeEvent{Kind: 0, ReasonAvailable: true, Reason: 3}, kind: "crash", outcome: "fatal_app_exit", recovery: webView2RecoveryNotApplicable},
		{name: "renderer recovered", event: webView2NativeEvent{Kind: 1, Recovery: webView2RecoverySucceeded}, kind: "performance", outcome: "recovered", recovery: webView2RecoverySucceeded},
		{name: "renderer recovery failed", event: webView2NativeEvent{Kind: 2, Recovery: webView2RecoveryFailed}, kind: "exception", outcome: "recovery_failed", recovery: webView2RecoveryFailed},
		{name: "gpu degraded", event: webView2NativeEvent{Kind: 6}, kind: "performance", outcome: "degraded", recovery: webView2RecoveryNotApplicable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, outcome := webView2NativeFailureReport(tt.event, "132.0.1", true)
			if report.Kind != tt.kind || outcome != tt.outcome || report.WebRuntime == nil || report.WebRuntime.Recovery != tt.recovery {
				t.Fatalf("report=%+v diagnostic=%+v outcome=%q", report, report.WebRuntime, outcome)
			}
		})
	}
}

func TestWebView2NativeFailureSanitizesModuleAndFingerprint(t *testing.T) {
	report, _ := webView2NativeFailureReport(webView2NativeEvent{
		Kind:                2,
		Reason:              1,
		ReasonAvailable:     true,
		ExitCode:            259,
		ExitCodeAvailable:   true,
		FailureSourceModule: `C:\Users\alice\Security Suite\inject.dll`,
		Recovery:            webView2RecoveryFailed,
	}, "", false)
	if got := report.WebRuntime.FailureSourceModule; got != "inject.dll" {
		t.Fatalf("failure source module = %q", got)
	}
	if strings.Contains(report.Message, `C:\Users`) || strings.Contains(report.FingerprintHint, "259") {
		t.Fatalf("report leaked a path or fingerprinted STILL_ACTIVE: %+v", report)
	}
	if report.FingerprintHint != "web.runtime.webview2.render_process_unresponsive.unresponsive.unknown" {
		t.Fatalf("fingerprint = %q", report.FingerprintHint)
	}
}

func TestWebKitNativeFailureClassification(t *testing.T) {
	context := webRuntimeContext{Engine: "webkitgtk", RuntimeVersion: "2.42.5", GPUMode: "on_demand"}
	recovered, outcome, failure := webKitNativeFailureReport(webKitNativeEvent{reason: 0, recovery: webKitRecoverySucceeded, runtimeContext: context})
	if recovered.Kind != "performance" || outcome != "recovered" || failure != "webkitgtk.web_process.crashed" {
		t.Fatalf("recovered report=%+v outcome=%q failure=%q", recovered, outcome, failure)
	}
	if recovered.WebRuntime == nil || recovered.WebRuntime.Engine != "webkitgtk" || recovered.WebRuntime.GPUMode != "on_demand" {
		t.Fatalf("runtime diagnostic=%+v", recovered.WebRuntime)
	}
	failed, outcome, _ := webKitNativeFailureReport(webKitNativeEvent{reason: 1, recovery: webKitRecoveryFailed, runtimeContext: context})
	if failed.Kind != "exception" || outcome != "recovery_failed" || failed.WebRuntime.Reason != "out_of_memory" {
		t.Fatalf("failed report=%+v outcome=%q", failed, outcome)
	}
	degraded, outcome, _ := webKitNativeFailureReport(webKitNativeEvent{reason: 99, recovery: webKitRecoveryNotApplicable, runtimeContext: context})
	if degraded.Kind != "performance" || outcome != "degraded" || degraded.WebRuntime.Reason != "unknown" {
		t.Fatalf("degraded report=%+v outcome=%q", degraded, outcome)
	}
}

func TestWebKitObserverReloadIsAbnormalPathOnly(t *testing.T) {
	source, err := os.ReadFile("webkit_diagnostics_linux.c")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Count(text, "webkit_web_view_reload(") != 1 {
		t.Fatalf("observer must have exactly one native reload call site")
	}
	_, recoveryTail, ok := strings.Cut(text, "static gboolean reasonix_reload_after_termination")
	if !ok {
		t.Fatal("native reload is not deferred until after termination signal dispatch")
	}
	reloadHelper, terminationTail, ok := strings.Cut(recoveryTail, "static void reasonix_web_process_terminated")
	if !ok || !strings.Contains(reloadHelper, "webkit_web_view_reload(") {
		t.Fatal("native reload escaped the deferred recovery helper")
	}
	terminationBody, _, ok := strings.Cut(terminationTail, "static gboolean reasonix_load_failed")
	if !ok || !strings.Contains(terminationBody, "reasonix_reload_after_termination") {
		t.Fatal("web-process termination does not schedule the bounded recovery helper")
	}
	if !strings.Contains(text, "if (!reasonix_recovery_pending) return;") ||
		!strings.Contains(text, "if (reasonix_recovery_load_started && event == WEBKIT_LOAD_FINISHED)") {
		t.Fatal("ordinary load-finished events are not gated by recovery state")
	}
	if !strings.Contains(text, "reasonix_recovery_pending && reasonix_recovery_load_started") {
		t.Fatal("a stale load failure could be attributed to the recovery navigation")
	}
	for _, forbidden := range []string{"fopen(", "open(", "curl_", "send(", "recv("} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("GTK callback source contains blocking I/O primitive %q", forbidden)
		}
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
	if report.Source != "native.lifecycle.legacy" || report.Label != "desktop.legacy_abnormal_exit" {
		t.Fatalf("report = %+v", report)
	}
	if !strings.Contains(report.Message, "uptime bucket: m_2_10") {
		t.Fatalf("message missing bounded uptime: %q", report.Message)
	}
}

func TestDesktopLifecycleReportUsesCurrentLifecycleNamespace(t *testing.T) {
	report := desktopLifecycleReport(desktopLifecycleObservation{
		Version: "v1.23.0", Channel: "stable", Phase: "healthy",
		StartedAt: "2026-08-10T01:00:00Z", UpdatedAt: "2026-08-10T02:00:00Z",
	})
	if report.Source != "native.lifecycle" || report.Label != "desktop.abnormal_exit.v2" {
		t.Fatalf("report = %+v", report)
	}
	if report.FingerprintHint != "desktop.abnormal_exit.v2."+runtime.GOOS+".healthy" {
		t.Fatalf("fingerprint = %q", report.FingerprintHint)
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
	platform := metricBucket(runtime.GOOS)
	if report.Label != platform+".window_restore.timeout" || report.TopFrame != platform+".window_restore.second_instance" {
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
