package executionpresence

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"reasonix/internal/base/filelock"
)

func storeFor(t *testing.T, root, state string) *Store {
	t.Helper()
	s, err := New(root, state)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func reserve(t *testing.T, s *Store, request Request) *Reservation {
	t.Helper()
	r, err := s.Reserve(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestSelectionReservesPreparingWritersWithoutLockingFiles(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	a, b := storeFor(t, root, state), storeFor(t, root, state)
	writer := Request{Strategy: Auto, Writer: true}
	first := reserve(t, a, writer)
	if first.Record().Selected != Shared || first.Record().Phase != "preparing" {
		t.Fatal(first.Record())
	}
	read := reserve(t, b, Request{Strategy: Auto})
	if read.Record().Selected != Shared {
		t.Fatal("read-only work created an isolated environment")
	}
	second := reserve(t, b, writer)
	if second.Record().Selected != Isolated {
		t.Fatal("preparing writer was invisible to another first execution")
	}
	if err := first.Activate(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if err := second.Activate(t.Context(), root); !errors.Is(err, ErrProtocol) {
		t.Fatalf("an isolated decision accepted the source directory: %v", err)
	}
	candidate := t.TempDir()
	if err := second.Activate(t.Context(), candidate); err != nil {
		t.Fatal(err)
	}
	insideCandidate := storeFor(t, candidate, state)
	if got := reserve(t, insideCandidate, writer).Record().Selected; got != Isolated {
		t.Fatal("a running isolated writer was invisible inside its own working copy")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Activate(t.Context(), root); !errors.Is(err, ErrStale) {
		t.Fatalf("closed reservation reactivated without presence: %v", err)
	}
	third := reserve(t, a, writer)
	if third.Record().Selected != Shared {
		t.Fatal("an isolated task falsely occupied the source working copy")
	}
}

func TestWorkingCopyAndLineageAreIndependentOfSharedGitStorage(t *testing.T) {
	state, root := t.TempDir(), t.TempDir()
	a := storeFor(t, root, state)
	first := reserve(t, a, Request{Strategy: Auto, Writer: true, RootExecutionID: "root-task"})
	child := reserve(t, a, Request{Strategy: Auto, Writer: true, RootExecutionID: "root-task", ParentExecutionID: first.Record().ID})
	if child.Record().Selected != Isolated {
		t.Fatal("lineage metadata hid a parent that can still write")
	}
	if got := reserve(t, a, Request{Strategy: Auto, Writer: true, RootExecutionID: "root-task"}).Record().Selected; got != Isolated {
		t.Fatal("an independent execution borrowed existing lineage to bypass isolation")
	}
	otherWorktree := storeFor(t, t.TempDir(), state)
	if got := reserve(t, otherWorktree, Request{Strategy: Auto, Writer: true}).Record().Selected; got != Shared {
		t.Fatalf("distinct working copies competed through shared state: %s", got)
	}
}

func TestFallbackRequiresAutoAndPersistsItsReason(t *testing.T) {
	root := t.TempDir()
	s := storeFor(t, root, t.TempDir())
	_ = reserve(t, s, Request{Strategy: Auto, Writer: true})
	r := reserve(t, s, Request{Strategy: Auto, Writer: true})
	if err := r.FallbackShared(t.Context(), "repository has no commit"); err != nil {
		t.Fatal(err)
	}
	if err := r.Activate(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	current, _, err := s.read(r.Record().ID)
	if err != nil || current.Selected != Shared || current.Reason != "repository has no commit" {
		t.Fatalf("fallback lost its explicit diagnosis: %+v %v", current, err)
	}
	strict := reserve(t, s, Request{Strategy: Isolated, Writer: true})
	if err := strict.FallbackShared(t.Context(), "failed"); !errors.Is(err, ErrProtocol) {
		t.Fatalf("required isolation silently degraded: %v", err)
	}
}

func TestGenerationChecksAndUnknownFieldsSurviveActivation(t *testing.T) {
	root := t.TempDir()
	s := storeFor(t, root, t.TempDir())
	r := reserve(t, s, Request{Strategy: Auto, Writer: true})
	current, fields, err := s.read(r.Record().ID)
	if err != nil {
		t.Fatal(err)
	}
	fields["future_field"] = json.RawMessage(`{"retained":true}`)
	raw, _ := json.Marshal(fields)
	if err := os.WriteFile(s.recordPath(current.ID), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.Activate(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	_, fields, err = s.read(current.ID)
	if err != nil || string(fields["future_field"]) != `{"retained":true}` {
		t.Fatalf("activation erased an unknown extension: %s %v", fields["future_field"], err)
	}
	newer := r.Record()
	newer.Generation = rand.Text()
	release, err := filelock.TryAcquire(s.livePath(newer))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := s.saveNew(newer); err != nil {
		t.Fatal(err)
	}
	if err := r.Activate(t.Context(), root); !errors.Is(err, ErrStale) {
		t.Fatalf("idempotent activation forgot generation validation: %v", err)
	}
	if err := r.Close(); !errors.Is(err, ErrStale) {
		t.Fatalf("old generation removed a successor: %v", err)
	}
	if _, _, err := s.read(newer.ID); err != nil {
		t.Fatalf("successor record lost: %v", err)
	}
	if probe, err := filelock.TryAcquire(s.livePath(newer)); !errors.Is(err, filelock.ErrHeld) {
		if probe != nil {
			probe()
		}
		t.Fatalf("old callback released successor liveness: %v", err)
	}
}

func TestCancelledAndUnknownProtocolReservationsFailClosed(t *testing.T) {
	s := storeFor(t, t.TempDir(), t.TempDir())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Reserve(ctx, Request{Strategy: Auto, Writer: true}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	id := rand.Text()
	raw := []byte(`{"version":2,"id":"` + id + `","future":"retained"}`)
	if err := os.WriteFile(s.recordPath(id), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(t.Context(), Request{Strategy: Auto, Writer: true}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("unknown protocol was silently pruned: %v", err)
	}
	if got, err := os.ReadFile(s.recordPath(id)); err != nil || string(got) != string(raw) {
		t.Fatalf("unknown record changed: %s %v", got, err)
	}
}
