package update

import (
	"strings"
	"testing"
)

func TestParseHelperPhaseLine(t *testing.T) {
	phase, ok := parseHelperPhaseLine(debHelperPhasePrefix + "installing")
	if !ok || phase != "installing" {
		t.Fatalf("got %q %v", phase, ok)
	}
	if _, ok := parseHelperPhaseLine("E: Could not get lock"); ok {
		t.Fatal("apt noise must not parse as phase")
	}
	if _, ok := parseHelperPhaseLine(debHelperPhasePrefix); ok {
		t.Fatal("empty phase must not parse")
	}
}

// An install error reaches a user, so it must not disclose where files live.
func TestHelperErrorMessageStripsPaths(t *testing.T) {
	got := helperErrorMessage(nil, []byte("dpkg: error processing /var/cache/x.deb\n"))
	if got == "" || strings.Contains(got, "/var/cache") {
		t.Fatalf("helperErrorMessage leaked a path: %q", got)
	}
}

// A helper that exits 0 while reporting ok:false has failed; the exit code did
// not say so, and treating it as success would publish a version never installed.
func TestParseHelperFailureReadsStructuredResult(t *testing.T) {
	msg, code := parseHelperFailure([]byte(`{"ok":false,"error":"apt is busy","code":"package_manager_busy"}`))
	if msg != "apt is busy" || code != "package_manager_busy" {
		t.Fatalf("parseHelperFailure = %q, %q", msg, code)
	}
	if msg, _ := parseHelperFailure([]byte(`{"ok":true}`)); msg != "" {
		t.Fatalf("a successful result must report no failure, got %q", msg)
	}
}
