package cli

import (
	"strings"
	"testing"

	"reasonix/internal/event"
)

func TestWriteAccessApprovalChoicesPreserveScopes(t *testing.T) {
	got := approvalChoices(&event.Approval{
		Tool: "bash", Kind: event.ApprovalKindWriteAccess,
		WriteAccess: &event.WriteAccessApproval{Directories: []string{"/tmp/out"}, PersistAllowed: true},
	})
	want := []approvalChoice{
		{allow: true},
		{allow: true, allowForSession: true},
		{allow: true, allowForSession: true, persistToConfig: true},
		{},
	}
	if len(got) != len(want) {
		t.Fatalf("write-access choices = %d, want %d", len(got), len(want))
	}
	for i := range got {
		got[i].label = ""
		if got[i] != want[i] {
			t.Fatalf("write-access choice %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	labels := approvalChoiceLabels(&event.Approval{Kind: event.ApprovalKindWriteAccess})
	if len(labels) != 4 || !strings.Contains(strings.ToLower(labels[2]), "project") {
		t.Fatalf("write-access labels = %v", labels)
	}
}
