package agent

import (
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

// A Controller snapshot saves the transcript before publishing its listing.
// Resume the latter half only after another runtime owns the same transcript
// and has published a different connection. No timing or fake authority is used.
func TestListingProjectionCannotOverwriteModelAfterAuthorityReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connection.jsonl")
	old := NewSession("same system prompt")
	old.Add(provider.Message{Role: provider.RoleUser, Content: "keep this conversation"})
	writer := bindSessionWriter(t, old, path)
	if err := old.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	expected, ok := old.PersistedState(path)
	if !ok {
		t.Fatal("missing saved transcript identity")
	}
	oldAuthority := old.WriteAuthority()
	if applied, err := UpdateOwnedSessionListingProjectionIfCurrent(path, "connection-one/same-model", "", "keep this conversation", 1, false, expected, oldAuthority); err != nil || !applied {
		t.Fatalf("initial listing: applied=%v err=%v", applied, err)
	}

	// This is SetModelForTab's publication ordering: bind the new controller,
	// then persist its canonical selection without rewriting the transcript.
	replacement := NewSession("same system prompt")
	if err := writer.Bind(replacement, NextSessionWriteGeneration()); err != nil {
		t.Fatal(err)
	}
	if oldAuthority.Valid() || !replacement.WriteAuthority().Valid() {
		t.Fatal("replacement did not fence the previous write authority")
	}
	const selected = "connection-two/same-model"
	if err := SetBranchModelPreserveUpdated(path, selected); err != nil {
		t.Fatal(err)
	}

	// The retired snapshot's transcript has already been committed. Its delayed
	// listing publication must not undo the subsequently committed model choice.
	_, err := UpdateOwnedSessionListingProjectionIfCurrent(path, "connection-one/same-model", "", "keep this conversation", 1, false, expected, oldAuthority)
	if !errors.Is(err, ErrSessionWriteAuthorityStale) {
		t.Fatalf("retired publication error = %v, want stale authority", err)
	}
	if got, ok := LoadSessionModel(path); !ok || got != selected {
		t.Fatalf("retired listing overwrote selected connection: model=%q present=%v want=%q", got, ok, selected)
	}
}

func TestListingAuthorityGuardRetainsGenerationThroughCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guarded-connection.jsonl")
	old := NewSession("system")
	old.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
	writer := bindSessionWriter(t, old, path)
	if err := old.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	authority := old.WriteAuthority()
	unlock, err := authority.lockCurrentLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	// This directly checks the exclusion invariant, without relying on a
	// scheduler delay to infer that the competing bind is blocked.
	if authority.lease.mu.TryLock() {
		authority.lease.mu.Unlock()
		t.Fatal("publication guard released the generation mutex before commit")
	}
	entered, bound := make(chan struct{}), make(chan error, 1)
	replacement := NewSession("system")
	go func() {
		close(entered)
		bound <- writer.Bind(replacement, NextSessionWriteGeneration())
	}()
	<-entered
	if err := SetBranchModelPreserveUpdated(path, "connection-one/model"); err != nil {
		t.Fatal(err)
	}
	if authority.lease.writeGeneration != authority.generation {
		t.Fatal("generation changed inside the publication critical section")
	}
	unlock()
	unlock = nil
	if err := <-bound; err != nil {
		t.Fatal(err)
	}
	if authority.Valid() || !replacement.WriteAuthority().Valid() {
		t.Fatal("replacement failed to advance generation after publication")
	}
}

func TestOwnedListingRejectsMissingAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-authority.jsonl")
	session := NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
	bindSessionWriter(t, session, path)
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	expected, ok := session.PersistedState(path)
	if !ok {
		t.Fatal("missing saved transcript identity")
	}
	if err := SetBranchModelPreserveUpdated(path, "selected/model"); err != nil {
		t.Fatal(err)
	}
	applied, err := UpdateOwnedSessionListingProjectionIfCurrent(path, "stale/model", "", "question", 1, false, expected, nil)
	if applied || !errors.Is(err, ErrSessionWriteAuthorityMissing) {
		t.Fatalf("missing authority publication: applied=%v err=%v", applied, err)
	}
	if model, ok := LoadSessionModel(path); !ok || model != "selected/model" {
		t.Fatalf("missing authority changed model: %q", model)
	}
}
