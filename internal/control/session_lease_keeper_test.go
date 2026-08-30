package control

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/store"
)

func TestSessionLeaseKeeperRebindMovesLease(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.jsonl")
	b := filepath.Join(dir, "b.jsonl")

	k := NewSessionLeaseKeeper()
	defer k.Release()

	if err := k.Rebind(a); err != nil {
		t.Fatalf("Rebind(a): %v", err)
	}
	if got, want := k.HeldPath(), agent.CanonicalSessionPath(a); got != want {
		t.Fatalf("HeldPath = %q, want %q", got, want)
	}
	if info, err := agent.LoadSessionLeaseInfo(agent.CanonicalSessionPath(a)); err != nil || info == nil {
		t.Fatalf("lease info for a missing: %v", err)
	}
	// a is held: an outside acquire must fail.
	if _, err := agent.TryAcquireSessionLease(a); !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("TryAcquireSessionLease(a) while kept = %v, want ErrSessionLeaseHeld", err)
	}

	if err := k.Rebind(b); err != nil {
		t.Fatalf("Rebind(b): %v", err)
	}
	if got, want := k.HeldPath(), agent.CanonicalSessionPath(b); got != want {
		t.Fatalf("HeldPath after rebind = %q, want %q", got, want)
	}
	// The old lease is released: a is acquirable again and its owner info
	// (published inside .lease.lock) is gone with the lock file.
	if _, err := agent.LoadSessionLeaseInfo(agent.CanonicalSessionPath(a)); !os.IsNotExist(err) {
		t.Fatalf("lease info for a after rebind err = %v, want not exist", err)
	}
	lease, err := agent.TryAcquireSessionLease(a)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease(a) after rebind: %v", err)
	}
	lease.Release()
}

func TestSessionLeaseKeeperRebindSamePathIsNoop(t *testing.T) {
	dir := t.TempDir()
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
	if got, want := k.HeldPath(), agent.CanonicalSessionPath(a); got != want {
		t.Fatalf("HeldPath = %q, want %q", got, want)
	}
}

func TestSessionLeaseKeeperRefusesHeldPathAndKeepsCurrent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.jsonl")
	b := filepath.Join(dir, "b.jsonl")

	holder, err := agent.TryAcquireSessionLease(b)
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
	if !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("Rebind(held b) = %v, want ErrSessionLeaseHeld", err)
	}
	// Failure leaves the keeper on its previous session.
	if got, want := k.HeldPath(), agent.CanonicalSessionPath(a); got != want {
		t.Fatalf("HeldPath after refused rebind = %q, want %q", got, want)
	}
}

func TestSessionLeaseKeeperEmptyPathReleases(t *testing.T) {
	dir := t.TempDir()
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
	lease, err := agent.TryAcquireSessionLease(a)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease(a) after empty rebind: %v", err)
	}
	lease.Release()
}

func TestSessionLeaseKeeperReleaseRemovesLeaseInfo(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.jsonl")

	k := NewSessionLeaseKeeper()
	if err := k.Rebind(a); err != nil {
		t.Fatalf("Rebind(a): %v", err)
	}
	k.Release()
	k.Release() // idempotent
	// Owner info lives inside .lease.lock and dies with the lock file on
	// release; no legacy .lease.json sidecar is left behind either.
	if _, err := agent.LoadSessionLeaseInfo(agent.CanonicalSessionPath(a)); !os.IsNotExist(err) {
		t.Fatalf("lease info after Release err = %v, want not exist", err)
	}
	if _, err := os.Stat(store.SessionLeaseInfo(agent.CanonicalSessionPath(a))); !os.IsNotExist(err) {
		t.Fatalf("legacy lease sidecar after Release stat err = %v, want not exist", err)
	}
	if got := k.HeldPath(); got != "" {
		t.Fatalf("HeldPath after Release = %q, want empty", got)
	}
}

func TestSessionLeaseKeeperRecoveryRebindsControllerBeforeReturning(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.jsonl")
	b := filepath.Join(dir, "b.jsonl")
	sess := agent.NewSession("sys")
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
	if got := k.HeldPath(); got != agent.CanonicalSessionPath(b) {
		t.Fatalf("HeldPath = %q, want %q", got, agent.CanonicalSessionPath(b))
	}
	if auth := sess.WriteAuthority(); auth == nil || !auth.Covers(b) {
		t.Fatal("controller was published without authority for recovery path")
	}
	releaseSave()
}

func TestSessionLeaseKeeperBindsPrivateResumeCandidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.jsonl")
	outgoing := agent.NewSession("outgoing")
	ctrl := New(Options{Executor: agent.New(nil, nil, outgoing, agent.Options{}, event.Discard), SessionPath: path, Sink: event.Discard})
	keeper := NewSessionLeaseKeeper()
	defer keeper.Release()
	if err := keeper.Rebind(path); err != nil {
		t.Fatal(err)
	}
	if err := keeper.BindControllerAuthority(ctrl); err != nil {
		t.Fatal(err)
	}
	oldAuthority := outgoing.WriteAuthority()
	candidate := agent.NewSession("candidate")
	if err := keeper.BindSessionAuthority(candidate); err != nil {
		t.Fatalf("BindSessionAuthority: %v", err)
	}
	if auth := candidate.WriteAuthority(); auth == nil || !auth.Covers(path) {
		t.Fatal("private resume candidate was not bound to the held path")
	}
	if oldAuthority == nil || oldAuthority.Valid() {
		t.Fatal("candidate binding did not retire the outgoing write generation")
	}
	ctrl.Resume(candidate, path)
	if got, want := ctrl.WriteAuthorityGeneration(), candidate.WriteAuthority().Generation(); got != want || got == 0 {
		t.Fatalf("resumed controller authority generation = %d, want %d", got, want)
	}
}

func TestSessionLeaseKeeperRebindDetachingTransfersRecoveryCallback(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.jsonl")
	b := filepath.Join(dir, "b.jsonl")
	c := filepath.Join(dir, "c.jsonl")
	ctrl := New(Options{Executor: agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard), SessionPath: a, Sink: event.Discard})
	keeper := NewSessionLeaseKeeper()
	if err := keeper.Rebind(a); err != nil {
		t.Fatal(err)
	}
	if err := keeper.BindControllerAuthority(ctrl); err != nil {
		t.Fatal(err)
	}
	ctrl.SetOnSessionRecovered(keeper.HandleSessionRecovered)
	detached, err := keeper.RebindDetaching(b)
	if err != nil {
		t.Fatal(err)
	}
	defer keeper.Release()
	defer detached.Release()
	handler := ctrl.sessionRecoveredHandler()
	if handler == nil {
		t.Fatal("detached controller lost its recovery callback")
	}
	if err := handler(SessionRecoveryInfo{RecoveryPath: c}); err != nil {
		t.Fatal(err)
	}
	if got := detached.HeldPath(); got != agent.CanonicalSessionPath(c) {
		t.Fatalf("detached recovery moved %q, want %q", got, agent.CanonicalSessionPath(c))
	}
	if got := keeper.HeldPath(); got != agent.CanonicalSessionPath(b) {
		t.Fatalf("foreground keeper moved to %q, want %q", got, agent.CanonicalSessionPath(b))
	}
}

func TestSessionLeaseKeeperTransitionBindsCandidateBeforeMove(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.jsonl")
	b := filepath.Join(dir, "b.jsonl")
	current := agent.NewSession("sys")
	exec := agent.New(nil, nil, current, agent.Options{}, event.Discard)
	ctrl := New(Options{Executor: exec, SessionPath: a, Sink: event.Discard})
	k := NewSessionLeaseKeeper()
	defer k.Release()
	if err := k.Rebind(a); err != nil {
		t.Fatal(err)
	}
	if err := k.BindControllerAuthority(ctrl); err != nil {
		t.Fatal(err)
	}

	candidate := agent.NewSession("sys")
	info := SessionTransitionInfo{OriginalPath: a, TargetPath: b, Reason: "fork", session: candidate}
	if err := k.HandleSessionTransition(info); err != nil {
		t.Fatalf("HandleSessionTransition: %v", err)
	}
	if got := k.HeldPath(); got != agent.CanonicalSessionPath(b) {
		t.Fatalf("HeldPath = %q, want %q", got, agent.CanonicalSessionPath(b))
	}
	if auth := candidate.WriteAuthority(); auth == nil || !auth.Covers(b) {
		t.Fatal("candidate was not bound before transition returned")
	}
	old, err := agent.TryAcquireSessionLease(a)
	if err != nil {
		t.Fatalf("old path remained held: %v", err)
	}
	old.Release()
}

func TestSessionLeaseKeeperTransitionFailureKeepsCurrentOwner(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.jsonl")
	b := filepath.Join(dir, "b.jsonl")
	holder, err := agent.TryAcquireSessionLease(b)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	k := NewSessionLeaseKeeper()
	defer k.Release()
	if err := k.Rebind(a); err != nil {
		t.Fatal(err)
	}
	candidate := agent.NewSession("sys")
	err = k.HandleSessionTransition(SessionTransitionInfo{TargetPath: b, Reason: "switch", session: candidate})
	if !errors.Is(err, agent.ErrSessionLeaseHeld) && !strings.Contains(err.Error(), "in use") {
		t.Fatalf("HandleSessionTransition error = %v, want held", err)
	}
	if got := k.HeldPath(); got != agent.CanonicalSessionPath(a) {
		t.Fatalf("failed transition moved keeper to %q", got)
	}
	if candidate.WriteAuthority() != nil {
		t.Fatal("failed transition bound candidate authority")
	}
}

func TestSessionInUseMessageNamesHolder(t *testing.T) {
	acquired := time.Date(2026, 7, 6, 3, 4, 0, 0, time.UTC)
	err := &agent.SessionLeaseError{
		Path: "/tmp/x.jsonl",
		Info: &agent.SessionLeaseInfo{
			SessionPath: "/tmp/x.jsonl",
			WriterID:    "writer-nonce-should-not-appear",
			PID:         12345,
			Hostname:    "devbox",
			AcquiredAt:  acquired,
		},
	}
	msg := SessionInUseMessage(err)
	if !strings.Contains(msg, "another Reasonix process") {
		t.Fatalf("message %q missing holder wording", msg)
	}
	if !strings.Contains(msg, "pid 12345") || !strings.Contains(msg, "on devbox") {
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
		"nil info":   &agent.SessionLeaseError{Path: "/tmp/x.jsonl"},
		"plain held": agent.ErrSessionLeaseHeld,
		"zero pid":   &agent.SessionLeaseError{Info: &agent.SessionLeaseInfo{PID: 0}},
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

func TestSessionLeaseKeeperRebindDetachingIsFailureAtomic(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "current.jsonl")
	target := filepath.Join(dir, "target.jsonl")
	keeper := NewSessionLeaseKeeper()
	defer keeper.Release()
	if err := keeper.Rebind(current); err != nil {
		t.Fatal(err)
	}
	outside, err := agent.TryAcquireSessionLease(target)
	if err != nil {
		t.Fatal(err)
	}
	defer outside.Release()

	detached, err := keeper.RebindDetaching(target)
	if !errors.Is(err, agent.ErrSessionLeaseHeld) || detached != nil {
		t.Fatalf("RebindDetaching = (%v, %v), want held error and nil keeper", detached, err)
	}
	if got, want := keeper.HeldPath(), agent.CanonicalSessionPath(current); got != want {
		t.Fatalf("held path = %q, want unchanged %q", got, want)
	}
}

func TestSessionLeaseKeeperSplitAndAdopt(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "current.jsonl")
	target := filepath.Join(dir, "target.jsonl")
	keeper := NewSessionLeaseKeeper()
	defer keeper.Release()
	if err := keeper.Rebind(current); err != nil {
		t.Fatal(err)
	}
	detached, err := keeper.RebindDetaching(target)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := detached.HeldPath(), agent.CanonicalSessionPath(current); got != want {
		t.Fatalf("detached path = %q, want %q", got, want)
	}
	if got, want := keeper.HeldPath(), agent.CanonicalSessionPath(target); got != want {
		t.Fatalf("foreground path = %q, want %q", got, want)
	}
	foreground := keeper.Split()
	defer foreground.Release()
	keeper.Adopt(detached)
	if got, want := keeper.HeldPath(), agent.CanonicalSessionPath(current); got != want {
		t.Fatalf("adopted path = %q, want %q", got, want)
	}
	if got := detached.HeldPath(); got != "" {
		t.Fatalf("source keeper still holds %q", got)
	}
}
