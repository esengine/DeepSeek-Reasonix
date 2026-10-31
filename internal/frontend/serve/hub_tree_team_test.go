package serve

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/team"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/teamroster"
)

// An agent's folder is not one of the user's projects, and an agent working in
// one of them does not add its conversations to that project's list.
func TestTheProjectTreeLeavesTheTeamOut(t *testing.T) {
	h, ids := teamHub(t, "Chief", "Ada")
	ctx := context.Background()
	project := testenv.TempDir(t)
	if err := config.NewProjectGrantStore(config.RootsForHome("").Home()).SetTrust(project, config.WorkspaceTrusted); err != nil {
		t.Fatal(err)
	}
	if err := addRememberedWorkspace(ctx, project); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(SessionDirFor(project), "20261001-120000.jsonl")
	s := sessionstore.NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "my own talk"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "an answer"})
	if err := s.SaveSnapshot(mine); err != nil {
		t.Fatal(err)
	}
	if _, err := h.team.store.Update(ctx, func(r *teamroster.Roster) error {
		a, _ := r.Agent(ids["Ada"])
		a.Folder = project
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ada := ensureSeat(t, h, team.SeatRef{Agent: ids["Ada"]})
	agentTalk := sessionstore.NewSession("system")
	agentTalk.Add(provider.Message{Role: provider.RoleUser, Content: "an agent's talk"})
	agentTalk.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	if err := agentTalk.SaveSnapshot(ada.Server.Controller().SessionPath()); err != nil {
		t.Fatal(err)
	}
	chief := ensureSeat(t, h, team.SeatRef{Agent: ids["Chief"]})

	c := newTeamClient(t, h)
	status, out := c.do("GET", "/tree", nil)
	if status != 200 {
		t.Fatalf("tree = %d %s", status, out)
	}
	var nodes []treeWorkspace
	if err := json.Unmarshal(out, &nodes); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range nodes {
		if sameFolder(n.Root, chief.Root) {
			t.Fatalf("an agent's own folder is listed as a project: %s", n.Root)
		}
		if !sameFolder(n.Root, project) {
			continue
		}
		found = true
		if n.Open {
			t.Fatal("an agent working in the project marks it open")
		}
		for _, row := range n.Sessions {
			if strings.Contains(row.Path, "agent") || sameFolder(row.Path, ada.Server.Controller().SessionPath()) {
				t.Fatalf("the agent's conversation is listed in the user's project: %s", row.Path)
			}
		}
		if len(n.Sessions) != 1 || filepath.Base(n.Sessions[0].Path) != filepath.Base(mine) {
			t.Fatalf("project sessions = %+v", n.Sessions)
		}
	}
	if !found {
		t.Fatalf("the user's project is missing from the tree: %s", out)
	}
}

// A seat's conversation is left out of the tree on purpose, so a window
// showing it is not a pane whose row went missing.
func TestATeamPaneIsNotAnOrphan(t *testing.T) {
	h, ids := teamHub(t, "Chief")
	chief := ensureSeat(t, h, team.SeatRef{Agent: ids["Chief"]})
	h.team.holdForWindow(chief.ID)
	if path := chief.Server.Controller().SessionPath(); path == "" {
		t.Fatal("a seat opens with a conversation path")
	}
	if got := h.orphanPanes(nil); len(got) != 0 {
		t.Fatalf("orphanPanes = %+v, want none: the tree leaves the team out by design", got)
	}
}
