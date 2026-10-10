package boot

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/safety/permission"
	"reasonix/internal/session/control"
)

// A tool that exists only where assembly adds it (system_one without a
// decision model, the planner's and reviewer's exits, one outside tools.enabled)
// is a real tool: its rule is neither reported at load nor refused on save.
func TestEffectPermissionRulesOnConditionalToolsAreNotDormant(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))
	rules := []string{"system_one", "submit_plan", "conclude_no_changes", "review_report", "write_file"}
	writeUserConfig(t, userModel+"\n[tools]\nenabled = [\"read_file\"]\n[permissions]\ndeny = [\"system_one\", \"submit_plan\"]\n")
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
	for _, n := range notices {
		t.Errorf("reported dormant at load: %s", n.Detail)
	}
	if err := ctrl.SavePermissionRules(control.PermissionLists{Mode: "ask", Deny: rules}); errors.Is(err, permission.ErrUnknownTool) {
		t.Errorf("save refused a real tool: %v", err)
	}
	vocab := ctrl.PermissionVocabulary()
	reg := tool.NewRegistry()
	agent.AttachReviewReportTool(reg, agent.ReviewReportGrant{})
	for _, name := range append(agent.PlannerToolRegistry(nil).Names(), reg.Names()...) {
		if err := vocab.Check("deny", name); err != nil {
			t.Errorf("sub-registry tool %q is not in the vocabulary: %v", name, err)
		}
	}
}
