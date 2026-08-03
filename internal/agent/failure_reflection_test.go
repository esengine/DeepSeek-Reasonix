package agent

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestFailedToolNames(t *testing.T) {
	calls := []provider.ToolCall{
		{Name: "bash"},
		{Name: "read_file"},
		{Name: "edit_file"},
	}
	outcomes := []toolOutcome{
		{errMsg: ""},                      // success
		{errMsg: "no such file"},          // failure
		{errMsg: "blocked by permission"}, // blocked counts as failure
	}
	got := failedToolNames(calls, outcomes)
	if len(got) != 2 || got[0] != "read_file" || got[1] != "edit_file" {
		t.Fatalf("failedToolNames = %v, want [read_file edit_file]", got)
	}
	// Duplicate failures of the same tool collapse to one name.
	calls = []provider.ToolCall{{Name: "bash"}, {Name: "bash"}}
	outcomes = []toolOutcome{{errMsg: "boom"}, {errMsg: "boom again"}}
	if got := failedToolNames(calls, outcomes); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("de-duplicated failedToolNames = %v, want [bash]", got)
	}
	// All-success batch yields nothing.
	if got := failedToolNames([]provider.ToolCall{{Name: "bash"}}, []toolOutcome{{errMsg: ""}}); len(got) != 0 {
		t.Fatalf("all-success failedToolNames = %v, want empty", got)
	}
}

func TestToolFailureReflectionMessage(t *testing.T) {
	msg := toolFailureReflectionMessage([]string{"bash", "edit_file"})
	for _, want := range []string{"bash", "edit_file", "Analyse the failures", "Do not blindly retry"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("reflection message %q missing %q", msg, want)
		}
	}
	// Edit-family failures add the re-read guidance so the model doesn't
	// retry blind against a stale old_string.
	if !strings.Contains(msg, "re-read that file") {
		t.Fatalf("edit failure should carry re-read guidance: %q", msg)
	}
	// Non-edit failures keep the generic nudge only.
	if got := toolFailureReflectionMessage([]string{"bash"}); strings.Contains(got, "re-read that file") {
		t.Fatalf("bash failure should not carry edit guidance: %q", got)
	}
}

func TestHostReflectionText(t *testing.T) {
	msg := hostReflectionMessage([]string{"bash"})
	if !strings.HasPrefix(msg, HostReflectionPrefix) {
		t.Fatalf("host reflection message should carry the marker prefix: %q", msg)
	}
	stripped, ok := HostReflectionText(msg)
	if !ok {
		t.Fatalf("HostReflectionText should recognize the marker")
	}
	if strings.Contains(stripped, HostReflectionPrefix) || !strings.Contains(stripped, "bash") {
		t.Fatalf("stripped reflection = %q, want guidance text without the marker", stripped)
	}
	// Plain user content is not a reflection.
	if _, ok := HostReflectionText("implement the parser"); ok {
		t.Fatalf("plain user content must not be treated as a reflection")
	}
}
