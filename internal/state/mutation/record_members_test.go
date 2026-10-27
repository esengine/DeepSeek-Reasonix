package mutation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/filelock"
)

func TestAmbiguousRecordMembersCannotGrantOrScavenge(t *testing.T) {
	for _, live := range []bool{false, true} {
		for _, shape := range []string{
			"version_duplicate", "version_case", "version_escaped_duplicate", "version_alias_only", "paths_duplicate",
			"active_duplicate", "effect_duplicate", "capacity_duplicate", "capacity_case",
		} {
			t.Run(shape+map[bool]string{false: "/dead", true: "/live"}[live], func(t *testing.T) {
				c := testCoordinator(t, t.TempDir(), t.TempDir())
				r := record{Version: 1, ID: "ambiguous", Order: 1, Active: true,
					Paths: []string{filepath.Join(c.root, "reserved")}, Effect: NamedPaths}
				if strings.HasPrefix(shape, "capacity_") {
					r.Version, r.Workspace = 2, c.root
					r.Capacities = []Capacity{{ID: "pool", Limit: 1, Units: 1}}
				}
				raw, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				text := string(raw)
				switch shape {
				case "version_duplicate":
					text = strings.Replace(text, `"version":1`, `"version":99,"version":1`, 1)
				case "version_case":
					text = strings.Replace(text, `"version":1`, `"version":99,"Version":1`, 1)
				case "version_escaped_duplicate":
					text = strings.Replace(text, `"version":1`, `"version":99,"\u0076ersion":1`, 1)
				case "version_alias_only":
					text = strings.Replace(text, `"version":1`, `"Version":1`, 1)
				case "paths_duplicate":
					text = strings.Replace(text, `"paths":`, `"paths":[],"paths":`, 1)
				case "active_duplicate":
					text = strings.Replace(text, `"active":true`, `"active":false,"active":true`, 1)
				case "effect_duplicate":
					text = strings.Replace(text, `"effect":"named_paths"`, `"effect":"future_effect","effect":"named_paths"`, 1)
				case "capacity_duplicate":
					text = strings.Replace(text, `"units":1`, `"units":2,"units":1`, 1)
				case "capacity_case":
					text = strings.Replace(text, `"units":1`, `"units":2,"Units":1`, 1)
				}
				if text == string(raw) {
					t.Fatal("fixture did not change record")
				}
				if err := os.WriteFile(c.recordPath(r.ID), []byte(text), 0600); err != nil {
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
						t.Errorf("ambiguous record allowed %s: lease=%v error=%v", request.Effect, lease != nil, err)
					}
				}
				if actual, err := os.ReadFile(c.recordPath(r.ID)); err != nil || string(actual) != text {
					t.Errorf("ambiguous record changed: %q, %v", actual, err)
				}
				entries, err := filepath.Glob(filepath.Join(c.dir, "*.json"))
				if err != nil || len(entries) != 1 || entries[0] != c.recordPath(r.ID) {
					t.Errorf("failed acquisition published a record: %v, %v", entries, err)
				}
				if live {
					release, err := filelock.TryAcquire(c.livePath(r.ID))
					if release != nil {
						release()
					}
					if !errors.Is(err, filelock.ErrHeld) {
						t.Errorf("refusal released original owner: %v", err)
					}
				}
			})
		}
	}
}

func TestRecordMembersRejectMalformedShapes(t *testing.T) {
	for _, input := range []string{
		`null`, `[]`, `1`, `{"version":1`, `{"version":1} {}`, `{"version":1} garbage`,
		`{"paths":[{"version":1}]}`, `{"capacities":{}}`, `{"capacities":[null]}`,
		`{"capacities":[{"id":"pool","units":1`, `{"unknown":true}`,
	} {
		var r record
		if err := decodeCoordinationRecord([]byte(input), &r); err == nil {
			t.Errorf("malformed record accepted: %s", input)
		}
	}
}

func TestRecordMembersPreserveUnambiguousJSON(t *testing.T) {
	for _, input := range []string{
		` { "id": "same", "version": 1, "paths": ["/file"], "order": 1 } `,
		`{"\u0076ersion":1,"paths":["/file"],"id":"same","order":1}`,
		`{"version":1,"paths":["/file"],"id":"same","order":1,"capacities":null}`,
		`{"version":2,"id":"same","paths":null,"effect":"capacity","workspace":"/root","order":1,"capacities":[{"units":1,"id":"pool","limit":1},{"limit":2,"units":1,"id":"other"}]}`,
	} {
		var r record
		if err := decodeCoordinationRecord([]byte(input), &r); err != nil {
			t.Errorf("unambiguous JSON rejected: %s: %v", input, err)
		}
	}
}
