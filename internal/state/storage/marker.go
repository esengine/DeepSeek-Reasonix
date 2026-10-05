// marker.go — what a relocated directory says about itself.
package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"reasonix/internal/contract/config"
)

// markerName is written at the top of a relocated root, never inside it.
const markerName = ".reasonix-root.json"

// Marker is a directory's claim to be one root's storage. Without it the
// [storage] table is the only record a relocation leaves, and that pointer can
// go — a config read from another Windows profile, a reinstall, a hand-edit —
// while the data it named sits untouched. A folder that says what it is can be
// pointed at again; one that says nothing is indistinguishable from a stranger's.
type Marker struct {
	Root config.RootID `json:"root"`
	// From is where the data was before, kept for diagnosis rather than for use.
	From    string    `json:"from,omitempty"`
	Written time.Time `json:"written"`
}

// ReadMarker answers what dir claims to be. Absent, unreadable, or naming a
// root this build does not declare all answer false: a marker is evidence, and
// missing evidence is not a claim.
func ReadMarker(dir string) (Marker, bool) {
	if dir == "" {
		return Marker{}, false
	}
	body, err := os.ReadFile(filepath.Join(dir, markerName))
	if err != nil {
		return Marker{}, false
	}
	var m Marker
	if err := json.Unmarshal(body, &m); err != nil {
		return Marker{}, false
	}
	if !slices.Contains(config.RootIDs(), m.Root) {
		return Marker{}, false
	}
	return m, true
}

// WriteMarker records that root now lives in dir — where the data is rather
// than beside the configuration, so losing one does not lose both. Written in
// place rather than replaced atomically: there is no earlier content to
// protect, and a half-written marker reads as no claim at all, which is the
// same answer as an absent one.
func WriteMarker(dir string, root config.RootID, from string) error {
	body, err := json.MarshalIndent(Marker{Root: root, From: from, Written: time.Now().UTC()}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, markerName), append(body, '\n'), 0o600)
}

// RecordRelocatedRoots marks the locations a configuration already chose and
// that carry no marker — every relocation performed before this file existed.
// A directory it cannot write is left alone: a marker is how the data is found
// again, never a condition of reading it.
func RecordRelocatedRoots() {
	// The rule the relocation importers follow: a run whose roots the
	// environment redirected did not ask for the production install to be
	// written into, and this writes into the directory the data lives in.
	if config.IsolatedHomeDir() != "" || config.IsolatedStateDir() != "" {
		return
	}
	for _, id := range config.RootIDs() {
		// Only a location someone chose. A root sitting where it falls back to
		// shares that directory with others, and a default home claiming to be
		// the state root is a claim nobody made.
		dir := config.RootConfiguredDir(id)
		if dir == "" || !isDirAt(dir) {
			continue
		}
		if _, ok := ReadMarker(dir); ok {
			continue
		}
		_ = WriteMarker(dir, id, "")
	}
}

// holdsRoot reports whether dir already holds this root's own data. Structure
// answers it: the marker names the root, or — for a root that declares what it
// owns — the folder holds those entries and nothing else. A root with sole
// occupancy and no marker cannot be told from a stranger's folder, so it is not
// claimed rather than guessed at.
func holdsRoot(root config.RootID, dir string, entries []os.DirEntry) bool {
	if m, ok := ReadMarker(dir); ok {
		return m.Root == root
	}
	owned := config.RootOwns(root)
	if len(owned) == 0 {
		return false
	}
	claimed := 0
	for _, entry := range entries {
		if entry.Name() == markerName {
			continue
		}
		if !slices.Contains(owned, entry.Name()) {
			return false
		}
		claimed++
	}
	return claimed > 0
}
