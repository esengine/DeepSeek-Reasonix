package isolation

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/safety/evidence"
)

func TestStoppedParentRecordsInterruptionWhileUnsettledChildrenRetainItsGeneration(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "finished", true: "cancelled"}[cancelled], func(t *testing.T) {
			s, parent, children, child := descendantFixture(t)
			write(t, parent.Cand.WorkspaceRoot, "parent.txt", "retained parent\n")
			parent.Evidence = &CandidateEvidence{Verification: "valid", FinishedContent: &evidence.ContentSnapshot{Complete: true}, Checks: []CheckEvidence{{Success: true}}}
			var runErr error
			if cancelled {
				runErr = context.Canceled
			}
			_, err := s.Complete(t.Context(), parent, Outcome{}, runErr)
			if refusalCodeOf(err) != CodeDependentResults || (cancelled && !errors.Is(err, context.Canceled)) {
				t.Fatalf("stopped parent lost dependent or cancellation cause: %v", err)
			}
			d, err := s.managedResultMetadata(parent.ID)
			if err != nil || d.State != Interrupted || d.RunGeneration != parent.RunGeneration || d.Lineage == nil || d.Lineage.Admission != "sealed" {
				t.Fatalf("stopped parent still claims an active run or lost its dependency identity: %+v %v", d, err)
			}
			if d.Evidence == nil || d.Evidence.Verification != "unknown" || d.Evidence.FinishedContent != nil || len(d.Evidence.Checks) != 0 {
				t.Fatal("unsealed parent retained finished verification evidence")
			}
			if _, err := s.Resume(t.Context(), parent.ID, "parent"); refusalCodeOf(err) != CodeDependentResults {
				t.Fatalf("parent resumed before its child settled: %v", err)
			}
			if _, err := children.Finish(t.Context(), child); err != nil {
				t.Fatal(err)
			}
			if _, err := children.Discard(t.Context(), child.ID); err != nil {
				t.Fatal(err)
			}
			next, err := s.Resume(t.Context(), parent.ID, "parent")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Abort(context.Background(), next) })
			if next.RunGeneration == parent.RunGeneration || read(t, next.Cand.WorkspaceRoot, "parent.txt") != "retained parent\n" {
				t.Fatal("parent continuation lost its new generation or retained work")
			}
			if _, err := s.Finish(t.Context(), parent); refusalCodeOf(err) != CodeBusy {
				t.Fatalf("old completion callback ended the new parent run: %v", err)
			}
		})
	}
}

func TestCatalogProjectsAnUnownedRunningRecordAsInterruptedWithoutRewritingIt(t *testing.T) {
	s, parent, _, _ := descendantFixture(t)
	s.endRun(parent)
	catalog, err := s.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range catalog.Results {
		if row.ID == parent.ID {
			found = row.State == Interrupted && row.Activity == "inactive" && len(row.Actions) == 0
		}
	}
	if !found {
		t.Fatalf("crashed parent still appears to run: %+v", catalog)
	}
	d, err := s.managedResultMetadata(parent.ID)
	if err != nil || d.State != Running {
		t.Fatalf("catalog rewrote crash state instead of observing it: %+v %v", d, err)
	}
}
