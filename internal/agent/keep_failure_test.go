package agent

import (
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func exitCode(n int) *int { return &n }

// A failing `go test` is the case prefix matching cannot see: the output opens
// with "=== RUN" and the failure only shows in the recorded execution.
func TestKeepErrorsSeesStructuredFailure(t *testing.T) {
	failing := "=== RUN   TestQuotedValues\n    format_test.go:412: expected beta-7d21, got gamma-4a88\n--- FAIL: TestQuotedValues (0.01s)\nFAIL\n"
	for _, tc := range []struct {
		name string
		ex   *provider.ToolExecution
		want bool
	}{
		{"state failed", &provider.ToolExecution{State: tool.ShellStateFailed}, true},
		{"timed out", &provider.ToolExecution{State: tool.ShellStateTimedOut}, true},
		{"non-zero exit", &provider.ToolExecution{State: tool.ShellStateCompleted, ExitCode: exitCode(1)}, true},
		{"verification failed", &provider.ToolExecution{Verification: tool.ShellVerificationFailed}, true},
		{"clean run", &provider.ToolExecution{State: tool.ShellStateCompleted, ExitCode: exitCode(0)}, false},
		{"no execution record", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := provider.Message{Role: provider.RoleTool, Content: failing, ToolExecution: tc.ex}
			if got := isErrorMessage(m); got != tc.want {
				t.Errorf("isErrorMessage = %v, want %v", got, tc.want)
			}
		})
	}
}

// The text fallback still classifies host-formatted failures, and a passing run
// must not be kept just because it mentions a failure in passing.
func TestKeepErrorsTextFallbackUnchanged(t *testing.T) {
	cases := map[string]struct {
		content string
		want    bool
	}{
		"error prefix":      {"error: file not found", true},
		"blocked prefix":    {"blocked: permission denied", true},
		"mentions failure":  {"all tests pass; the earlier FAIL is fixed", false},
		"ordinary output":   {"ok  reasonix/config\t0.2s", false},
		"uppercase prefix":  {"Error: broken", true},
		"prefix mid-string": {"note: error: not at the start", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := provider.Message{Role: provider.RoleTool, Content: tc.content}
			if got := isErrorMessage(m); got != tc.want {
				t.Errorf("isErrorMessage(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

// Only tool results are keep-policy candidates; a failing execution record on
// another role must not promote it.
func TestKeepErrorsIgnoresNonToolRoles(t *testing.T) {
	ex := &provider.ToolExecution{State: tool.ShellStateFailed, ExitCode: exitCode(2)}
	for _, role := range []provider.Role{provider.RoleUser, provider.RoleAssistant, provider.RoleSystem} {
		if isErrorMessage(provider.Message{Role: role, Content: "error: x", ToolExecution: ex}) {
			t.Errorf("role %v classified as an error tool result", role)
		}
	}
}
