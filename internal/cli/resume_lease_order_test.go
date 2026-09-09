package cli

import (
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

func TestBindAndLoadCLIResumeHoldsLeaseBeforeReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume.jsonl")
	saveTestSession(t, path, "latest prompt")

	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	loaded, err := bindAndLoadCLIResume(leases, path, func(candidate string) (*agent.Session, error) {
		contender, acquireErr := agent.TryAcquireSessionLease(candidate)
		if contender != nil {
			contender.Release()
		}
		if !errors.Is(acquireErr, agent.ErrSessionLeaseHeld) {
			t.Fatalf("loader observed lease error %v, want held", acquireErr)
		}
		return agent.LoadSession(candidate)
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("resume returned a nil session")
	}
}

func TestBindAndLoadCLIResumeDoesNotReadHeldSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "held.jsonl")
	holder, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()

	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	called := false
	_, err = bindAndLoadCLIResume(leases, path, func(string) (*agent.Session, error) {
		called = true
		return nil, nil
	})
	if !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("bindAndLoadCLIResume error = %v, want held", err)
	}
	if called {
		t.Fatal("held session was read before takeover acquired its lease")
	}
}

func TestCommitSessionSwitchAcquiresTargetBeforeLoading(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.jsonl")
	target := filepath.Join(dir, "target.jsonl")
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(source); err != nil {
		t.Fatal(err)
	}
	m := &chatTUI{leases: leases}
	wantErr := errors.New("stop after ownership check")
	err := m.commitSessionSwitchWithLoader(target, func(candidate string) (*agent.Session, error) {
		contender, acquireErr := agent.TryAcquireSessionLease(candidate)
		if contender != nil {
			contender.Release()
		}
		if !errors.Is(acquireErr, agent.ErrSessionLeaseHeld) {
			t.Fatalf("switch loader observed lease error %v, want held", acquireErr)
		}
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("commitSessionSwitch error = %v, want %v", err, wantErr)
	}
	if got := leases.HeldPath(); got != agent.CanonicalSessionPath(source) {
		t.Fatalf("source lease was not restored after load failure: %q", got)
	}
}
