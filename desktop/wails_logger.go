package main

import (
	"fmt"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/logger"
)

type crashCaptureLogger struct {
	delegate logger.Logger
	app      *App
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
	if goruntime.GOOS != "windows" {
		return
	}
	kind, ok := parseWebView2ProcessFailure(message)
	if !ok {
		return
	}
	report := webView2ProcessFailureReport(kind)
	_ = writePendingReport(report, true)
	if l.app != nil {
		l.app.recordDiagnosticMetric("desktop_webview2_failure", webView2ProcessKindBucket(kind))
	}
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

func webView2ProcessFailureReport(kind int) crashReport {
	bucket := webView2ProcessKindBucket(kind)
	report := baseCrashReport("crash")
	report.SchemaVersion = 2
	report.Source = "webview2.process"
	report.Label = "windows.webview2.process_failed"
	report.ErrorType = "WebView2ProcessFailed"
	report.ErrorMessage = sanitizeCrashText(fmt.Sprintf("WebView2 process failed: %s.", bucket), maxCrashFieldBytes)
	report.TopFrame = "webview2.process." + bucket
	report.FingerprintHint = "windows.webview2." + bucket
	report.OccurredAt = time.Now().UTC().Format(time.RFC3339)
	report.Message = sanitizeCrashText(fmt.Sprintf(`[windows.webview2.process_failed]

WebView2 reported a failed child process.

process kind: %s
kind code: %d`, bucket, kind), maxCrashDetailBytes)
	return report
}
