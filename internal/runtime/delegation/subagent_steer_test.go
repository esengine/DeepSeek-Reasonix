package delegation

import (
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
)

type fakeSteerer struct {
	open  bool
	user  []string
	host  []string
	calls int
	raw   []string
}

func (f *fakeSteerer) SteerWithContext(text, userText string) bool {
	f.raw = append(f.raw, userText)
	return f.Steer(text)
}

func (f *fakeSteerer) Steer(text string) bool {
	f.calls++
	if f.open {
		f.user = append(f.user, text)
	}
	return f.open
}

func (f *fakeSteerer) SteerHostNotice(text string) bool {
	f.calls++
	if f.open {
		f.host = append(f.host, text)
	}
	return f.open
}

func steerFixture(t *testing.T, limits SteerLimits) (*SubagentStore, SubagentSpec, *SubagentRun, *fakeSteerer, *time.Time) {
	t.Helper()
	store, spec := viewFixture(t)
	now := time.Unix(1_760_000_000, 0)
	store.WithSteerLimits(func() SteerLimits { return limits })
	store.steer.now = func() time.Time { return now }
	run := startedRun(t, store, spec, said(provider.RoleUser, "task"))
	t.Cleanup(run.Release)
	st := &fakeSteerer{open: true}
	store.attachSteerer(run.Session, st)
	return store, spec, run, st, &now
}

func TestGuidanceReachesTheRunningChildFramedByWhoSentIt(t *testing.T) {
	store, spec, run, st, _ := steerFixture(t, SteerLimits{})
	if err := store.Steer(run.Ref, spec.ParentSession, spec.WorkspaceRoot, "use the cache", SteerFromUser); err != nil {
		t.Fatalf("user steer: %v", err)
	}
	if err := store.Steer(run.Ref, spec.ParentSession, spec.WorkspaceRoot, "stop reading tests", SteerFromParent); err != nil {
		t.Fatalf("parent steer: %v", err)
	}
	if len(st.user) != 1 || st.user[0] != "use the cache" {
		t.Fatalf("user guidance = %q", st.user)
	}
	if len(st.host) != 1 || !strings.HasPrefix(st.host[0], "From the parent session") || !strings.Contains(st.host[0], "stop reading tests") {
		t.Fatalf("parent guidance = %q, want it labelled as the parent's", st.host)
	}
}

func TestGuidanceIsRefusedWhenNothingCanReadIt(t *testing.T) {
	store, spec, run, st, _ := steerFixture(t, SteerLimits{})
	st.open = false
	if err := store.Steer(run.Ref, spec.ParentSession, spec.WorkspaceRoot, "late", SteerFromUser); !errors.Is(err, ErrSubagentNotRunning) {
		t.Fatalf("a child between runs = %v, want ErrSubagentNotRunning", err)
	}
	st.open = true
	other := spec
	other.ParentSession = "someone-else"
	if err := store.Steer(run.Ref, other.ParentSession, spec.WorkspaceRoot, "hi", SteerFromUser); !errors.Is(err, ErrSubagentNotOwned) {
		t.Fatalf("another conversation = %v, want ErrSubagentNotOwned", err)
	}
	finished := startedRun(t, store, spec, said(provider.RoleUser, "b"))
	if err := store.SaveCompleted(finished); err != nil {
		t.Fatal(err)
	}
	finished.Release()
	if err := store.Steer(finished.Ref, spec.ParentSession, spec.WorkspaceRoot, "hi", SteerFromUser); !errors.Is(err, ErrSubagentNotRunning) {
		t.Fatalf("a finished run = %v, want ErrSubagentNotRunning", err)
	}
	if err := store.Steer("sa_0000000000000000", spec.ParentSession, spec.WorkspaceRoot, "hi", SteerFromUser); !errors.Is(err, ErrSubagentNotFound) {
		t.Fatalf("an unknown ref = %v, want ErrSubagentNotFound", err)
	}
}

func TestGuidanceHasANamedSizeAndRateLimitThatZeroLifts(t *testing.T) {
	store, spec, run, st, now := steerFixture(t, SteerLimits{MaxBytes: 8, PerMinute: 2})
	send := func(text string) error {
		return store.Steer(run.Ref, spec.ParentSession, spec.WorkspaceRoot, text, SteerFromUser)
	}
	if err := send("123456789"); !errors.Is(err, ErrSteerTooLarge) {
		t.Fatalf("9 bytes against a limit of 8 = %v, want ErrSteerTooLarge", err)
	}
	if err := send("   "); !errors.Is(err, ErrSteerEmpty) {
		t.Fatalf("blank = %v, want ErrSteerEmpty", err)
	}
	if st.calls != 0 {
		t.Fatalf("a refused message reached the child (%d calls)", st.calls)
	}
	if send("a") != nil || send("b") != nil {
		t.Fatal("the first two within the minute should be admitted")
	}
	if err := send("c"); !errors.Is(err, ErrSteerRateLimited) {
		t.Fatalf("third within a minute = %v, want ErrSteerRateLimited", err)
	}
	*now = now.Add(61 * time.Second)
	if err := send("d"); err != nil {
		t.Fatalf("after the minute: %v", err)
	}

	unlimited, uspec, urun, _, _ := steerFixture(t, SteerLimits{})
	for i := range 50 {
		if err := unlimited.Steer(urun.Ref, uspec.ParentSession, uspec.WorkspaceRoot, strings.Repeat("x", 1<<16), SteerFromUser); err != nil {
			t.Fatalf("zero limits refused message %d: %v", i, err)
		}
	}
}
