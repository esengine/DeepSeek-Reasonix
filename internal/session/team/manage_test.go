package team

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/state/teamroster"
)

func TestOnlyAMainAgentCreatesAgentsAndGroupsAndStopsOthers(t *testing.T) {
	h := newHarness(t, "Chief", "Ada")
	ctx := context.Background()
	chief, ada := Caller{Agent: h.id("Chief"), Turn: "t1"}, Caller{Agent: h.id("Ada")}
	if _, err := h.svc.CreateAgent(ctx, ada, AgentChange{Name: new("Ben")}); !errors.Is(err, ErrNotMain) {
		t.Fatalf("a member creating an agent = %v", err)
	}
	ben, err := h.svc.CreateAgent(ctx, chief, AgentChange{Name: new("Ben"), Role: new("Coder"), Instructions: new("Write Go."), Model: new("deepseek-v4"), Avatar: &teamroster.Avatar{Glyph: "whale", Color: "cat-2"}})
	if err != nil || ben.CreatedBy != h.id("Chief") || ben.InitialModel != "deepseek-v4" || ben.Avatar.Glyph != "whale" || ben.Main {
		t.Fatalf("created = %+v, %v", ben, err)
	}
	if _, err := h.svc.CreateAgent(ctx, chief, AgentChange{Name: new("ben")}); !errors.Is(err, teamroster.ErrNameTaken) {
		t.Fatalf("a taken name = %v", err)
	}
	for _, n := range []string{"Cy", "Dee"} {
		if _, err := h.svc.CreateAgent(ctx, chief, AgentChange{Name: new(n)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.svc.CreateAgent(ctx, chief, AgentChange{Name: new("Eve")}); !errors.Is(err, ErrCreateLimit) {
		t.Fatalf("a fourth create in one turn = %v", err)
	}
	if _, err := h.svc.StopAgent(ada, "Ben"); !errors.Is(err, ErrNotMain) {
		t.Fatalf("a member stopping an agent = %v", err)
	}
	if _, err := h.svc.StopAgent(chief, "Chief"); !errors.Is(err, ErrSelf) {
		t.Fatalf("stopping itself = %v", err)
	}
	if stopped, err := h.svc.StopAgent(chief, "Ben"); err != nil || stopped {
		t.Fatalf("stopping an agent with no open session = %v, %v", stopped, err)
	}
	if _, err := h.svc.CreateGroup(ctx, ada, "x", "", []string{"Ben"}, "", ""); !errors.Is(err, ErrNotMain) {
		t.Fatalf("a member creating a group = %v", err)
	}
	g, err := h.svc.CreateGroup(ctx, chief, "Launch", "Ship it.", []string{"Ada", "ben"}, "", "")
	if err != nil || len(g.Members) != 3 || g.Admin != h.id("Chief") || g.Mode != teamroster.ModeAdmin || g.CreatedBy != h.id("Chief") {
		t.Fatalf("group = %+v, %v", g, err)
	}
}

func TestAnAgentChangesItselfAndOnlyAMainAgentChangesOthers(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	ctx := context.Background()
	ada := Caller{Agent: h.id("Ada"), ConfigurationApproved: true}
	got, err := h.svc.UpdateAgent(ctx, ada, "", AgentChange{Role: new("Writer"), Model: new("m2")})
	if err != nil || got.Role != "Writer" || got.InitialModel != "m2" {
		t.Fatalf("self update = %+v, %v", got, err)
	}
	if _, err := h.svc.UpdateAgent(ctx, ada, "Ben", AgentChange{Role: new("Intern")}); !errors.Is(err, ErrSelfOnly) {
		t.Fatalf("a member changing another = %v", err)
	}
	if got, err := h.svc.UpdateAgent(ctx, Caller{Agent: h.id("Chief"), ConfigurationApproved: true}, "ben", AgentChange{Instructions: new("Be brief.")}); err != nil || got.Instructions != "Be brief." {
		t.Fatalf("main changing another = %+v, %v", got, err)
	}
	if _, err := h.store.Update(ctx, func(r *teamroster.Roster) error {
		a, _ := r.Agent(h.id("Ada"))
		a.SessionPath = "/sessions/ada.jsonl"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.svc.UpdateAgent(ctx, ada, "", AgentChange{Model: new("m3")}); got.InitialModel != "m2" {
		t.Fatalf("a model change reached an agent whose session owns its model: %q", got.InitialModel)
	}
}

func TestGroupChangesFollowTheAdminAndMainRules(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben", "Cy")
	ctx := context.Background()
	g := h.group("Launch", teamroster.ModeAdmin, "Ada", "Ada", "Ben")
	ada, ben, chief := Caller{Agent: h.id("Ada")}, Caller{Agent: h.id("Ben")}, Caller{Agent: h.id("Chief")}
	if _, err := h.svc.UpdateGroup(ctx, ben, g, GroupChange{Notice: new("mine now")}); !errors.Is(err, ErrNotAdmin) {
		t.Fatalf("a plain member changing the group = %v", err)
	}
	changed, err := h.svc.UpdateGroup(ctx, ada, g, GroupChange{Notice: new("Ship Friday."), Add: []string{"Cy"}})
	if err != nil || len(changed) != 2 {
		t.Fatalf("admin update = %v, %v", changed, err)
	}
	if _, err := h.svc.UpdateGroup(ctx, ada, g, GroupChange{Admin: new("")}); !errors.Is(err, ErrNotMain) {
		t.Fatalf("an admin leaving the group without one = %v", err)
	}
	changed, err = h.svc.UpdateGroup(ctx, chief, g, GroupChange{Remove: []string{"Ada"}})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := h.store.Load()
	gr, _ := r.Group(g)
	if gr.Admin != "" || gr.Mode != teamroster.ModeEveryone || gr.IsMember(h.id("Ada")) || len(changed) != 2 {
		t.Fatalf("removing the admin: %+v, changed %v", gr, changed)
	}
	if _, err := h.svc.UpdateGroup(ctx, chief, g, GroupChange{Remove: []string{"Ben", "Cy"}}); !errors.Is(err, teamroster.ErrGroupSize) {
		t.Fatalf("emptying the group = %v", err)
	}
	if err := h.svc.DeleteGroup(ctx, ada, g); !errors.Is(err, ErrNotMain) {
		t.Fatalf("a member archiving = %v", err)
	}
	if err := h.svc.DeleteGroup(ctx, chief, g); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.UserPost(ctx, g, "anyone?", nil, false); !errors.Is(err, ErrArchived) {
		t.Fatalf("a post to an archived group = %v", err)
	}
	if _, err := h.svc.UpdateGroup(ctx, chief, g, GroupChange{Notice: new("x")}); !errors.Is(err, teamroster.ErrUnknownGroup) {
		t.Fatalf("changing an archived group = %v", err)
	}
	if id, err := h.svc.GroupID("launch"); err != nil || id != g {
		t.Fatalf("GroupID by name = %q, %v", id, err)
	}
}

func TestListAgentsShowsTheCallerAndTheRest(t *testing.T) {
	h := newHarness(t, "Chief", "Ada")
	got, err := h.svc.ListAgents(Caller{Agent: h.id("Chief")})
	if err != nil {
		t.Fatal(err)
	}
	contains(t, got, "You are Chief ("+h.id("Chief")+"), a main agent.", "- Ada ("+h.id("Ada")+")")
}
