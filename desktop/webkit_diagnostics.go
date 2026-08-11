package main

import (
	"fmt"
	"strings"
	"time"
)

const (
	webKitRecoveryNotApplicable = 0
	webKitRecoverySucceeded     = 1
	webKitRecoveryFailed        = 2
)

type webKitNativeEvent struct {
	reason         int
	recovery       int
	generation     uint64
	runtimeContext webRuntimeContext
}

func webKitReasonBucket(reason int) string {
	switch reason {
	case 0:
		return "crashed"
	case 1:
		return "out_of_memory"
	case 2:
		return "terminated_by_api"
	default:
		return "unknown"
	}
}

func webKitRecoveryBucket(recovery int) string {
	switch recovery {
	case webKitRecoverySucceeded:
		return webView2RecoverySucceeded
	case webKitRecoveryFailed:
		return webView2RecoveryFailed
	default:
		return webView2RecoveryNotApplicable
	}
}

func webKitNativeFailureReport(event webKitNativeEvent) (crashReport, string, string) {
	// The native state machine owns generation matching; retaining it in the
	// queued value prevents callbacks from consulting mutable native state.
	_ = event.generation
	runtimeContext := event.runtimeContext
	reason := webKitReasonBucket(event.reason)
	recovery := webKitRecoveryBucket(event.recovery)
	reportKind, outcome := "performance", "degraded"
	switch recovery {
	case webView2RecoverySucceeded:
		outcome = "recovered"
	case webView2RecoveryFailed:
		reportKind, outcome = "exception", "recovery_failed"
	}
	runtimeVersion := sanitizeCrashField(runtimeContext.RuntimeVersion, 128)
	if runtimeVersion == "" {
		runtimeVersion = "unknown"
	}
	gpuMode := sanitizeCrashField(runtimeContext.GPUMode, 32)
	if gpuMode == "" {
		gpuMode = "unknown"
	}
	diagnostic := &webRuntimeDiagnostic{
		Engine: "webkitgtk", Kind: "web_process", Reason: reason,
		RuntimeVersion: runtimeVersion, GPUMode: gpuMode, Recovery: recovery,
	}
	report := baseCrashReport(reportKind)
	report.SchemaVersion = 3
	report.Source = "web.runtime.native"
	report.Label = "linux.webkitgtk.web_process_terminated"
	report.ErrorType = "WebKitWebProcessTerminated"
	report.ErrorMessage = sanitizeCrashText(fmt.Sprintf("WebKitGTK web process: %s (%s).", reason, outcome), maxCrashFieldBytes)
	report.TopFrame = "webkitgtk.process.web_process"
	report.FingerprintHint = strings.Join([]string{"web.runtime", "webkitgtk", "web_process", reason, "unknown"}, ".")
	report.OccurredAt = time.Now().UTC().Format(time.RFC3339)
	report.WebRuntime = diagnostic
	report.Message = sanitizeCrashText(fmt.Sprintf(`[linux.webkitgtk.web_process_terminated]

WebKitGTK reported an abnormal web process termination.

reason: %s
exit code: unavailable
runtime version: %s
GPU mode: %s
recovery: %s`, reason, runtimeVersion, gpuMode, recovery), maxCrashDetailBytes)
	return report, outcome, strings.Join([]string{"webkitgtk", "web_process", reason}, ".")
}
