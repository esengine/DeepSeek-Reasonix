package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"reasonix/internal/state/workspacelease"
)

func TestStaleWaiterLeavesBeforeCurrentWriterReleases(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "shared")
	holder := testCoordinator(t, root, state)
	held, err := holder.Acquire(t.Context(), intent(path))
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	var notices []Notice
	waiter, err := New(root, state, func(n Notice) {
		notices = append(notices, n)
		if n.State == "waiting" {
			if err := os.WriteFile(path, []byte("writer postimage"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	request := intent(path)
	request.Expected = map[string]ContentVersion{path: {Kind: "absent"}}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	lease, err := waiter.Acquire(ctx, request)
	lease.End(NotExecuted)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale waiter waited for resource release: %v", err)
	}
	if len(notices) != 2 || notices[0].State != "waiting" || notices[1].State != "abandoned" || notices[1].Reason != "conflict" {
		t.Fatalf("stale wait was not closed as a conflict: %+v", notices)
	}
	if notices[0].TransactionID != notices[1].TransactionID {
		t.Fatal("conflict closed another wait")
	}
	if err := held.CheckPaths([]string{path}); err != nil {
		t.Fatalf("stale waiter disturbed the writer: %v", err)
	}
	records, err := waiter.records()
	if err != nil || slices.ContainsFunc(records, func(r record) bool { return r.ID != held.ID }) {
		t.Fatalf("stale queue entry retained: %+v %v", records, err)
	}
}

func TestAlreadyStaleRequestDoesNotEnterCompatibilityPreparation(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "shared")
	if err := os.WriteFile(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	c := testCoordinator(t, root, state)
	c.compatClaim = func(string, string, string, []string) (func(), *workspacelease.ConflictError, error) {
		t.Fatal("stale request entered compatibility preparation")
		return nil, nil, ErrProtocol
	}
	request := intent(path)
	request.Expected = map[string]ContentVersion{path: {Kind: "absent"}}
	lease, err := c.Acquire(t.Context(), request)
	lease.End(NotExecuted)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale request: %v", err)
	}
}
