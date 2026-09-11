package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var literalEmitRe = regexp.MustCompile(`(?:emitRuntimeEvent|emitRemoteEvent|EventsEmit\([^,]+,|runtimeEvents\.Emit\([^,]+,)\s*"([^"]+)"`)

func desktopSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		sources[name] = string(data)
	}
	return sources
}

func TestHostEventsListIsSortedUniqueAndEmitted(t *testing.T) {
	if !slices.IsSorted(hostEventNames) {
		t.Fatalf("hostEventNames must be sorted: %v", hostEventNames)
	}
	if len(slices.Compact(slices.Clone(hostEventNames))) != len(hostEventNames) {
		t.Fatalf("hostEventNames has duplicates: %v", hostEventNames)
	}
	sources := desktopSources(t)
	for _, name := range hostEventNames {
		literal := `"` + name + `"`
		found := false
		for file, src := range sources {
			if file != "host_events.go" && strings.Contains(src, literal) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("hostEventNames lists %q but no desktop source emits that literal", name)
		}
	}
}

func TestHostEventsCoverLiteralEmitSites(t *testing.T) {
	sources := desktopSources(t)
	for file, src := range sources {
		for _, match := range literalEmitRe.FindAllStringSubmatch(src, -1) {
			name := match[1]
			if strings.Contains(name, "%") {
				continue
			}
			if !slices.Contains(hostEventNames, name) {
				t.Errorf("%s emits %q which hostEventNames does not list", file, name)
			}
		}
	}
}
