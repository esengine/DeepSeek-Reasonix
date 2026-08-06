package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/sessiontemp"
)

func TestWithSubagentSessionTempIsIndependent(t *testing.T) {
	parent := sessiontemp.NewWithRoot(t.TempDir())
	parent.Retain()
	defer parent.Release()
	parentLease, err := parent.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	parentDir := parentLease.Dir()
	if err := os.WriteFile(filepath.Join(parentDir, "parent.txt"), []byte("p"), 0o600); err != nil {
		t.Fatal(err)
	}
	parentLease.Release()

	ctx := sessiontemp.WithManager(context.Background(), parent)
	childCtx, releaseChild := withSubagentSessionTemp(ctx)
	defer releaseChild()

	child := sessiontemp.FromContext(childCtx)
	if child == nil || child == parent {
		t.Fatal("sub-agent must install a distinct SessionTemp Manager")
	}
	if sessiontemp.FromContext(childCtx) != child {
		t.Fatal("FromContext should return child manager")
	}

	childLease, err := child.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	childDir := childLease.Dir()
	if childDir == parentDir {
		t.Fatal("child temporary directory must not equal parent")
	}
	if _, err := os.Stat(filepath.Join(childDir, "parent.txt")); !os.IsNotExist(err) {
		t.Fatalf("child must not see parent temp files: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parentDir, "parent.txt")); err != nil {
		t.Fatalf("parent temp lost: %v", err)
	}
	childLease.Release()

	// Nested sibling also gets a fresh manager.
	sibCtx, releaseSib := withSubagentSessionTemp(ctx)
	defer releaseSib()
	sib := sessiontemp.FromContext(sibCtx)
	if sib == nil || sib == child || sib == parent {
		t.Fatal("sibling sub-agent must get its own Manager")
	}
}

func TestContinueFromGetsFreshTempDirectory(t *testing.T) {
	// continue_from restores conversation only; each RunSubAgentWithSession
	// call installs a new Manager via withSubagentSessionTemp.
	ctx1, release1 := withSubagentSessionTemp(context.Background())
	m1 := sessiontemp.FromContext(ctx1)
	lease1, err := m1.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	dir1 := lease1.Dir()
	lease1.Release()
	release1() // seals m1

	ctx2, release2 := withSubagentSessionTemp(context.Background())
	defer release2()
	m2 := sessiontemp.FromContext(ctx2)
	if m2 == m1 {
		t.Fatal("second run must not reuse first Manager")
	}
	lease2, err := m2.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease2.Release()
	if lease2.Dir() == dir1 {
		t.Fatal("second run must get a new temporary directory")
	}
	if !m1.Sealed() {
		t.Fatal("first run's manager should be sealed after release")
	}
}
