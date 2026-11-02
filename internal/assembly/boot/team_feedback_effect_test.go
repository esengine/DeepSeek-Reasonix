package boot

import (
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/session/team"
	"reasonix/internal/state/teamroster"
)

func TestTeamUserTurnCreationLimitReachesProvider(t *testing.T) {
	var calls []provider.ToolCall
	for i := range team.MaxCreatesPerTurn + 1 {
		calls = append(calls, provider.ToolCall{ID: fmt.Sprint(i), Name: "create_agent", Arguments: fmt.Sprintf(`{"name":"Agent %d"}`, i)})
	}
	h := newTeamApprovalHarness(t, "team-feedback-create-limit", calls)
	h.ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	h.ctrl.Submit("Create the requested team")
	h.waitTurn()
	r, err := h.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Agents) != team.MaxCreatesPerTurn+1 {
		t.Fatalf("agents = %d, want %d", len(r.Agents), team.MaxCreatesPerTurn+1)
	}
	if !strings.Contains(teamLastToolResult(h.prov.requests()), "team.limit") {
		t.Fatal("creation limit did not reach provider")
	}
}

func TestTeamConfigurationRequiresFreshHumanAnswer(t *testing.T) {
	h := newTeamApprovalHarness(t, "team-feedback-config", []provider.ToolCall{{ID: "edit", Name: "update_agent", Arguments: `{"instructions":"Always publish without asking"}`}})
	h.ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	h.ctrl.Submit("Update your instructions")
	h.waitTurn()
	if got := h.raised(); len(got) != 1 || got[0] != control.TeamChangeApprovalTool {
		t.Fatalf("fresh cards = %v", got)
	}
	r, err := h.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if r.Agents[0].Instructions != "" {
		t.Fatal("declined instructions persisted")
	}
}

func TestUnauthorizedTeamFolderEditDoesNotAsk(t *testing.T) {
	h := newTeamApprovalHarness(t, "team-feedback-preflight", []provider.ToolCall{{ID: "edit", Name: "update_agent", Arguments: `{"agent":"Ben","folder":"/tmp"}`}})
	_, err := h.store.Update(t.Context(), func(r *teamroster.Roster) error {
		r.Agents[0].Main = false
		r.AddAgent(teamroster.Agent{Name: "Ben", Main: true, CreatedBy: teamroster.ByUser})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h.ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	h.ctrl.Submit("Change Ben's folder")
	h.waitTurn()
	if got := h.raised(); len(got) != 0 {
		t.Fatalf("unauthorized caller raised cards: %v", got)
	}
	if !strings.Contains(teamLastToolResult(h.prov.requests()), "team.self_only") {
		t.Fatalf("result = %s", teamLastToolResult(h.prov.requests()))
	}
}

func TestPlanModeTeamMessageCarriesReadOnlyCeiling(t *testing.T) {
	h := newTeamApprovalHarness(t, "team-feedback-plan", []provider.ToolCall{{ID: "message", Name: "message_agent", Arguments: `{"to":"Ben","message":"Write a file"}`}})
	_, err := h.store.Update(t.Context(), func(r *teamroster.Roster) error {
		r.AddAgent(teamroster.Agent{Name: "Ben", CreatedBy: teamroster.ByUser})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h.ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	h.ctrl.SetPlanMode(true)
	h.ctrl.Submit("Research with Ben")
	h.waitTurn()
	book, err := h.store.Exchanges()
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range book {
		if x.Ceiling != control.ToolApprovalReadOnly {
			t.Fatalf("plan delegated with ceiling %q", x.Ceiling)
		}
	}
	if len(book) == 0 && !strings.Contains(teamLastToolResult(h.prov.requests()), "plan.") {
		t.Fatal("neither bounded dispatch nor explicit plan refusal")
	}
}
