//go:build debugpprof

package main

import "testing"

// debugLogPath only exists under the debugpprof tag (pprof_debug.go). Referencing
// it here is a compile gate: if the tagged file is dropped or renamed, this test
// build fails instead of the diagnostics silently disappearing.
func TestDebugLogPathUnderTag(t *testing.T) {
	if path := debugLogPath(); path == "" {
		t.Fatal("debugLogPath returned an empty path")
	}
}
