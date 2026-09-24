//go:build !debugpprof

package main

import "testing"

// The debugpprof diagnostics compile out of ordinary builds, so these tests pin
// the no-op contract the production binary links against. CI must also build
// and test the tagged path — `go build -tags debugpprof ./...` and
// `go test -tags debugpprof` — to catch a call site that drifts from the tagged
// definitions in pprof_debug.go.
func TestDebugTickPhaseNoopIsCallable(t *testing.T) {
	done := debugTickPhase("test:phase")
	done()
	// The returned closure is a deferred timer in production; it must tolerate
	// being called more than once without panicking.
	done()
}

func TestDebugNoteNoopAcceptsArgs(t *testing.T) {
	debugNote("test:note", "key", "value", "n", 1)
}
