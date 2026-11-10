package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/eventwire"
)

// A wider answer to the write-outside-the-scope question covers a folder, so the
// terminal's choices name that folder rather than the internal tool id.
func TestWriteScopeChoicesNameTheFolderTheyCover(t *testing.T) {
	m, _ := testModel(t)
	file := filepath.Join("/srv", "app", "wsl.conf")
	apply(m, eventwire.Event{Kind: "approval_request", Approval: &eventwire.Approval{
		ID: "w1", Tool: "write_scope", Fresh: true, Subject: file, AllowsSession: true, AllowsPersist: true,
		Reason: "This file is outside the folders the agent may write to.",
	}})
	it := m.tr.OpenPrompt()
	if it == nil {
		t.Fatal("the approval did not open a prompt")
	}
	var wide []string
	for _, c := range m.approvalChoices(it.Approval) {
		if c.session || c.persist {
			wide = append(wide, c.label)
		}
	}
	if len(wide) != 2 {
		t.Fatalf("wider answers = %v, want the session and the recorded one", wide)
	}
	for _, label := range wide {
		if !strings.Contains(label, filepath.Join("/srv", "app")) || strings.Contains(label, "write_scope") {
			t.Errorf("choice %q should name the folder and not the internal tool", label)
		}
	}
}
