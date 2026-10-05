package serve

import (
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// studioPane builds a runtime the way the desktop shell does: a controller with
// somewhere to save but no session yet, adopted by a hub. The CLI arranges its
// own lease before adopting; this host does not, which is the case that used to
// leave the session unowned.
func studioPane(t *testing.T, dir string) (*Hub, *control.Controller, *Runtime) {
	t.Helper()
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: dir})
	srv := New(ctrl, bc, config.ServeConfig{})
	hub := NewHub(HubOptions{})
	rt, err := hub.Adopt(srv, bc)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close(rt.ID) })
	return hub, ctrl, rt
}

// A window opens with no session at all — the first turn mints one. That path
// has to arrive owned: an unowned session is one a second process can open and
// write too, and a session that never bound authority forks recovery copies on
// conflict instead of refusing the write. That pair is what turned "the window
// did not shut down before I reopened it" into an endless run of backups.
func TestAnAdoptedPaneOwnsTheSessionItMints(t *testing.T) {
	dir := testenv.TempDir(t)
	_, ctrl, _ := studioPane(t, dir)

	if ctrl.SessionPath() != "" {
		t.Fatal("a fresh window is expected to open without a session")
	}
	ctrl.EnsureSessionPath() // what the first turn does
	path := ctrl.SessionPath()
	if path == "" {
		t.Fatal("no session was minted")
	}

	if lease, err := sessionstore.TryAcquireSessionLease(path); err == nil {
		lease.Release()
		t.Fatal("the session this pane is writing was free for another writer to take")
	}
}

// Adopting is where a host learns the session it inherited is still being
// written, which is the moment it can still choose another one.
func TestAdoptRefusesASessionAnotherWriterHolds(t *testing.T) {
	dir := testenv.TempDir(t)
	held := filepath.Join(dir, "held.jsonl")
	saveServeTestSession(t, held)
	holder, err := sessionstore.TryAcquireSessionLease(held)
	if err != nil {
		t.Fatalf("test holder acquire: %v", err)
	}
	defer holder.Release()

	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: held})
	srv := New(ctrl, bc, config.ServeConfig{})
	hub := NewHub(HubOptions{})

	rt, err := hub.Adopt(srv, bc)
	if err == nil {
		if rt != nil {
			_ = hub.Close(rt.ID)
		}
		t.Fatal("adopted a session another writer already holds")
	}
	// The refusal has to leave nothing behind: the host's next move is to pick
	// another session and adopt again.
	if srv.leases != nil {
		t.Error("a refused adoption left a keeper on the server")
	}
}

// The host that arranged ownership itself keeps it — the hub must not take a
// second lease on the same file and must not release what it did not acquire.
func TestAdoptLeavesAHostArrangedLeaseAlone(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "own.jsonl")
	saveServeTestSession(t, path)

	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: path})
	srv := New(ctrl, bc, config.ServeConfig{})
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(path); err != nil {
		t.Fatalf("host acquires its own lease: %v", err)
	}
	if err := srv.SetSessionLeases(leases); err != nil {
		t.Fatalf("host hands it over: %v", err)
	}

	hub := NewHub(HubOptions{})
	rt, err := hub.Adopt(srv, bc)
	if err != nil {
		t.Fatalf("adopt with a host-arranged lease: %v", err)
	}
	if rt.leases != nil {
		t.Error("the hub took ownership of a lease the host is responsible for")
	}
	_ = hub.Close(rt.ID)
	// Closing the pane must not have released the host's lease.
	if lease, err := sessionstore.TryAcquireSessionLease(path); err == nil {
		lease.Release()
		t.Error("closing the pane released a lease the host still holds")
	}
}

func TestReadonlyPanePromotesAfterTransientHolderLeaves(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "handoff.jsonl")
	saveServeTestSession(t, path)
	holder, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("holder acquire: %v", err)
	}

	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: path})
	defer ctrl.Close()
	srv := New(ctrl, bc, config.ServeConfig{})
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := srv.SetSessionLeases(leases); err != nil {
		t.Fatal(err)
	}
	if writable, err := leases.Attach(path); err != nil || writable {
		t.Fatalf("attach while held = writable %v, err %v", writable, err)
	}
	if err := leases.BindControllerAuthority(ctrl); err != nil {
		t.Fatal(err)
	}
	rt := &Runtime{ID: "handoff", Server: srv, Events: bc, leases: leases}
	if !rt.view().ReadOnly {
		t.Fatal("pane should remain read-only while the holder is live")
	}

	holder.Release()
	if view := rt.view(); view.ReadOnly {
		t.Fatal("pane stayed read-only after the transient holder released")
	}
	if got := leases.HeldPath(); got != sessionstore.CanonicalSessionPath(path) {
		t.Fatalf("promoted lease path = %q", got)
	}
}
