//go:build windows

package main

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/go-webview2/pkg/edge"
)

var webView2ObserverState = struct {
	sync.Once
	events        chan edge.ProcessFailedDiagnostic
	dropped       atomic.Uint64
	windowEvents  chan edge.WindowStateDiagnostic
	windowDropped atomic.Uint64
}{
	events:       make(chan edge.ProcessFailedDiagnostic, 32),
	windowEvents: make(chan edge.WindowStateDiagnostic, 32),
}

func installWebView2ProcessObserver(app *App) {
	if app == nil {
		return
	}
	nativeWebView2ObserverInstalled.Store(true)
	process := func(diagnostic edge.ProcessFailedDiagnostic) {
		event := webView2NativeEvent{
			Kind:                int(diagnostic.Kind),
			Reason:              int(diagnostic.Reason),
			ReasonAvailable:     diagnostic.ReasonAvailable,
			ExitCode:            diagnostic.ExitCode,
			ExitCodeAvailable:   diagnostic.ExitCodeAvailable,
			ProcessDescription:  diagnostic.ProcessDescription,
			FailureSourceModule: diagnostic.FailureSourceModule,
			Recovery:            diagnostic.Recovery,
		}
		if app.webView2Recovery != nil {
			app.webView2Recovery.nativeFailure(event)
		}
		if !app.diagnosticsTelemetry {
			return
		}
		report, outcome := webView2NativeFailureReport(event, webView2RuntimeVersion(), windowsWebview2GPUDisabled())
		_ = writePendingReport(report, true)
		if report.WebRuntime != nil {
			app.recordDiagnosticMetric("desktop_web_runtime_failure", "webview2."+report.WebRuntime.Kind+"."+report.WebRuntime.Reason)
		}
		app.recordDiagnosticMetric("desktop_web_runtime_outcome", outcome)
	}
	webView2ObserverState.Do(func() {
		go func() {
			for diagnostic := range webView2ObserverState.events {
				process(diagnostic)
				recordDroppedWebRuntimeEvents(app, "webview2", &webView2ObserverState.dropped)
			}
		}()
		go func() {
			for diagnostic := range webView2ObserverState.windowEvents {
				slog.Debug("WebView2 window synchronized",
					"clientWidth", diagnostic.ClientRect.Right-diagnostic.ClientRect.Left,
					"clientHeight", diagnostic.ClientRect.Bottom-diagnostic.ClientRect.Top,
					"windowLeft", diagnostic.WindowRect.Left,
					"windowTop", diagnostic.WindowRect.Top,
					"dpi", diagnostic.DPI,
					"monitorScale", diagnostic.Scale,
				)
				recordDroppedWebRuntimeEvents(app, "webview2.window_state", &webView2ObserverState.windowDropped)
			}
		}()
		edge.SetProcessFailedObserver(func(diagnostic edge.ProcessFailedDiagnostic) {
			if diagnostic.Kind == edge.COREWEBVIEW2_PROCESS_FAILED_KIND_BROWSER_PROCESS_EXITED {
				// Wails exits immediately after its callback. Preserve the fatal
				// report and outcome synchronously before control returns to Wails.
				process(diagnostic)
				return
			}
			select {
			case webView2ObserverState.events <- diagnostic:
			default:
				if diagnostic.Kind == edge.COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_EXITED ||
					diagnostic.Kind == edge.COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_UNRESPONSIVE {
					// GPU/utility storms may fill the diagnostic queue immediately
					// before the renderer also fails. Recovery is control-plane work:
					// never drop it merely because best-effort telemetry is congested.
					go process(diagnostic)
					return
				}
				// Keep the COM callback non-blocking and let the background consumer
				// expose queue pressure as an aggregate, content-free metric.
				webView2ObserverState.dropped.Add(1)
			}
		})
		edge.SetWindowStateObserver(func(diagnostic edge.WindowStateDiagnostic) {
			select {
			case webView2ObserverState.windowEvents <- diagnostic:
			default:
				webView2ObserverState.windowDropped.Add(1)
			}
		})
	})
}

func refreshWebRuntimeContext() {
	gpuMode := "enabled"
	if windowsWebview2GPUDisabled() {
		gpuMode = "disabled"
	}
	publishWebRuntimeContext(webRuntimeContext{
		Engine: "webview2", RuntimeVersion: webView2RuntimeVersion(), GPUMode: gpuMode,
	})
}
