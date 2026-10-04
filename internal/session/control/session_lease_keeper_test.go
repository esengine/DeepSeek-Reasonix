package control

import (
	"errors"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strconv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/store"
)

func TestSessionLeaseKeeperRebindMovesLease(t *testing.T) {
	dir := testenv.TempDir(t)
	a := filepath.Join(dir, "a.jsonl")
	b := filepath.Join(dir, "b.jsonl")

	k := NewSessionLeaseKeeper()
	defer k.Release()

	if err := k.Rebind(a); err != nil {
		t.Fatalf("Rebind(a): %v", err)
	}
	if got, want := k.HeldPath(), sessionstore.CanonicalSessionPath(a); got != want {
		t.Fatalf("HeldPath = %q, want %q", got, want)
	}
	if _, err := os.Stat(store.SessionLeaseInfo(sessionstore.CanonicalSessionPath(a))); err != nil {
		t.Fatalf("lease info for a missing: %v", err)
	}
	// a is held: an outside acquire must fail.
	if _, err := sessionstore.TryAcquireSessionLease(a); !errors.Is(err, sessionstore.ErrSessionLeaseHeld) {
		t.Fatalf("TryAcquireSessionLease(a) while kept = %v, want ErrSessionLeaseHeld", err)
	}

	if err := k.Rebind(b); err != nil {
		t.Fatalf("Rebind(b): %v", err)
	}
	if got, want := k.HeldPath(), sessionstore.CanonicalSessionPath(b); got != want {
		t.Fatalf("HeldPath after rebind = %q, want %q", got, want)
	}
	// The old lease is released: a is acquirable again and its info is gone.
	if _, err := os.Stat(store.SessionLeaseInfo(sessionstore.CanonicalSessionPath(a))); !os.IsNotExist(err) {
		t.Fatalf("lease info for a after rebind stat err = %v, want not exist", err)
	}
	lease, err := sessionstore.TryAcquireSessionLease(a)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease(a) after rebind: %v", err)
	}
	lease.Release()
}

func TestSessionLeaseKeeperRebindSamePathIsNoop(t *testing.T) {
	dir := testenv.TempDir(t)
	a := filepath.Join(dir, "a.jsonl")

	k := NewSessionLeaseKeeper()
	defer k.Release()
	if err := k.Rebind(a); err != nil {
		t.Fatalf("Rebind(a): %v", err)
	}
	// Same canonical path again must not trip over the keeper's own lease.
	if err := k.Rebind(a); err != nil {
		t.Fatalf("Rebind(a) again: %v", err)
	}
	if got, want := k.HeldPath(), sessionstore.CanonicalSessionPath(a); got != want {
		t.Fatalf("HeldPath = %q, want %q", got, want)
	}
}

func TestSessionLeaseKeeperRefusesHeldPathAndKeepsCurrent(t *testing.T) {
	dir := testenv.TempDir(t)
	a := filepath.Join(dir, "a.jsonl")
	b := filepath.Join(dir, "b.jsonl")

	holder, err := sessionstore.TryAcquireSessionLease(b)
	if err != nil {
		t.Fatalf("holder acquire: %v", err)
	}
	defer holder.Release()

	k := NewSessionLeaseKeeper()
	defer k.Release()
	if err := k.Rebind(a); err != nil {
		t.Fatalf("Rebind(a): %v", err)
	}
	err = k.Rebind(b)
	if !errors.Is(err, sessionstore.ErrSessionLeaseHeld) {
		t.Fatalf("Rebind(held b) = %v, want ErrSessionLeaseHeld", err)
	}
	// Failure leaves the keeper on its previous session.
	if got, want := k.HeldPath(), sessionstore.CanonicalSessionPath(a); got != want {
		t.Fatalf("HeldPath after refused rebind = %q, want %q", got, want)
	}
}

func TestSessionLeaseKeeperEmptyPathReleases(t *testing.T) {
	dir := testenv.TempDir(t)
	a := filepath.Join(dir, "a.jsonl")

	k := NewSessionLeaseKeeper()
	defer k.Release()
	if err := k.Rebind(a); err != nil {
		t.Fatalf("Rebind(a): %v", err)
	}
	if err := k.Rebind(""); err != nil {
		t.Fatalf("Rebind(empty): %v", err)
	}
	if got := k.HeldPath(); got != "" {
		t.Fatalf("HeldPath after empty rebind = %q, want empty", got)
	}
	lease, err := sessionstore.TryAcquireSessionLease(a)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease(a) after empty rebind: %v", err)
	}
	lease.Release()
}

func TestSessionLeaseKeeperReleaseRemovesLeaseInfo(t *testing.T) {
	dir := testenv.TempDir(t)
	a := filepath.Join(dir, "a.jsonl")

	k := NewSessionLeaseKeeper()
	if err := k.Rebind(a); err != nil {
		t.Fatalf("Rebind(a): %v", err)
	}
	k.Release()
	k.Release() // idempotent
	if _, err := os.Stat(store.SessionLeaseInfo(sessionstore.CanonicalSessionPath(a))); !os.IsNotExist(err) {
		t.Fatalf("lease info after Release stat err = %v, want not exist", err)
	}
	if got := k.HeldPath(); got != "" {
		t.Fatalf("HeldPath after Release = %q, want empty", got)
	}
}

func TestSessionLeaseKeeperRecoveryRebindsControllerBeforeReturning(t *testing.T) {
	dir := testenv.TempDir(t)
	a := filepath.Join(dir, "a.jsonl")
	b := filepath.Join(dir, "b.jsonl")
	sess := sessionstore.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	ctrl := New(Options{Executor: exec, SessionPath: a, Sink: event.Discard})

	k := NewSessionLeaseKeeper()
	defer k.Release()
	if err := k.Rebind(a); err != nil {
		t.Fatal(err)
	}
	if err := k.BindControllerAuthority(ctrl); err != nil {
		t.Fatal(err)
	}
	releaseSave, err := sess.WriteAuthority().BeginSave(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.HandleSessionRecovered(SessionRecoveryInfo{RecoveryPath: b}); err != nil {
		t.Fatalf("HandleSessionRecovered: %v", err)
	}
	if got := k.HeldPath(); got != sessionstore.CanonicalSessionPath(b) {
		t.Fatalf("HeldPath = %q, want %q", got, sessionstore.CanonicalSessionPath(b))
	}
	if auth := sess.WriteAuthority(); auth == nil || !auth.Covers(b) {
		t.Fatal("controller was published without authority for recovery path")
	}
	releaseSave()
}

func TestSessionInUseMessageNamesHolder(t *testing.T) {
	otherPID := os.Getpid() + 1
	acquired := time.Date(2026, 7, 6, 3, 4, 0, 0, time.UTC)
	err := &sessionstore.SessionLeaseError{
		Path: "/tmp/x.jsonl",
		Info: &sessionstore.SessionLeaseInfo{
			SessionPath: "/tmp/x.jsonl",
			WriterID:    "writer-nonce-should-not-appear",
			PID:         otherPID,
			Hostname:    "devbox",
			AcquiredAt:  acquired,
		},
	}
	msg := SessionInUseMessage(err)
	if !strings.Contains(msg, "another Reasonix process") {
		t.Fatalf("message %q missing holder wording", msg)
	}
	if !strings.Contains(msg, "pid "+strconv.Itoa(otherPID)) || !strings.Contains(msg, "on devbox") {
		t.Fatalf("message %q missing pid/host", msg)
	}
	if !strings.Contains(msg, "since "+acquired.Local().Format("15:04")) {
		t.Fatalf("message %q missing local acquire time", msg)
	}
	if strings.Contains(msg, "writer-nonce-should-not-appear") {
		t.Fatalf("message %q leaks the writer id", msg)
	}
	if strings.Contains(msg, "/tmp/x.jsonl") {
		t.Fatalf("message %q leaks the session path", msg)
	}
}

func TestSessionInUseMessageFallsBackWithoutInfo(t *testing.T) {
	for name, err := range map[string]error{
		"nil info":   &sessionstore.SessionLeaseError{Path: "/tmp/x.jsonl"},
		"plain held": sessionstore.ErrSessionLeaseHeld,
		"zero pid":   &sessionstore.SessionLeaseError{Info: &sessionstore.SessionLeaseInfo{PID: 0}},
	} {
		msg := SessionInUseMessage(err)
		if msg != "this session is in use by another Reasonix window or process" {
			t.Fatalf("%s: message = %q, want generic fallback", name, msg)
		}
		if strings.Contains(msg, "pid "+strconv.Itoa(os.Getpid())) {
			t.Fatalf("%s: fallback should not invent a pid: %q", name, msg)
		}
	}
}
