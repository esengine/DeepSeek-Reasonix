package rootenv

import (
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/state/sessionstore"
)

func attachmentAuthority(t *testing.T, path string) *sessionstore.SessionWriteAuthority {
	t.Helper()
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.Release)
	authority, err := lease.IssueWriteAuthority(sessionstore.NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func TestEnvironmentAttachmentsRequireBothOwnersAndSurviveReopen(t *testing.T) {
	s, _ := fixture(t)
	parent, next := filepath.Join(t.TempDir(), "parent.jsonl"), filepath.Join(t.TempDir(), "next.jsonl")
	parent = sessionstore.CanonicalSessionPath(parent)
	next = sessionstore.CanonicalSessionPath(next)
	e, err := s.Prepare(t.Context(), parent, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.BindSession(t.Context(), parent, attachmentAuthority(t, parent)); err != nil {
		t.Fatal(err)
	}
	if err := e.AttachSession(t.Context(), parent, next, nil); !errors.Is(err, sessionstore.ErrSessionWriteAuthorityMissing) {
		t.Fatalf("missing target authority: %v", err)
	}
	if err := e.CheckSession(t.Context(), next); err == nil {
		t.Fatal("unbound transcript accepted")
	}
	nextAuthority := attachmentAuthority(t, next)
	if err := e.AttachSession(t.Context(), "unowned", next, nextAuthority); err == nil {
		t.Fatal("unowned parent attached a session")
	}
	if err := e.AttachSession(t.Context(), parent, next, nextAuthority); err != nil {
		t.Fatal(err)
	}
	r := e.Record()
	if r.SessionID != parent {
		t.Fatal("attachment changed creation identity")
	}
	r.Attachments[next] = "forged"
	if err := e.CheckSession(t.Context(), next); err != nil {
		t.Fatal("record accessor leaked mutable membership", err)
	}
	e.Close()
	reopened, err := s.Open(t.Context(), r.ID, next)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.CheckSession(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	if err := e.AttachSession(t.Context(), parent, filepath.Join(t.TempDir(), "stale.jsonl"), nextAuthority); !errors.Is(err, ErrStale) {
		t.Fatalf("retired owner attached a session: %v", err)
	}
}
