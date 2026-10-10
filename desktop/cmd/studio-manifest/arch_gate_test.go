package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/platform/update"
)

// archMap is desktop/electron/arch.json: what the packager builds for, and what
// each of those is called in a release.
type archMap struct {
	Map map[string]string   `json:"map"`
	FPM map[string][]string `json:"fpm"`
}

func loadArchMap(t *testing.T) archMap {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "electron", "arch.json"))
	if err != nil {
		t.Fatalf("arch.json: %v", err)
	}
	var m archMap
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("arch.json: %v", err)
	}
	if len(m.Map) == 0 {
		t.Fatal("arch.json maps nothing")
	}
	return m
}

// The packager and the updater name architectures differently, and only one of
// them is right about what a running binary calls itself. Every arch the
// packager can build has to land on exactly one platform key, or the release
// carries a file describing a platform nobody asks for — an update silently
// never offered.
func TestEveryPackagedArchResolvesToOnePlatformKey(t *testing.T) {
	m := loadArchMap(t)

	seen := map[string]string{}
	for from, to := range m.Map {
		if first, dup := seen[to]; dup {
			t.Errorf("%s and %s both release as %s; the second build overwrites the first", first, from, to)
		}
		seen[to] = from

		if to == "universal" {
			// One archive carries both slices, so it answers to both keys. This
			// is the only arch that is not one platform, and it is macOS-only.
			got := macArchiveKeys(artifactPrefix + "darwin-universal.zip")
			want := []string{update.PlatformKey("darwin", "amd64"), update.PlatformKey("darwin", "arm64")}
			if !slices.Equal(got, want) {
				t.Errorf("universal darwin archive = %v, want %v", got, want)
			}
			continue
		}

		if got := macArchiveKeys(artifactPrefix + "darwin-" + to + ".zip"); !slices.Equal(got, []string{update.PlatformKey("darwin", to)}) {
			t.Errorf("darwin %s archive = %v", to, got)
		}
		if got, ok := windowsInstallerKey(artifactPrefix + "windows-" + to + "-installer.exe"); !ok || got != update.PlatformKey("windows", to) {
			t.Errorf("windows %s installer = %q (%v)", to, got, ok)
		}
		if got, ok := nativePackageKey(artifactPrefix + "linux-" + to + ".deb"); !ok || got != update.PlatformKey("linux", to) {
			t.Errorf("linux %s package = %q (%v)", to, got, ok)
		}
	}
}

// Why the rename has to happen in the packager rather than here: the parsers
// accept the packager's own spelling perfectly well, and produce a key that is
// simply never asked for. Nothing downstream can notice, which is what makes an
// unmapped arch a silent release rather than a failed one.
func TestThePackagerSpellingResolvesToAKeyNobodyAsksFor(t *testing.T) {
	m := loadArchMap(t)
	canonical := map[string]bool{}
	for _, to := range m.Map {
		canonical[to] = true
	}
	for from, to := range m.Map {
		if from == to {
			continue
		}
		got, ok := windowsInstallerKey(artifactPrefix + "windows-" + from + "-installer.exe")
		if !ok {
			t.Fatalf("%s: the parser rejected it, so the risk this pins is gone — revisit the gate", from)
		}
		if canonical[got[len("windows-"):]] {
			t.Errorf("%s resolved to a canonical platform %q; the mapping is no longer load-bearing", from, got)
		}
	}
}

// fpm names an .rpm for the architecture its own way, and the parser accepts
// that name too: it keys a package nobody asks for. The packager hook renames
// these spellings, so every one must be a non-canonical key here.
func TestFPMSpellingOfAnRPMIsNotAReleaseKey(t *testing.T) {
	m := loadArchMap(t)
	if len(m.FPM) == 0 {
		t.Fatal("arch.json lists no fpm spellings")
	}
	canonical := map[string]bool{}
	for _, to := range m.Map {
		canonical[to] = true
	}
	for from, spellings := range m.FPM {
		to, ok := m.Map[from]
		if !ok {
			t.Errorf("fpm spells %s, which map does not release", from)
			continue
		}
		if got, ok := rpmPackageKey(artifactPrefix + "linux-" + to + ".rpm"); !ok || got != update.RPMPackageKey("linux", to) {
			t.Errorf("linux %s rpm = %q (%v)", to, got, ok)
		}
		for _, spelled := range spellings {
			got, ok := rpmPackageKey(artifactPrefix + "linux-" + spelled + ".rpm")
			if !ok || canonical[strings.TrimSuffix(strings.TrimPrefix(got, "linux-"), "-rpm")] {
				t.Errorf("%s resolved to %q (%v); the rename is no longer load-bearing", spelled, got, ok)
			}
		}
	}
}
