package main

import "strings"

// emitRemoteEvent bridges a kernel callback to the frontend through the async
// emitter so a slow webview never blocks the kernel.
func (a *App) emitRemoteEvent(name string, payload any) {
	if a.remoteEventHook != nil {
		a.remoteEventHook(name, payload)
	}
	if strings.HasPrefix(name, "remote-tab:") && strings.HasSuffix(name, ":state") {
		a.emitRuntimeStateChanged()
	}
	ctx := a.bootContext()
	if ctx == nil {
		return
	}
	a.runtimeEvents.Emit(ctx, name, payload)
}
