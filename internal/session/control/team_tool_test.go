package control

import (
	"errors"
	"fmt"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/session/team"
	"reasonix/internal/state/teamroster"
)

func TestStricterApprovalKeepsTheModeThatDoesLessUnattended(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"", "", ""},
		{"", "auto", "auto"},
		{"yolo", "", "yolo"},
		{"yolo", "ask", "ask"},
		{"auto", "yolo", "auto"},
		{"ask", "dontAsk", "dontAsk"},
		{"readOnly", "dontAsk", "readOnly"},
		{"auto", "readOnly", "readOnly"},
	}
	for _, tc := range cases {
		if got := stricterApproval(tc.a, tc.b); got != tc.want {
			t.Errorf("stricterApproval(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestTeamErrorsReachTheModelUnderTheirIdentity(t *testing.T) {
	cases := map[error]string{
		team.ErrUpstream: "team.upstream",
		fmt.Errorf("wrapped: %w", team.ErrNotMain):                        "team.not_main",
		teamroster.ErrNameTaken:                                           "team.name_taken",
		&teamroster.FieldError{Field: "name", Err: teamroster.ErrTooLong}: "team.too_long",
		fmt.Errorf("%w: boom", team.ErrUnavailable):                       "team.unavailable",
	}
	for err, code := range cases {
		var r tool.Refusal
		if got := teamRefusal(err); !errors.As(got, &r) || r.Code != code {
			t.Errorf("teamRefusal(%v) = %v, want code %s", err, got, code)
		}
	}
	internal := errors.New("disk full")
	var r tool.Refusal
	if got := teamRefusal(internal); !errors.Is(got, internal) || errors.As(got, &r) {
		t.Fatalf("an error the team does not name was given an identity: %v", got)
	}
	if teamRefusal(nil) != nil {
		t.Fatal("nil became an error")
	}
}

func TestATurnThatWroteNothingHasNoFinalText(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "first"},
		{Role: provider.RoleAssistant, Content: "first answer"},
		{Role: provider.RoleUser, Content: "second"},
		{Role: provider.RoleAssistant, Content: "", ToolCalls: []provider.ToolCall{{ID: "1", Name: "bash"}}},
		{Role: provider.RoleTool, Content: "output"},
	}
	if got := turnFinalText(msgs); got != "" {
		t.Fatalf("a turn without an answer reported %q", got)
	}
	msgs = append(msgs, provider.Message{Role: provider.RoleAssistant, Content: "second answer"})
	if got := turnFinalText(msgs); got != "second answer" {
		t.Fatalf("final text = %q", got)
	}
}
