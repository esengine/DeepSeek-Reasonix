package delegation

import (
	"reasonix/internal/contract/provider"
	"testing"
)

func TestTranscriptTailIsBoundedAndPagesBackWithoutGaps(t *testing.T) {
	s := mustSubagentStore(t)
	spec := testSubagentSpec(t, "tail")
	run := startedRun(t, s, spec)
	for range 12 {
		run.Session.Add(said(provider.RoleAssistant, "entry"))
	}
	if err := s.SaveCompleted(run); err != nil {
		t.Fatal(err)
	}
	run.Release()
	page, err := s.Transcript(run.Ref, spec.ParentSession, spec.WorkspaceRoot, -1, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 4 || page.After != page.Total-4 || page.Next != page.Total {
		t.Fatalf("tail = %+v", page)
	}
	previous, err := s.Transcript(run.Ref, spec.ParentSession, spec.WorkspaceRoot, page.After-4, 4)
	if err != nil || previous.Next != page.After {
		t.Fatalf("previous = %+v, %v", previous, err)
	}
}
