package tool_test

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
	_ "reasonix/internal/tools/builtin"
)

// A tool that can be out of context says what would put it back in, and says
// it under an identity. Without the words the model is left finding out by
// trial, as a real session did against three different gates; without the code
// every reader downstream classifies the words instead, which is the same
// failure one layer up.
func TestContextualToolsExplainThemselves(t *testing.T) {
	ctx := context.Background()
	var checked int
	for _, target := range tool.Builtins() {
		if _, ok := target.(tool.ContextualTool); !ok {
			continue
		}
		checked++
		r, ok := target.(tool.ContextualReasoner)
		if !ok {
			t.Errorf("%s is a ContextualTool but cannot say why it is unavailable", target.Name())
			continue
		}
		refusal := r.Unavailable(ctx)
		if strings.TrimSpace(refusal.Message) == "" {
			t.Errorf("%s returned a blank reason", target.Name())
		}
		if refusal.Code == "" {
			t.Errorf("%s refuses without an identity; a reader can only match its words", target.Name())
		}
	}
	if checked == 0 {
		t.Fatal("no contextual tools found — the registry or this test stopped seeing them")
	}
}

// The codes are pinned rather than derived: nothing about a tool's name says
// which host fact stops it, and two tools sharing a code is a claim that one
// predicate stops both. Changing this table is where that claim gets made — a
// new tool quietly reusing goal.no_active_turn for a different fact would
// otherwise read as classified when it is only labelled.
func TestContextualBuiltinRefusalCodesArePinned(t *testing.T) {
	want := map[string]string{
		"bash_output":   "jobs.no_job_context",
		"kill_shell":    "jobs.no_job_context",
		"wait":          "jobs.no_job_context",
		"complete_step": "plan.mode_active",
		"update_goal":   "goal.no_active_turn",
	}
	got := map[string]string{}
	for _, target := range tool.Builtins() {
		r, ok := target.(tool.ContextualReasoner)
		if !ok {
			continue
		}
		got[target.Name()] = r.Unavailable(context.Background()).Code
	}
	for name, code := range want {
		if got[name] != code {
			t.Errorf("%s refusal code = %q, want %q", name, got[name], code)
		}
	}
	for name, code := range got {
		if _, declared := want[name]; !declared {
			t.Errorf("contextual builtin %s refuses as %q but is not in the pinned table", name, code)
		}
	}
}
