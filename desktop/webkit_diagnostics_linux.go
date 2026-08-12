//go:build linux && cgo

package main

/*
#cgo !webkit2_41 pkg-config: gtk+-3.0 webkit2gtk-4.0
#cgo webkit2_41 pkg-config: gtk+-3.0 webkit2gtk-4.1
#include "webkit_diagnostics_linux.h"
*/
import "C"

import (
	"fmt"
	"sync"
	"sync/atomic"
)

var webKitObserverState = struct {
	sync.Once
	events  chan webKitNativeEvent
	dropped atomic.Uint64
}{events: make(chan webKitNativeEvent, 32)}

// observeWebKitNativeEvent is a no-op in production. The tagged native smoke
// replaces it during process startup so an actual WebKit termination can drive
// the same callback and recovery state machine without polling.
var observeWebKitNativeEvent = func(webKitNativeEvent) {}

func installWebKitProcessObserver(app *App, enabled bool) {
	if app == nil || !enabled || !app.diagnosticsOwner {
		return
	}
	webKitObserverState.Do(func() {
		go func() {
			for event := range webKitObserverState.events {
				report, outcome, failureBucket := webKitNativeFailureReport(event)
				_ = writePendingReport(report, true)
				app.recordDiagnosticMetric("desktop_web_runtime_failure", failureBucket)
				app.recordDiagnosticMetric("desktop_web_runtime_outcome", outcome)
				recordDroppedWebRuntimeEvents(app, "webkitgtk", &webKitObserverState.dropped)
			}
		}()
		C.reasonix_install_webkit_observer()
	})
}

//export reasonixWebKitRuntimeReady
func reasonixWebKitRuntimeReady(major, minor, micro C.int, gpuMode C.int) {
	mode := "unknown"
	switch int(gpuMode) {
	case 0:
		mode = "always"
	case 1:
		mode = "disabled"
	case 2:
		mode = "on_demand"
	}
	publishWebRuntimeContext(webRuntimeContext{
		Engine:         "webkitgtk",
		RuntimeVersion: fmt.Sprintf("%d.%d.%d", int(major), int(minor), int(micro)),
		GPUMode:        mode,
	})
}

//export reasonixWebKitProcessTerminated
func reasonixWebKitProcessTerminated(reason, recovery C.int, generation C.ulonglong) {
	event := webKitNativeEvent{
		reason: int(reason), recovery: int(recovery), generation: uint64(generation),
		runtimeContext: webRuntimeContextForTelemetry(0),
	}
	observeWebKitNativeEvent(event)
	select {
	case webKitObserverState.events <- event:
	default:
		// Keep the GTK callback bounded; the background consumer reports queue
		// pressure without making this callback touch disk or network.
		webKitObserverState.dropped.Add(1)
	}
}
