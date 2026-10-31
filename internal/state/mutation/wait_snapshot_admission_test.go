package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/state/workspacelease"
)

func TestWaitObservationNeverAuthorizesPastANewLegacyClaim(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "file")
	owner := testCoordinator(t, root, state)
	held, err := owner.Acquire(t.Context(), intent(path))
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	ready, resume, done := make(chan Notice, 1), make(chan struct{}), make(chan error, 1)
	unblock := sync.OnceFunc(func() { close(resume) })
	waiter, err := New(root, state, func(n Notice) {
		if n.State == "waiting" {
			ready <- n
			<-resume
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer func() { cancel(); unblock(); <-done }()
	claim := waiter.compatClaim
	checked := make(chan error, 1)
	waiter.compatClaim = func(root, dir, session string, paths []string) (func(), *workspacelease.ConflictError, error) {
		release, conflict, err := claim(root, dir, session, paths)
		select {
		case checked <- err:
		default:
		}
		return release, conflict, err
	}
	go func() {
		lease, err := waiter.Acquire(ctx, intent(path))
		if lease != nil {
			lease.End(NotExecuted)
		}
		done <- err
	}()
	awaitCapacityNotice(t, ready, "waiting")
	held.End(NotExecuted)
	if _, err := waitingSnapshots.get(t.Context(), waiter.dir, waiter.readWaitingRecords); err != nil {
		t.Fatal(err)
	}
	legacy, err := workspacelease.New(root, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	legacy.BeginRun()
	defer legacy.EndRun()
	if err := legacy.AcquirePaths(t.Context(), []string{path}); err != nil {
		t.Fatal(err)
	}
	// The callback stays pinned until the new legacy claim is actually held.
	unblock()
	select {
	case err := <-checked:
		if !errors.Is(err, workspacelease.ErrClaimBusy) {
			t.Fatalf("cached empty view bypassed the new compatibility claim: %v", err)
		}
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	cancel()
}

func TestWaitObservationReportsNewUnknownRecordAndPreservesIt(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "file")
	owner := testCoordinator(t, root, state)
	held, err := owner.Acquire(t.Context(), intent(path))
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	ready, done := make(chan Notice, 1), make(chan error, 1)
	waiter, err := New(root, state, func(n Notice) {
		if n.State == "waiting" {
			ready <- n
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	defer func() { cancel(); <-stopped }()
	go func() {
		defer close(stopped)
		lease, err := waiter.Acquire(ctx, intent(path))
		if lease != nil {
			lease.End(NotExecuted)
		}
		done <- err
	}()
	awaitCapacityNotice(t, ready, "waiting")
	if _, err := waitingSnapshots.get(t.Context(), waiter.dir, waiter.readWaitingRecords); err != nil {
		t.Fatal(err)
	}
	unknown := waiter.recordPath("future")
	raw := []byte(`{"version":999,"id":"future","order":1,"paths":[]}`)
	if err := os.WriteFile(unknown, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrProtocol) {
		t.Fatalf("cached blocker hid unknown protocol: %v", err)
	}
	if actual, err := os.ReadFile(unknown); err != nil || string(actual) != string(raw) {
		t.Fatalf("unknown record was changed: %q %v", actual, err)
	}
	if err := held.CheckPaths([]string{path}); err != nil {
		t.Fatalf("unknown record released another holder: %v", err)
	}
	if lease, err := waiter.TryAcquire(t.Context(), intent(filepath.Join(root, "other"))); !errors.Is(err, ErrProtocol) {
		lease.End(NotExecuted)
		t.Fatalf("new admission bypassed unknown protocol: %v", err)
	}
}

func TestWaitObservationEmptyCannotGrantPastANewTransaction(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	c := testCoordinator(t, root, state)
	path := filepath.Join(root, "file")
	if _, err := waitingSnapshots.get(t.Context(), c.dir, c.readWaitingRecords); err != nil {
		t.Fatal(err)
	}
	// Pin the empty observation so registry IO speed cannot expire the fixture.
	waitingSnapshots.mu.Lock()
	waitingSnapshots.entries[c.dir].expires = time.Now().Add(time.Hour)
	waitingSnapshots.mu.Unlock()
	held, err := c.Acquire(t.Context(), intent(path))
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	request := &Lease{record: record{ID: "waiting", Order: held.record.Order + 1, Paths: held.record.Paths}}
	previous := &blocker{record: held.record, kind: NamedPaths}
	blocker, err := c.retryGrant(t.Context(), request, previous)
	if err != nil || blocker == nil || blocker.ID != held.ID {
		t.Fatalf("empty observation authorized past new transaction: %+v %v", blocker, err)
	}
	if request.record.Active {
		t.Fatal("unregistered waiting request became active")
	}
}
