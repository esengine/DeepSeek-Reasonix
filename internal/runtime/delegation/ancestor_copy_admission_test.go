package delegation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

func ancestorCopyFixture(t *testing.T) (*SubagentStore, string, SubagentSpec) {
	t.Helper()
	dir := t.TempDir()
	s := NewSubagentStore(filepath.Join(dir, "subagents"))
	spec := testSubagentSpec(t, "review")
	spec.ParentSession = "root"
	source, err := s.PrepareFresh(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Release()
	source.Session.Add(provider.Message{Role: provider.RoleUser, Content: "original transcript bytes"})
	if err := s.SaveCompleted(source); err != nil {
		t.Fatal(err)
	}
	saveTestBranchMeta(t, dir, "root", "")
	saveTestBranchMeta(t, dir, "child", "root")
	saveTestBranchMeta(t, dir, "sibling", "root")
	spec.ParentSession = "child"
	return s, source.Ref, spec
}

func TestAncestorCopyAdmissionCoversUnpublishedRun(t *testing.T) {
	for _, firstLegacy := range []bool{false, true} {
		for _, secondLegacy := range []bool{false, true} {
			firstName, secondName := "continue", "continue"
			if firstLegacy {
				firstName = "legacy"
			}
			if secondLegacy {
				secondName = "legacy"
			}
			t.Run(firstName+"_then_"+secondName, func(t *testing.T) {
				s, source, spec := ancestorCopyFixture(t)
				prepare := func(legacy bool) (*SubagentRun, error) {
					if legacy {
						return s.PrepareLegacyForkFrom(source, spec)
					}
					return s.PrepareContinue(source, spec)
				}
				first, err := prepare(firstLegacy)
				if err != nil {
					t.Fatal(err)
				}
				defer first.Release()
				if _, err := os.Stat(s.metaPath(first.Ref)); !os.IsNotExist(err) {
					t.Fatalf("preparation published metadata: %v", err)
				}
				second, err := prepare(secondLegacy)
				if second != nil {
					second.Release()
				}
				if !errors.Is(fmt.Errorf("caller: %w", err), ErrAncestorCopyBusy) {
					t.Fatalf("second admission = %v; first copy has not been published", err)
				}
				if err := s.SaveCompleted(first); err != nil {
					t.Fatal(err)
				}
				first.Release()
				reused, err := prepare(secondLegacy)
				if err != nil {
					t.Fatal(err)
				}
				defer reused.Release()
				if reused.Ref != first.Ref {
					t.Fatalf("continued %s instead of original copy %s", reused.Ref, first.Ref)
				}
			})
		}
	}
}

func TestAncestorCopyAdmissionAllowsDifferentParents(t *testing.T) {
	s, source, spec := ancestorCopyFixture(t)
	first, err := s.PrepareContinue(source, spec)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	spec.ParentSession = "sibling"
	second, err := s.PrepareContinue(source, spec)
	if err != nil {
		t.Fatalf("independent target parent blocked: %v", err)
	}
	defer second.Release()
	if first.Ref == second.Ref || second.Meta.ParentSession != "sibling" {
		t.Fatal("independent target parent did not get its own copy")
	}
}

func TestAncestorCopyAdmissionReleasesAbandonedPreparation(t *testing.T) {
	s, source, spec := ancestorCopyFixture(t)
	first, err := s.PrepareContinue(source, spec)
	if err != nil {
		t.Fatal(err)
	}
	first.Release()
	first.Release()
	second, err := s.PrepareContinue(source, spec)
	if err != nil {
		t.Fatalf("abandoned preparation retained admission: %v", err)
	}
	defer second.Release()
	if second.Ref == first.Ref {
		t.Fatal("unpublished preparation was adopted as persisted state")
	}
}

func TestAncestorCopyAdmissionReleasesAfterInvalidCopy(t *testing.T) {
	s, source, spec := ancestorCopyFixture(t)
	copy, err := s.PrepareFresh(spec)
	if err != nil {
		t.Fatal(err)
	}
	copy.Meta.ForkedFrom = source
	if err := s.SaveFailed(copy); err != nil {
		t.Fatal(err)
	}
	copy.Release()
	if run, err := s.PrepareContinue(source, spec); err == nil || !strings.Contains(err.Error(), "failed") {
		if run != nil {
			run.Release()
		}
		t.Fatalf("failed existing copy admission = %v", err)
	}
	// Repair only the fixture, to check that the failed lookup released its
	// transient reservation. Production does not recover failed transcripts.
	meta, err := s.LoadMeta(copy.Ref)
	if err != nil {
		t.Fatal(err)
	}
	meta.Status = sessionstore.SubagentCompleted
	if err := s.saveMeta(meta); err != nil {
		t.Fatal(err)
	}
	run, err := s.PrepareContinue(source, spec)
	if err != nil {
		t.Fatalf("failed preparation retained admission: %v", err)
	}
	run.Release()
}

func TestAncestorCopyAdmissionAllowsDifferentSources(t *testing.T) {
	s, source, spec := ancestorCopyFixture(t)
	otherSpec := spec
	otherSpec.ParentSession = "root"
	other, err := s.PrepareFresh(otherSpec)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCompleted(other); err != nil {
		t.Fatal(err)
	}
	other.Release()
	first, err := s.PrepareContinue(source, spec)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := s.PrepareContinue(other.Ref, spec)
	if err != nil {
		t.Fatalf("different ancestor source was blocked: %v", err)
	}
	second.Release()
}

func TestAncestorCopyAdmissionUsesNearestSourceIdentity(t *testing.T) {
	s, source, spec := ancestorCopyFixture(t)
	child, err := s.PrepareContinue(source, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCompleted(child); err != nil {
		t.Fatal(err)
	}
	child.Release()
	saveTestBranchMeta(t, filepath.Dir(s.dir), "grandchild", "child")
	spec.ParentSession = "grandchild"
	first, err := s.PrepareContinue(source, spec)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := s.PrepareContinue(child.Ref, spec)
	if second != nil {
		second.Release()
	}
	if !errors.Is(err, ErrAncestorCopyBusy) {
		t.Fatalf("alias of nearest ancestor bypassed admission: %v", err)
	}
}

func TestAncestorCopyAdmissionReleasesAfterBusyCurrentReference(t *testing.T) {
	s, source, spec := ancestorCopyFixture(t)
	copy, err := s.PrepareContinue(source, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCompleted(copy); err != nil {
		t.Fatal(err)
	}
	copy.Release()
	direct, err := s.PrepareContinue(copy.Ref, spec)
	if err != nil {
		t.Fatal(err)
	}
	defer direct.Release()
	if run, err := s.PrepareContinue(source, spec); err == nil || !strings.Contains(err.Error(), "already") {
		if run != nil {
			run.Release()
		}
		t.Fatalf("busy current copy admission = %v", err)
	}
	direct.Release()
	viaAncestor, err := s.PrepareContinue(source, spec)
	if err != nil {
		t.Fatalf("busy-ref failure retained ancestor reservation: %v", err)
	}
	defer viaAncestor.Release()
	if viaAncestor.Ref != copy.Ref {
		t.Fatal("ancestor retry created another copy")
	}
}
