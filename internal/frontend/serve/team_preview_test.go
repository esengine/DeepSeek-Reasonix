package serve

import (
	"context"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/teamroster"
)

// An agent's row says where its conversation stands now, not how it began.
func TestAnAgentsRowPreviewsItsLatestMessage(t *testing.T) {
	h, ids := teamHub(t, "Chief")
	path := filepath.Join(testenv.TempDir(t), "chief.jsonl")
	sess := sessionstore.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "create a writer"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "Created it."})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hand it work"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "The writer answered."})
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := h.team.store.Update(context.Background(), func(r *teamroster.Roster) error {
		a, _ := r.Agent(ids["Chief"])
		a.SessionPath = path
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v := newTeamClient(t, h).view("GET", "/team", nil)
	chief, ok := agentNamed(v, "Chief")
	if !ok || chief.Preview != "The writer answered." {
		t.Fatalf("Chief's row = %+v", chief)
	}
}
