package sessioncatalog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
)

func newRepairPublicationTestCatalog(t *testing.T, now *time.Time) (*Catalog, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.jsonl")
	catalog, err := Open(context.Background(), Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true,
		Now: func() time.Time { return *now },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if _, err := catalog.upsertSessionsWithNotification(context.Background(), []SessionRecord{{
		Path: path, Directory: dir, Scope: "global", TurnsState: TurnsUnknown, Health: HealthOK,
	}}, nil, "seed", false, upsertDirectoryProjection); err != nil {
		t.Fatal(err)
	}
	catalog.testRepairSessionHook = func(context.Context, string) (agent.SessionListingRepairResult, error) {
		return agent.SessionListingRepairResult{Status: agent.SessionListingRepairApplied, Preview: "ok", Turns: 1}, nil
	}
	return catalog, path
}

func TestRepairBatchPublicationFailuresDoNotPublishPartialState(t *testing.T) {
	for _, stage := range []string{"begin", "update", "revision", "commit"} {
		t.Run(stage, func(t *testing.T) {
			now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
			catalog, path := newRepairPublicationTestCatalog(t, &now)
			beforeRevision := catalog.revision.Load()
			catalog.testRepairBatchError = func(got string) error {
				if got == stage {
					return errors.New("injected " + stage)
				}
				return nil
			}
			catalog.runRepairWave(context.Background())
			var state, turnsState string
			var retryAt int64
			if err := catalog.db.QueryRow(`SELECT repair_state,repair_retry_at,turns_state FROM catalog_sessions WHERE path_key=?`,
				catalog.pathKey(path)).Scan(&state, &retryAt, &turnsState); err != nil {
				t.Fatal(err)
			}
			if state != "active" || turnsState != string(TurnsUnknown) || retryAt != now.Add(30*time.Second).UnixMilli() {
				t.Fatalf("row after %s failure = %s/%s/%d", stage, state, turnsState, retryAt)
			}
			if catalog.revision.Load() != beforeRevision {
				t.Fatalf("revision published after %s failure", stage)
			}
		})
	}
}

func TestExpiredActiveRepairReclaimsWithExponentialLeaseInSameProcess(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	catalog, path := newRepairPublicationTestCatalog(t, &now)
	failures := 2
	catalog.testRepairBatchError = func(stage string) error {
		if stage == "commit" && failures > 0 {
			failures--
			return errors.New("injected commit")
		}
		return nil
	}
	catalog.runRepairWave(context.Background())
	now = now.Add(30 * time.Second)
	catalog.runRepairWave(context.Background())
	now = now.Add(59 * time.Second)
	catalog.runRepairWave(context.Background())
	var state string
	if err := catalog.db.QueryRow(`SELECT repair_state FROM catalog_sessions WHERE path_key=?`, catalog.pathKey(path)).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "active" {
		t.Fatalf("repair ran before 60-second lease: %s", state)
	}
	now = now.Add(time.Second)
	catalog.runRepairWave(context.Background())
	if err := catalog.db.QueryRow(`SELECT repair_state FROM catalog_sessions WHERE path_key=?`, catalog.pathKey(path)).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "complete" {
		t.Fatalf("same-process reclaimed repair = %s, want complete", state)
	}
}
