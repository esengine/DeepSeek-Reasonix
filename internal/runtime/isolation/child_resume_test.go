package isolation

import (
	"bytes"
	"context"
	"os"
	"testing"

	"reasonix/internal/state/mutation"
)

func TestChildResumeRequiresLiveRegisteredDirectParent(t *testing.T) {
	for _, mode := range []string{"valid", "stopped_parent", "sealed_parent", "stale_parent", "unregistered", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s, parent, children, child := descendantFixture(t)
			write(t, child.Cand.WorkspaceRoot, "retained.txt", "resume input\n")
			if _, err := children.Finish(t.Context(), child); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "stopped_parent":
				s.endRun(parent)
			case "sealed_parent":
				if err := s.SealChildren(ctx, parent); refusalCodeOf(err) != CodeDependentResults {
					t.Fatal(err)
				}
			case "stale_parent":
				stale := *parent
				stale.RunGeneration = "retired"
				parent = &stale
			case "unregistered":
				var err error
				child, err = children.BeginOwned(ctx, parent.Cand.WorkspaceRoot, "child")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := children.Finish(ctx, child); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				cancel()
			}
			resumed, err := s.ResumeChild(ctx, parent, children, child.ID, "child")
			if mode != "valid" {
				if err == nil {
					children.endRun(resumed)
					t.Fatal("child resumed without its registered live parent authority")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { children.Abort(context.Background(), resumed) })
			if resumed.RunGeneration == child.RunGeneration || !matchesParentResult(resumed.Lineage.Parent, parent, child.Lineage.Parent.Token) {
				t.Fatal("child continuation lost its generation or direct parent identity")
			}
			if got := read(t, resumed.Cand.WorkspaceRoot, "retained.txt"); got != "resume input\n" {
				t.Fatalf("continuation reset retained content: %q", got)
			}
		})
	}
}

func TestChildResumeRechecksParentAdmissionAfterResourceWait(t *testing.T) {
	s, parent, children, child := descendantFixture(t)
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	waiting := make(chan struct{}, 1)
	coordinator, err := mutation.New(children.workspace, t.TempDir(), func(notice mutation.Notice) {
		if notice.CallID == "isolation.resume" && notice.State == "waiting" {
			select {
			case waiting <- struct{}{}:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	children.WithMutationCoordinator(coordinator)
	lease, err := coordinator.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{child.Cand.WorktreeRoot}, CallID: "hold.resume"})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.End(mutation.NotExecuted)
	before, err := os.ReadFile(entryRecordPath(child))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		resumed, err := s.ResumeChild(t.Context(), parent, children, child.ID, "child")
		if resumed != nil {
			children.endRun(resumed)
		}
		done <- err
	}()
	<-waiting
	if err := s.SealChildren(t.Context(), parent); refusalCodeOf(err) != CodeDependentResults {
		lease.End(mutation.NotExecuted)
		<-done
		t.Fatal(err)
	}
	lease.End(mutation.NotExecuted)
	if err := <-done; refusalCodeOf(err) != CodeDependentResults {
		t.Fatalf("waiting child acquired a new run after its parent sealed: %v", err)
	}
	after, err := os.ReadFile(entryRecordPath(child))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rejected continuation changed child generation or content: %v", err)
	}
}
