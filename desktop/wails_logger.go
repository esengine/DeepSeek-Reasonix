package main

import (
	"fmt"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/logger"
)

type crashCaptureLogger struct {
	delegate         logger.Logger
	app              *App
	webView2Failures webView2FailureTracker
}

const webView2FailureReportCooldown = 10 * time.Minute

type webView2FailureTracker struct {
	mu           sync.Mutex
	counts       map[int]int
	lastReported map[int]time.Time
}

func (t *webView2FailureTracker) observe(kind int, now time.Time) (occurrence int, shouldReport bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.counts == nil {
		t.counts = make(map[int]int)
		t.lastReported = make(map[int]time.Time)
	}
	t.counts[kind]++
	last := t.lastReported[kind]
	if last.IsZero() || now.Sub(last) >= webView2FailureReportCooldown {
		t.lastReported[kind] = now
		return t.counts[kind], true
	}
	return t.counts[kind], false
}

func newCrashCaptureLogger(app *App) logger.Logger {
	return &crashCaptureLogger{delegate: logger.NewDefaultLogger(), app: app}
}

func (l *crashCaptureLogger) Print(message string)   { l.delegate.Print(message) }
func (l *crashCaptureLogger) Trace(message string)   { l.delegate.Trace(message) }
func (l *crashCaptureLogger) Debug(message string)   { l.delegate.Debug(message) }
func (l *crashCaptureLogger) Info(message string)    { l.delegate.Info(message) }
func (l *crashCaptureLogger) Warning(message string) { l.delegate.Warning(message) }
func (l *crashCaptureLogger) Fatal(message string) {
	l.captureWebView2Failure(message)
	l.delegate.Fatal(message)
}
func (l *crashCaptureLogger) Error(message string) {
	l.captureWebView2Failure(message)
	l.delegate.Error(message)
}

func (l *crashCaptureLogger) captureWebView2Failure(message string) {
	if goruntime.GOOS != "windows" || webView2NativeObserverInstalled() || l.app == nil || !l.app.diagnosticsTelemetry {
		return
	}
	kind, ok := parseWebView2ProcessFailure(message)
	if !ok {
		return
	}
	l.app.recordDiagnosticMetric("desktop_web_runtime_failure", "webview2."+webView2ProcessKindBucket(kind)+".unknown")
	occurrence, shouldReport := l.webView2Failures.observe(kind, time.Now())
	if !shouldReport {
		return
	}
	report := webView2ProcessFailureReportWithContext(kind, occurrence, webView2RuntimeVersion())
	_ = writePendingReport(report, true)
}

func parseWebView2ProcessFailure(message string) (int, bool) {
	const marker = "Process failed with kind "
	index := strings.LastIndex(message, marker)
	if index < 0 {
		return 0, false
	}
	value := strings.TrimSpace(message[index+len(marker):])
	kind, err := strconv.Atoi(value)
	return kind, err == nil && kind >= 0 && kind <= 9
}

func webView2ProcessKindBucket(kind int) string {
	names := []string{
		"browser_process_exited",
		"render_process_exited",
		"render_process_unresponsive",
		"frame_render_process_exited",
		"utility_process_exited",
		"sandbox_helper_process_exited",
		"gpu_process_exited",
		"ppapi_plugin_process_exited",
		"ppapi_broker_process_exited",
		"unknown_process_exited",
	}
	if kind < 0 || kind >= len(names) {
		return "unknown"
	}
	return names[kind]
}

func webView2ProcessFailureReportWithContext(kind, occurrence int, runtimeVersion string) crashReport {
	bucket := webView2ProcessKindBucket(kind)
	reportKind := "performance"
	if kind == 0 {
		reportKind = "crash"
	}
	report := baseCrashReport(reportKind)
	report.SchemaVersion = 3
	report.Source = "web.runtime.fallback"
	report.Label = "windows.webview2.process_failed"
	report.ErrorType = "WebView2ProcessFailed"
	report.ErrorMessage = sanitizeCrashText(fmt.Sprintf("WebView2 process failed: %s.", bucket), maxCrashFieldBytes)
	report.TopFrame = "webview2.process." + bucket
	report.FingerprintHint = "web.runtime.webview2." + bucket + ".unknown.unknown"
	report.OccurredAt = time.Now().UTC().Format(time.RFC3339)
	runtimeVersion = sanitizeCrashField(runtimeVersion, 128)
	if runtimeVersion == "" {
		runtimeVersion = "unknown"
	}
	gpuMode := "enabled"
	if windowsWebview2GPUDisabled() {
		gpuMode = "disabled"
	}
	report.WebRuntime = &webRuntimeDiagnostic{
		Engine: "webview2", Kind: bucket, Reason: "unknown",
		RuntimeVersion: runtimeVersion, GPUMode: gpuMode, Recovery: webView2RecoveryNotApplicable,
	}
	report.Message = sanitizeCrashText(fmt.Sprintf(`[windows.webview2.process_failed]

WebView2 reported a failed child process.

process kind: %s
kind code: %d
runtime version: %s
same-process occurrence: %d
exit code: unavailable from the current Wails ProcessFailed callback
GPU mode: %s`, bucket, kind, runtimeVersion, occurrence, gpuMode), maxCrashDetailBytes)
	return report
}
