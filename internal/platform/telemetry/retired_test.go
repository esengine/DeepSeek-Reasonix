package telemetry

import (
	"testing"

	"reasonix/internal/base/testenv"
)

func TestRetiredReportsNothing(t *testing.T) {
	Retired = true
	t.Cleanup(func() { Retired = false })
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("REASONIX_TELEMETRY", "")
	t.Setenv("CI", "")

	if Enabled("on", "v2.61.0", true) {
		t.Fatal("a Retired reporter must not be enabled, even when asked for")
	}
	if !OptedOut() {
		t.Fatal("a Retired reporter must read as opted out for every other sender")
	}
	if r := Start(Options{Mode: "on", Version: "v2.61.0", Interactive: true, HomeDir: testenv.TempDir(t)}); r != nil {
		t.Fatal("a Retired reporter must not start")
	}
}
