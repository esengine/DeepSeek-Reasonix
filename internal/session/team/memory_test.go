package team

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/state/memory"
	"reasonix/internal/state/teamroster"
)

func groupOf(t *testing.T, h *harness, admin string, members ...string) string {
	t.Helper()
	g, err := h.svc.UserCreateGroup(context.Background(), UserGroupChange{Name: new("Launch"), Members: members, Admin: new(admin)})
	if err != nil {
		t.Fatal(err)
	}
	return g.ID
}

func save(t *testing.T, s memory.Store, name, desc string) {
	t.Helper()
	if _, err := s.Save(memory.Memory{Name: name, Description: desc, Type: memory.TypeProject, Body: desc}); err != nil {
		t.Fatal(err)
	}
}

// Everyone reads the team's memory; only a main agent or a group's admin
// writes it, the way DS-Bot's team tier works.
func TestTheTeamsMemoryIsReadByAllAndWrittenByTheLeads(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	groupOf(t, h, h.id("Ada"), h.id("Ada"), h.id("Ben"))
	for _, who := range []string{"Chief", "Ada", "Ben"} {
		if _, err := h.svc.SharedMemory(Caller{Agent: h.id(who)}, TierTeam, "", false); err != nil {
			t.Fatalf("%s reading the team's memory: %v", who, err)
		}
	}
	for _, who := range []string{"Chief", "Ada"} {
		if _, err := h.svc.SharedMemory(Caller{Agent: h.id(who)}, TierTeam, "", true); err != nil {
			t.Fatalf("%s writing the team's memory: %v", who, err)
		}
	}
	if _, err := h.svc.SharedMemory(Caller{Agent: h.id("Ben")}, TierTeam, "", true); !errors.Is(err, ErrNotAdmin) {
		t.Fatalf("Ben writing the team's memory = %v, want ErrNotAdmin", err)
	}
}

// A group's memory belongs to its members: in a group seat it is the seat's
// group, elsewhere it is named, and an outsider reaches it neither way.
func TestAGroupsMemoryIsItsMembersAlone(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	g := groupOf(t, h, h.id("Ada"), h.id("Ada"), h.id("Ben"))
	seat, err := h.svc.SharedMemory(Caller{Agent: h.id("Ben"), Group: g}, TierGroup, "", true)
	if err != nil {
		t.Fatal(err)
	}
	named, err := h.svc.SharedMemory(Caller{Agent: h.id("Ada")}, TierGroup, "Launch", false)
	if err != nil || named.Dir != seat.Dir {
		t.Fatalf("named group = %q (%v), want the seat's %q", named.Dir, err, seat.Dir)
	}
	if _, err := h.svc.SharedMemory(Caller{Agent: h.id("Chief")}, TierGroup, g, false); !errors.Is(err, teamroster.ErrNotMember) {
		t.Fatalf("an outsider reading the group's memory = %v, want ErrNotMember", err)
	}
	if _, err := h.svc.SharedMemory(Caller{Agent: h.id("Ada")}, TierGroup, "", false); !errors.Is(err, ErrNotInGroup) {
		t.Fatalf("no group named outside a group seat = %v, want ErrNotInGroup", err)
	}
	if _, err := h.svc.SharedMemory(Caller{Agent: h.id("Ada")}, "everyone", "", false); !errors.Is(err, teamroster.ErrInvalid) {
		t.Fatalf("an unknown tier = %v, want ErrInvalid", err)
	}
}

// What the team and the group remember is listed at the turn's tail, by title
// and hook, and the same memories give the same bytes.
func TestTheProjectionListsSharedMemory(t *testing.T) {
	h := newHarness(t, "Chief", "Ada", "Ben")
	g := groupOf(t, h, h.id("Ada"), h.id("Ada"), h.id("Ben"))
	teamStore, _ := h.svc.SharedMemory(Caller{Agent: h.id("Chief")}, TierTeam, "", true)
	groupStore, _ := h.svc.SharedMemory(Caller{Agent: h.id("Ada"), Group: g}, TierGroup, "", true)
	save(t, teamStore, "house-style", "write in plain English")
	save(t, groupStore, "launch-date", "the launch is on Friday")

	own, err := h.svc.Projection(SeatRef{Agent: h.id("Ben")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(own.Memory, "write in plain English") || strings.Contains(own.Memory, "Friday") {
		t.Fatalf("a private seat's memory block:\n%s", own.Memory)
	}
	seat, _ := h.svc.Projection(SeatRef{Agent: h.id("Ben"), Group: g})
	if !strings.Contains(seat.Memory, "write in plain English") || !strings.Contains(seat.Memory, "the launch is on Friday") || !strings.Contains(seat.Memory, "recall_shared") {
		t.Fatalf("a group seat's memory block:\n%s", seat.Memory)
	}
	again, _ := h.svc.Projection(SeatRef{Agent: h.id("Ben"), Group: g})
	if again.Memory != seat.Memory {
		t.Fatal("the same memories projected different bytes")
	}
	solo := newHarness(t, "Solo")
	empty, err := solo.svc.Projection(SeatRef{Agent: solo.id("Solo")})
	if err != nil || empty.Memory != "" {
		t.Fatalf("no shared memory still projected:\n%s", empty.Memory)
	}
}
