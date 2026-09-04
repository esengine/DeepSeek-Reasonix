package control

import (
	"testing"

	"reasonix/internal/command"
)

func TestClassifySubmitRouteSeparatesManagementFromTurns(t *testing.T) {
	c := New(Options{Commands: []command.Command{{Name: "review", Body: "review: $ARGUMENTS"}}})
	tests := []struct {
		input string
		want  SubmitDisposition
	}{
		{input: "/compact", want: SubmitManagementHandled},
		{input: "/new", want: SubmitManagementHandled},
		{input: "/context", want: SubmitManagementHandled},
		{input: "/model", want: SubmitManagementHandled},
		{input: "/provider deepseek", want: SubmitManagementHandled},
		{input: "/memory recall", want: SubmitManagementHandled},
		{input: "/remember keep this", want: SubmitManagementHandled},
		{input: "# keep this", want: SubmitManagementHandled},
		{input: "/goal status", want: SubmitManagementHandled},
		{input: "/goal pause", want: SubmitManagementHandled},
		{input: "/docs", want: SubmitManagementHandled},
		{input: "/plan-exec", want: SubmitManagementHandled},
		{input: "/prometheus", want: SubmitManagementHandled},
		{input: "/compactly", want: SubmitTurnStarted},
		{input: "review the diff", want: SubmitTurnStarted},
		{input: "/goal ship the fix", want: SubmitTurnStarted},
		{input: "/docs explain compaction", want: SubmitTurnStarted},
		{input: "/prometheus ship the fix", want: SubmitTurnStarted},
		{input: "/review the diff", want: SubmitTurnStarted},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := c.ClassifySubmitRoute(tt.input); got != tt.want {
				t.Fatalf("ClassifySubmitRoute(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestManagementNoticeLeavesTurnOwnedCommandsUnclaimed(t *testing.T) {
	c := New(Options{Commands: []command.Command{{Name: "review", Body: "review: $ARGUMENTS"}}})
	for _, input := range []string{"/unknown", "/review inspect", "/compactly"} {
		if c.managementNotice(input) {
			t.Fatalf("managementNotice(%q) claimed a turn-owned command", input)
		}
	}
}
