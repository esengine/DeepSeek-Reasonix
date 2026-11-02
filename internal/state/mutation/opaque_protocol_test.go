package mutation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/filelock"
)

func TestOpaqueWritePublishesExplicitVersionAndPreservesLegacyDirectoryMeaning(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	c := testCoordinator(t, root, state)
	for _, pool := range []bool{false, true} {
		request := opaqueIntent()
		if pool {
			request.Capacities = []Capacity{capacity("model")}
		}
		lease, err := c.Acquire(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(c.recordPath(lease.ID))
		if err != nil {
			t.Fatal(err)
		}
		var r record
		if err := json.Unmarshal(raw, &r); err != nil || r.Version != 3 || r.Effect != OpaqueWrite {
			t.Fatalf("opaque semantics published under an old version: %s %v", raw, err)
		}
		lease.End(NotExecuted)
	}
	legacy, err := c.Acquire(t.Context(), Intent{Effect: Workspace})
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.End(NotExecuted)
	other := testCoordinator(t, t.TempDir(), state)
	named, err := other.TryAcquire(t.Context(), intent("unrelated"))
	if err != nil {
		t.Fatalf("legacy directory silently became global: %v", err)
	}
	named.End(NotExecuted)
}

func TestOpaqueUnsupportedProtocolPreservesLiveAndDeadRecords(t *testing.T) {
	for _, scenario := range []string{"old_version", "future_version", "other_effect", "no_paths", "unknown_field"} {
		t.Run(scenario, func(t *testing.T) {
			c := testCoordinator(t, t.TempDir(), t.TempDir())
			r := record{Version: 3, ID: "opaque", Order: 1, Active: true, Effect: OpaqueWrite,
				Workspace: c.root, Paths: []string{c.root}}
			switch scenario {
			case "old_version":
				r.Version = 1
			case "future_version":
				r.Version = 4
			case "other_effect":
				r.Effect = NamedPaths
			case "no_paths":
				r.Paths = nil
			}
			raw, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "unknown_field" {
				raw = append(raw[:len(raw)-1], []byte(`,"future":true}`)...)
			}
			if err := os.WriteFile(c.recordPath(r.ID), raw, 0600); err != nil {
				t.Fatal(err)
			}
			release, err := filelock.TryAcquire(c.livePath(r.ID))
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			for _, phase := range []string{"live", "dead"} {
				if phase == "dead" {
					release()
				}
				lease, err := c.TryAcquire(t.Context(), intent(filepath.Join(c.root, "new")))
				if lease != nil {
					lease.End(NotExecuted)
				}
				if lease != nil || !errors.Is(err, ErrProtocol) {
					t.Errorf("%s invalid record admitted mutation: %v", phase, err)
				}
				actual, err := os.ReadFile(c.recordPath(r.ID))
				if err != nil || string(actual) != string(raw) {
					t.Errorf("%s invalid record changed: %q %v", phase, actual, err)
				}
			}
		})
	}
}
