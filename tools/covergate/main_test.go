package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/testenv"
)

func TestPackagesForMapsGlobsOntoPackages(t *testing.T) {
	got := packagesFor([]string{
		"internal/permission/**",
		"internal/control/approval.go",
		"internal/permission/**",
	})
	want := []string{"./internal/control/...", "./internal/permission/..."}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packagesFor = %v, want %v", got, want)
	}
	// A go package pattern is slash-separated on every host. Only Windows ever
	// said otherwise, and it said it twice: an unmatchable pattern, and an order
	// that changed with it because a backslash outranks a slash.
	for _, pkg := range got {
		if strings.ContainsRune(pkg, '\\') {
			t.Errorf("package pattern %q carries a host separator", pkg)
		}
	}
}

func TestMatchesGlobHandlesSubtreesAndExactFiles(t *testing.T) {
	for _, tc := range []struct {
		glob, file string
		want       bool
	}{
		{"internal/permission/**", "internal/permission/rule.go", true},
		{"internal/permission/**", "internal/permissionx/rule.go", false},
		{"internal/control/approval.go", "internal/control/approval.go", true},
		{"internal/control/approval.go", "internal/control/controller.go", false},
	} {
		if got := matchesGlob(tc.glob, tc.file); got != tc.want {
			t.Errorf("matchesGlob(%q, %q) = %v, want %v", tc.glob, tc.file, got, tc.want)
		}
	}
}

// A file-level declaration must be measured as that file, not as its package:
// approval.go sits in a package the project did not call sensitive.
func TestFromProfileAggregatesPerDeclaredPath(t *testing.T) {
	dir := testenv.TempDir(t)
	profile := filepath.Join(dir, "c.out")
	body := "mode: set\n" +
		"reasonix/internal/control/approval.go:1.1,2.2 4 1\n" +
		"reasonix/internal/control/approval.go:3.1,4.2 4 0\n" +
		"reasonix/internal/control/controller.go:1.1,2.2 100 0\n" +
		"reasonix/internal/permission/rule.go:1.1,2.2 3 1\n"
	if err := os.WriteFile(profile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _, err := fromProfile(profile, []string{"internal/control/approval.go", "internal/permission/**"})
	if err != nil {
		t.Fatal(err)
	}
	if got["internal/control/approval.go"] != 50.0 {
		t.Errorf("approval.go = %v, want 50 (the file, not its package)", got["internal/control/approval.go"])
	}
	if got["internal/permission/**"] != 100.0 {
		t.Errorf("permission = %v, want 100", got["internal/permission/**"])
	}
}

// The recorded floor and the value compared against it must be the same number,
// or a freshly written baseline fails against itself.
func TestMeasuredValuesAreRoundedOnce(t *testing.T) {
	dir := testenv.TempDir(t)
	profile := filepath.Join(dir, "c.out")
	if err := os.WriteFile(profile, []byte("mode: set\nreasonix/internal/x/a.go:1.1,2.2 3 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _, err := fromProfile(profile, []string{"internal/x/**"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "baseline.json")
	if err := write(path, got); err != nil {
		t.Fatal(err)
	}
	loaded, err := load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded["internal/x/**"] != got["internal/x/**"] {
		t.Fatalf("round trip changed the floor: %v -> %v", got["internal/x/**"], loaded["internal/x/**"])
	}
	if d := drops(loaded, got); len(d) != 0 {
		t.Fatalf("a freshly written baseline reports a drop against itself: %v", d)
	}
}

func TestDropsReportsOnlyLoweredFloors(t *testing.T) {
	prev := map[string]float64{"a": 80, "b": 50}
	next := map[string]float64{"a": 79.9, "b": 60}
	got := drops(prev, next)
	if len(got) != 1 || got[0] != "a: 80.0% -> 79.9%" {
		t.Fatalf("drops = %v, want only the lowered floor", got)
	}
}

// The gate reads the same `sensitive:` bullets the runtime reads, so declaring
// a path once both hardens its review and holds its coverage.
func TestSensitiveGlobsComeFromTheProjectHostChecks(t *testing.T) {
	globs, err := sensitiveGlobs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(globs) == 0 {
		t.Fatal("no sensitive paths read from the project's host checks")
	}
	found := false
	for _, g := range globs {
		if g == "internal/shellsafe/**" {
			found = true
		}
	}
	if !found {
		t.Fatalf("declared sensitive paths not read: %v", globs)
	}
}

// A floor that has slipped names the lines, largest first. A percentage alone
// leaves the reader measuring a package by hand on a platform they may not be
// on, which is how a shortfall on one GOOS goes unfixed.
func TestUncoveredBlocksAreReportedLargestFirst(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "c.out")
	body := "mode: set\n" +
		"reasonix/internal/x/a.go:1.1,2.2 3 1\n" +
		"reasonix/internal/x/a.go:4.1,5.2 2 0\n" +
		"reasonix/internal/x/b.go:7.1,9.2 5 0\n"
	if err := os.WriteFile(profile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, gaps, err := fromProfile(profile, []string{"internal/x/**"})
	if err != nil {
		t.Fatal(err)
	}
	got := gaps["internal/x/**"]
	if len(got) != 2 {
		t.Fatalf("gaps = %+v, want the two blocks nothing executed", got)
	}
	if got[0].stmts != 5 || got[0].where != "internal/x/b.go:7.1,9.2" {
		t.Fatalf("first gap = %+v, want the largest one", got[0])
	}
	if n := len(topGaps(got, 1)); n != 1 {
		t.Fatalf("topGaps kept %d, want the cap honoured", n)
	}
}
