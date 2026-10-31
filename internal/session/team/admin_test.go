package team

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/state/teamroster"
)

func TestBootstrapGivesAnEmptyTeamOneMainAgent(t *testing.T) {
	h := newHarness(t)
	for range 2 {
		r, err := h.svc.Bootstrap(context.Background(), "Chief", "Chief of Staff")
		if err != nil || len(r.Agents) != 1 || !r.Agents[0].Main || r.Agents[0].Name != "Chief" {
			t.Fatalf("Bootstrap = %+v, %v", r.Agents, err)
		}
	}
}

func TestTheUserSetsWhatNoAgentCan(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	ctx := context.Background()
	got, err := h.svc.UserUpdateAgent(ctx, h.id("Ada"), UserAgentChange{Permission: new("auto"), Main: new(true), Pinned: new(true)})
	if err != nil || got.Permission != "auto" || !got.Main || !got.Pinned {
		t.Fatalf("update = %+v, %v", got, err)
	}
	if _, err := h.svc.UserUpdateAgent(ctx, h.id("Ada"), UserAgentChange{Main: new(false)}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.UserUpdateAgent(ctx, h.id("Chief"), UserAgentChange{Main: new(false)}); !errors.Is(err, teamroster.ErrLastMain) {
		t.Fatalf("demoting the last main agent = %v", err)
	}
	g, err := h.svc.UserCreateGroup(ctx, UserGroupChange{Name: new("Launch"), Members: []string{h.id("Ada"), h.id("Ben")}, Admin: new(h.id("Ada")), RelayLimit: new(4)})
	if err != nil || g.Mode != teamroster.ModeAdmin || g.RelayLimit != 4 {
		t.Fatalf("group = %+v, %v", g, err)
	}
	if err := h.svc.UserDeleteAgent(ctx, h.id("Ada")); err != nil {
		t.Fatal(err)
	}
	r, _ := h.store.Load()
	if _, ok := r.Agent(h.id("Ada")); ok || len(r.Groups) != 0 {
		t.Fatalf("after deleting Ada: agents %d, groups %+v", len(r.Agents), r.Groups)
	}
}

func TestDeletingAMemberKeepsALargerGroupWithoutIt(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben", "Cy")
	ctx := context.Background()
	g, err := h.svc.UserCreateGroup(ctx, UserGroupChange{Members: []string{h.id("Ada"), h.id("Ben"), h.id("Cy")}, Admin: new(h.id("Ada"))})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.UserDeleteAgent(ctx, h.id("Ada")); err != nil {
		t.Fatal(err)
	}
	r, _ := h.store.Load()
	got, ok := r.Group(g.ID)
	if !ok || len(got.Members) != 2 || got.Admin != "" || got.Mode != teamroster.ModeEveryone {
		t.Fatalf("group after its admin left = %+v, %v", got, ok)
	}
	if err := h.svc.UserDeleteGroup(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.UserUpdateGroup(ctx, g.ID, UserGroupChange{Name: new("x")}); !errors.Is(err, teamroster.ErrUnknownGroup) {
		t.Fatalf("a deleted group = %v", err)
	}
}
