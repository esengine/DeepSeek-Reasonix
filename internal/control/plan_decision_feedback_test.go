package control

import (
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestResolvePlanDecisionStagesRevisionBeforeAnswer(t *testing.T) {
	session := agent.NewSession("sys")
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "proposed plan"})
	c := New(Options{Executor: agent.New(nil, nil, session, agent.Options{}, event.Discard), WorkspaceRoot: t.TempDir()})
	c.SetSessionPath(filepath.Join(c.workspaceRoot, "session.jsonl"))
	id, reply := c.approval.registerDecisionKind(planApprovalTool, "", "", true, false, "plan", nil)
	if err := c.ResolvePlanDecisionWithFeedback(id, PlanDecisionRevisePlan, "cover the rollback path"); err != nil {
		t.Fatal(err)
	}
	if got := <-reply; got.allow {
		t.Fatal("revise_plan unexpectedly allowed execution")
	}
	snap := c.InboxSnapshot()
	if len(snap.Items) != 1 {
		t.Fatalf("inbox items = %+v", snap.Items)
	}
	_, envelope, err := c.ReadInboxItem(snap.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.SubmitText != "cover the rollback path" || snap.Items[0].Idempotency != "plan-revision:"+id {
		t.Fatalf("revision = %+v / %+v", snap.Items[0], envelope)
	}
}
