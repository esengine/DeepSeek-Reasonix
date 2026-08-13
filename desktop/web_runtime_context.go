package main

import (
	"sync"
	"time"
)

type webRuntimeContext struct {
	Engine         string
	RuntimeVersion string
	GPUMode        string
}

var nativeWebRuntimeContext = struct {
	sync.RWMutex
	value webRuntimeContext
	ready chan struct{}
	once  sync.Once
}{ready: make(chan struct{})}

//nolint:unused // Called by the mutually exclusive Windows and Linux implementations.
func publishWebRuntimeContext(value webRuntimeContext) {
	value.Engine = sanitizeCrashField(value.Engine, 32)
	value.RuntimeVersion = sanitizeCrashField(value.RuntimeVersion, 128)
	value.GPUMode = sanitizeCrashField(value.GPUMode, 32)
	if value.Engine == "" {
		return
	}
	nativeWebRuntimeContext.Lock()
	nativeWebRuntimeContext.value = value
	nativeWebRuntimeContext.Unlock()
	nativeWebRuntimeContext.once.Do(func() { close(nativeWebRuntimeContext.ready) })
}

func webRuntimeContextForTelemetry(timeout time.Duration) webRuntimeContext {
	engine := platformWebRuntimeEngine()
	if engine == "" {
		return webRuntimeContext{}
	}
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-nativeWebRuntimeContext.ready:
		case <-timer.C:
		}
	}
	nativeWebRuntimeContext.RLock()
	value := nativeWebRuntimeContext.value
	nativeWebRuntimeContext.RUnlock()
	if value.Engine == "" {
		value = webRuntimeContext{Engine: engine, RuntimeVersion: "unknown", GPUMode: "unknown"}
	}
	return value
}
