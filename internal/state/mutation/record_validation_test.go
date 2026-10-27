package mutation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/filelock"
)

func TestUnsupportedRecordCannotBeGrantedOrScavenged(t *testing.T) {
	for _, live := range []bool{false, true} {
		for _, shape := range []string{"future_effect", "read_only_paths", "external_paths", "extension", "capacity_extension"} {
			t.Run(shape+map[bool]string{false: "/dead", true: "/live"}[live], func(t *testing.T) {
				root, state := t.TempDir(), t.TempDir()
				c := testCoordinator(t, root, state)
				r := record{Version: 1, ID: "unsupported", Order: 1, Active: true,
					Paths: []string{filepath.Join(c.root, "reserved")}, Effect: NamedPaths}
				var fields map[string]any
				raw, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(raw, &fields); err != nil {
					t.Fatal(err)
				}
				switch shape {
				case "future_effect":
					fields["effect"] = "future_resource"
				case "read_only_paths":
					fields["effect"] = ReadOnly
				case "external_paths":
					fields["effect"] = External
				case "extension":
					fields["future_resources"] = []string{"retained"}
				case "capacity_extension":
					fields["version"], fields["effect"], fields["paths"] = 2, CapacityOnly, []string{}
					fields["workspace"] = c.root
					fields["capacities"] = []any{map[string]any{"id": "pool", "limit": 1, "units": 1, "future_units": 2}}
				}
				raw, err = json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(c.recordPath(r.ID), raw, 0600); err != nil {
					t.Fatal(err)
				}
				if live {
					release, err := filelock.TryAcquire(c.livePath(r.ID))
					if err != nil {
						t.Fatal(err)
					}
					defer release()
				}
				for _, request := range []Intent{intent(filepath.Join(c.root, "unrelated")), {
					Effect: CapacityOnly, Capacities: []Capacity{{ID: "different", Limit: 1, Units: 1}},
				}} {
					lease, err := c.TryAcquire(t.Context(), request)
					if lease != nil {
						lease.End(NotExecuted)
					}
					if lease != nil || !errors.Is(err, ErrProtocol) {
						t.Errorf("unsupported registry admitted %s: lease=%v error=%v", request.Effect, lease != nil, err)
					}
				}
				if actual, err := os.ReadFile(c.recordPath(r.ID)); err != nil || string(actual) != string(raw) {
					t.Errorf("unsupported record changed: %q, %v", actual, err)
				}
				entries, err := filepath.Glob(filepath.Join(c.dir, "*.json"))
				if err != nil || len(entries) != 1 || entries[0] != c.recordPath(r.ID) {
					t.Errorf("failed acquisition left publication: %v, %v", entries, err)
				}
				if live {
					release, err := filelock.TryAcquire(c.livePath(r.ID))
					if release != nil {
						release()
					}
					if !errors.Is(err, filelock.ErrHeld) {
						t.Errorf("refusal released another owner: %v", err)
					}
				}
			})
		}
	}
}

func TestKnownRecordShapesRemainReadableAndReclaimable(t *testing.T) {
	for _, version := range []int{1, 2} {
		for _, effect := range []Effect{"", NamedPaths, Workspace, GitMetadata, CapacityOnly} {
			if version == 1 && effect == CapacityOnly || version == 2 && effect == "" {
				continue
			}
			t.Run(string(effect)+string(rune('0'+version)), func(t *testing.T) {
				c := testCoordinator(t, t.TempDir(), t.TempDir())
				r := record{Version: version, ID: "known", Order: 1, Paths: []string{c.root}, Effect: effect}
				if version == 2 {
					r.Workspace = c.root
					r.Capacities = []Capacity{{ID: "pool", Limit: 1, Units: 1}}
				}
				if effect == CapacityOnly {
					r.Paths = nil
				}
				if err := c.save(r); err != nil {
					t.Fatal(err)
				}
				release, err := filelock.TryAcquire(c.livePath(r.ID))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(release)
				if records, err := c.records(); err != nil || len(records) != 1 || records[0].ID != r.ID {
					t.Fatalf("known live record rejected: %+v, %v", records, err)
				}
				release()
				lease, err := c.TryAcquire(t.Context(), intent(filepath.Join(c.root, "file")))
				if err != nil {
					t.Fatal(err)
				}
				lease.End(NotExecuted)
				if _, err := os.Stat(c.recordPath(r.ID)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("known dead owner not reclaimed: %v", err)
				}
			})
		}
	}
}
