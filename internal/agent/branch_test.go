package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

func TestBranchMetaCrossProcessReadModifyWrite(t *testing.T) {
	if os.Getenv("REASONIX_META_LOCK_HELPER") == "1" {
		path := os.Getenv("REASONIX_META_LOCK_PATH")
		unlock, err := LockSessionMetaPath(path)
		if err != nil {
			t.Fatal(err)
		}
		meta, err := EnsureBranchMetaLocked(path)
		if err != nil {
			unlock()
			t.Fatal(err)
		}
		meta.Name = "written-by-peer"
		if err := SaveBranchMetaPreserveUpdatedLocked(path, meta); err != nil {
			unlock()
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("REASONIX_META_READY"), []byte("ready"), 0o600); err != nil {
			unlock()
			t.Fatal(err)
		}
		for {
			if _, err := os.Stat(os.Getenv("REASONIX_META_RELEASE")); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		unlock()
		return
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := SaveBranchMeta(path, BranchMeta{ID: "session"}); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "ready")
	release := filepath.Join(dir, "release")
	cmd := exec.Command(os.Args[0], "-test.run", "^TestBranchMetaCrossProcessReadModifyWrite$")
	cmd.Env = append(os.Environ(),
		"REASONIX_META_LOCK_HELPER=1",
		"REASONIX_META_LOCK_PATH="+path,
		"REASONIX_META_READY="+ready,
		"REASONIX_META_RELEASE="+release,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("metadata helper did not acquire lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	done := make(chan error, 1)
	go func() {
		done <- UpdateBranchMeta(path, false, func(meta *BranchMeta) error {
			meta.CustomTitle = "written-after-peer"
			return nil
		})
	}()
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if meta.Name != "written-by-peer" || meta.CustomTitle != "written-after-peer" {
		t.Fatalf("cross-process update lost a field: %+v", meta)
	}
}

func TestBranchMetaIgnoresRetiredAutoRecoveryField(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "legacy.jsonl")
	metaPath := BranchMetaPath(sessionPath)
	legacy := `{"id":"legacy","name":"kept","recovery_checkpoint_enabled":false}`
	if err := os.WriteFile(metaPath, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy branch meta: %v", err)
	}

	meta, ok, err := LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if meta.Name != "kept" {
		t.Fatalf("name = %q, want kept", meta.Name)
	}
	if err := SaveBranchMeta(sessionPath, meta); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}
	written, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read rewritten branch meta: %v", err)
	}
	if strings.Contains(string(written), "recovery_checkpoint_enabled") {
		t.Fatalf("retired recovery field survived rewrite: %s", written)
	}
}

func TestBranchMetaRoundTripAndList(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "root.jsonl")
	childPath := filepath.Join(dir, "child.jsonl")

	root := NewSession("sys")
	root.Add(provider.Message{Role: provider.RoleUser, Content: "root prompt"})
	if err := root.Save(rootPath); err != nil {
		t.Fatal(err)
	}
	if err := TouchBranchMeta(rootPath); err != nil {
		t.Fatal(err)
	}

	child := NewSession("sys")
	child.Add(provider.Message{Role: provider.RoleUser, Content: "child prompt"})
	if err := child.Save(childPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(childPath, BranchMeta{Name: "experiment", ParentID: BranchID(rootPath), ForkTurn: 2}); err != nil {
		t.Fatal(err)
	}

	branches, err := ListBranches(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 {
		t.Fatalf("branches = %d, want 2", len(branches))
	}
	var rootFound, childFound bool
	for _, b := range branches {
		if b.ID == "root" {
			rootFound = true
		}
		if b.ParentID == "root" && b.Name == "experiment" {
			childFound = true
		}
	}
	if !rootFound {
		t.Fatal("root branch not found")
	}
	if !childFound {
		t.Fatalf("child with parent root and name experiment not found among %+v", branches)
	}
}

func TestListBranchesSkipsCleanupPending(t *testing.T) {
	dir := t.TempDir()
	visiblePath := filepath.Join(dir, "visible.jsonl")
	pendingPath := filepath.Join(dir, "pending.jsonl")

	visible := NewSession("sys")
	visible.Add(provider.Message{Role: provider.RoleUser, Content: "visible prompt"})
	if err := visible.Save(visiblePath); err != nil {
		t.Fatal(err)
	}
	if err := TouchBranchMeta(visiblePath); err != nil {
		t.Fatal(err)
	}

	pending := NewSession("sys")
	pending.Add(provider.Message{Role: provider.RoleUser, Content: "pending prompt"})
	if err := pending.Save(pendingPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(pendingPath, BranchMeta{Name: "pending experiment"}); err != nil {
		t.Fatal(err)
	}
	if err := MarkCleanupPending(pendingPath, "delete"); err != nil {
		t.Fatal(err)
	}

	branches, err := ListBranches(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 {
		t.Fatalf("branches = %d, want 1: %+v", len(branches), branches)
	}
	if branches[0].Path != visiblePath {
		t.Fatalf("listed branch path = %q, want %q", branches[0].Path, visiblePath)
	}
}

func TestSessionInFlightTurnMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "in-flight.jsonl")
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "work"})
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := TouchBranchMeta(path); err != nil {
		t.Fatal(err)
	}
	before, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	updatedAt := before.UpdatedAt

	if err := MarkSessionInFlightTurn(path, 1, true); err != nil {
		t.Fatal(err)
	}
	marked, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta marked ok=%v err=%v", ok, err)
	}
	if marked.InFlightTurn == nil {
		t.Fatal("in-flight turn marker missing")
	}
	if marked.InFlightTurn.ID == "" || marked.InFlightTurn.StartRevision == 0 || marked.InFlightTurn.StartDigest == "" {
		t.Fatalf("marker identity = %+v, want id/revision/digest", marked.InFlightTurn)
	}
	if marked.InFlightTurn.StartMessageIndex != 1 || !marked.InFlightTurn.PreserveUser {
		t.Fatalf("in-flight marker = %+v, want index=1 preserveUser=true", marked.InFlightTurn)
	}
	if marked.InFlightTurn.StartedAt.IsZero() || time.Since(marked.InFlightTurn.StartedAt) > time.Minute {
		t.Fatalf("unexpected marker timestamp: %v", marked.InFlightTurn.StartedAt)
	}
	if !marked.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("MarkSessionInFlightTurn updated activity time: got %v want %v", marked.UpdatedAt, updatedAt)
	}
	oldMarker := *marked.InFlightTurn
	newMarker, err := BeginSessionInFlightTurn(path, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if cleared, err := ClearSessionInFlightTurnIfMatch(path, oldMarker); err != nil {
		t.Fatal(err)
	} else if cleared {
		t.Fatal("stale marker unexpectedly cleared a newer marker")
	}
	current, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || current.InFlightTurn == nil || current.InFlightTurn.ID != newMarker.ID {
		t.Fatalf("new marker after stale clear = %+v ok=%v err=%v", current.InFlightTurn, ok, err)
	}

	if err := UpdateSessionMeta(path, "model-a", "preview", 1, true); err != nil {
		t.Fatal(err)
	}
	refreshed, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta refreshed ok=%v err=%v", ok, err)
	}
	if refreshed.InFlightTurn == nil {
		t.Fatal("UpdateSessionMeta dropped in-flight marker")
	}
	if refreshed.InFlightTurn.StartMessageIndex != 2 || refreshed.InFlightTurn.PreserveUser {
		t.Fatalf("refreshed in-flight marker = %+v, want index=2 preserveUser=false", refreshed.InFlightTurn)
	}
	updatedAt = refreshed.UpdatedAt

	if _, err := ClearSessionInFlightTurnIfMatch(path, newMarker); err != nil {
		t.Fatal(err)
	}
	cleared, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta cleared ok=%v err=%v", ok, err)
	}
	if cleared.InFlightTurn != nil {
		t.Fatalf("in-flight marker survived clear: %+v", cleared.InFlightTurn)
	}
	if !cleared.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("ClearSessionInFlightTurn updated activity time: got %v want %v", cleared.UpdatedAt, updatedAt)
	}
}

func TestUpdateBranchMetaWithoutTouchUsesFileMtimeForZeroUpdatedAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	when := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(BranchMetaPath(path), []byte(`{"id":"session","created_at":"2026-02-03T04:05:06Z","updated_at":"0001-01-01T00:00:00Z"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := UpdateBranchMeta(path, false, func(meta *BranchMeta) error {
		meta.TopicID = "topic"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if !got.UpdatedAt.Equal(when.UTC()) {
		t.Fatalf("UpdatedAt = %v, want file mtime %v (listing writes must not mint wall-clock activity)", got.UpdatedAt, when.UTC())
	}
	if got.TopicID != "topic" {
		t.Fatalf("TopicID = %q, want topic", got.TopicID)
	}
}

func TestSessionModelRoundTripPreservesActivity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	session := NewSession("sys")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadSessionModel(path); ok {
		t.Fatal("fresh session should not have a stored model")
	}
	meta, err := EnsureBranchMeta(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := SetBranchModelPreserveUpdated(path, "openrouter/anthropic/claude-sonnet"); err != nil {
		t.Fatal(err)
	}
	model, ok := LoadSessionModel(path)
	if !ok || model != "openrouter/anthropic/claude-sonnet" {
		t.Fatalf("LoadSessionModel = %q, %v", model, ok)
	}
	updated, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if !updated.UpdatedAt.Equal(meta.UpdatedAt) {
		t.Fatalf("model write refreshed activity: before=%s after=%s", meta.UpdatedAt, updated.UpdatedAt)
	}
}
