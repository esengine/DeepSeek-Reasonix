package isolation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
)

func TestNewIsolationRecordIsInvisibleToLegacySweep(t *testing.T) {
	repo, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, repo, opened(t, repo))
	e, err := s.Begin(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(t.Context(), e) })
	legacy, err := filepath.Glob(filepath.Join(managed, "*", "entry.json"))
	if err != nil || len(legacy) != 0 {
		t.Fatalf("old sweeper can discover new result: %v %v", legacy, err)
	}
	if _, err := os.Stat(entryPath(e.Cand)); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownIsolationStateIsRetained(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	e, err := s.Begin(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(t.Context(), e) })
	write(t, e.Cand.WorkspaceRoot, "a.txt", "retained\n")
	if _, err := s.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(entryPath(e.Cand))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["state"] = "future_state"
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryPath(e.Cand), raw, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = s.Discard(t.Context(), e.ID)
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeUnsupportedState {
		t.Fatalf("unknown state discarded: %v", err)
	}
	s.SweepExpired(t.Context(), -time.Second)
	if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "retained\n" {
		t.Fatal(got)
	}
}

func TestLegacyMarkerRemainsReadable(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	e, err := s.Begin(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	write(t, e.Cand.WorkspaceRoot, "a.txt", "legacy\n")
	if _, err := s.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(filepath.Dir(e.Cand.WorktreeRoot), legacyEntryFile)
	if err := os.Rename(entryPath(e.Cand), oldPath); err != nil {
		t.Fatal(err)
	}
	e.legacy = true
	t.Cleanup(func() { s.Abort(t.Context(), e) })
	legacy, err := s.lookup(e.ID)
	if err != nil || !legacy.legacy {
		t.Fatalf("legacy marker: %+v %v", legacy, err)
	}
}

func TestLifecycleUpdatePreservesNestedUnknownFields(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	e, err := s.Begin(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(t.Context(), e) })
	write(t, e.Cand.WorkspaceRoot, "a.txt", "candidate\n")
	if _, err := s.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(entryPath(e.Cand))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["snap"].(map[string]any)["future_snapshot_field"] = "retained"
	fields["changes"].([]any)[0].(map[string]any)["future_change_field"] = "retained"
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryPath(e.Cand), raw, 0600); err != nil {
		t.Fatal(err)
	}
	e.State = Applying
	if err := persist(e); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(entryPath(e.Cand))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["snap"].(map[string]any)["future_snapshot_field"] != "retained" || fields["changes"].([]any)[0].(map[string]any)["future_change_field"] != "retained" {
		t.Fatal("lifecycle transition discarded nested unknown fields")
	}
}
