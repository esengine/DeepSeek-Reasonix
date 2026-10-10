package boot

import (
	"context"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent/testutil"
)

// A rule that names no tool is reported at load with its list and rule as a
// typed payload; a shell-command rule, an MCP-namespace rule and a rule on a
// built-in tool say nothing.
func TestEffectPermissionRulesNamingNoToolAreReportedAtLoad(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))
	writeUserConfig(t, userModel+"\n[permissions]\nask = [\"rm\", \"Bash(git push:*)\", \"mcp__later__tool\"]\ndeny = [\"git reset\", \"read_file(*.env*)\"]\n")
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("rules"))
	var notices []event.Event
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Code == event.NoticeCodePermissionRulesDormant {
			notices = append(notices, e)
		}
	})
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if len(notices) != 1 {
		t.Fatalf("notices = %v, want one", notices)
	}
	want := event.PermissionRulesDormant{Rules: []event.DormantPermissionRule{
		{List: "deny", Rule: "git reset", Tool: "git reset"}, {List: "ask", Rule: "rm", Tool: "rm"},
	}}.Encode()
	if notices[0].Detail != want {
		t.Fatalf("payload = %s, want %s", notices[0].Detail, want)
	}
}
