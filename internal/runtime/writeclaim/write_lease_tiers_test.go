package writeclaim

import (
	"context"
	"testing"

	"reasonix/internal/base/testenv"
)

// The write-lease tiers reach the in-session scheduler, not only the
// cross-session lease: strict holds the whole workspace for a writer that
// declared nothing; relaxed stops that hold, and off takes no claim at all.

func TestStrictTierHoldsWholeWorkspaceForUndeclaredWriter(t *testing.T) {
	root := testenv.TempDir(t)
	claim, err := UndeclaredWriterClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	if !claim.WholeWorkspace {
		t.Fatalf("strict: undeclared claim = %+v, want the whole workspace", claim)
	}
}

func TestRelaxedTierDropsTheHoldButKeepsDeclaredClaimsExcluding(t *testing.T) {
	SetWriteLeaseTiers(true, false)
	t.Cleanup(func() { SetWriteLeaseTiers(false, false) })
	root := testenv.TempDir(t)

	claim, err := UndeclaredWriterClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	if !claim.Empty() {
		t.Fatalf("relaxed: undeclared claim = %+v, want nothing", claim)
	}

	declared, err := NormalizeWritePaths(root, []string{"a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	s := NewSubagentScheduler(4, 4)
	release, err := s.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: declared})
	if err != nil {
		t.Fatalf("acquire holder: %v", err)
	}
	defer release()
	if err := s.TryClaimWritePaths(declared); err == nil {
		t.Fatal("relaxed: two declared claims on one path must still exclude each other")
	}
}

func TestOffTierTakesNoClaimFromAnyone(t *testing.T) {
	SetWriteLeaseTiers(true, true)
	t.Cleanup(func() { SetWriteLeaseTiers(false, false) })
	root := testenv.TempDir(t)

	held, err := WholeWorkspaceWriteClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	declared, err := NormalizeWritePaths(root, []string{"a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	s := NewSubagentScheduler(4, 4)
	release, err := s.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: held})
	if err != nil {
		t.Fatalf("acquire holder: %v", err)
	}
	defer release()

	if err := s.TryClaimWritePaths(declared); err != nil {
		t.Fatalf("off: a declared claim was refused: %v", err)
	}
	done, err := s.ReserveWrite(declared)
	if err != nil {
		t.Fatalf("off: a declared reservation was refused: %v", err)
	}
	done()

	// Back to strict: the same overlap is refused again, so the tier really is
	// what lifted it.
	SetWriteLeaseTiers(false, false)
	if err := s.TryClaimWritePaths(held); err == nil {
		t.Fatal("strict: an overlapping claim must be refused while one is held")
	}
}
