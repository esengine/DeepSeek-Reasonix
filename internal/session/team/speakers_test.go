package team

import (
	"testing"

	"reasonix/internal/state/teamroster"
)

func members(slots []teamroster.Slot) []string {
	out := make([]string, 0, len(slots))
	for _, s := range slots {
		out = append(out, s.Member)
	}
	return out
}

func same(a []string, b ...string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestOpeningSpeakers(t *testing.T) {
	admin := teamroster.Group{Members: []string{"a", "b", "c"}, Admin: "b", Mode: teamroster.ModeAdmin}
	everyone := teamroster.Group{Members: []string{"a", "b", "c"}, Admin: "b", Mode: teamroster.ModeEveryone}
	noAdmin := teamroster.Group{Members: []string{"a", "b"}, Mode: teamroster.ModeEveryone}
	cases := []struct {
		name     string
		g        teamroster.Group
		mentions []string
		everyone bool
		by       string
		want     []string
		cause    teamroster.SlotCause
	}{
		{"admin mode calls the admin", admin, nil, false, teamroster.ByUser, []string{"b"}, teamroster.CauseUser},
		{"everyone mode: admin first, then roster order", everyone, nil, false, teamroster.ByUser, []string{"b", "a", "c"}, teamroster.CauseUser},
		{"@everyone in admin mode", admin, nil, true, teamroster.ByUser, []string{"b", "a", "c"}, teamroster.CauseUser},
		{"mentions win, in order, deduplicated", admin, []string{"c", "a", "c"}, false, teamroster.ByUser, []string{"c", "a"}, teamroster.CauseMention},
		{"no admin, everyone", noAdmin, nil, false, teamroster.ByUser, []string{"a", "b"}, teamroster.CauseUser},
		{"the poster never answers itself", everyone, nil, false, "b", []string{"a", "c"}, teamroster.CausePost},
		{"a mention of a non-member is dropped", admin, []string{"z", "a"}, false, teamroster.ByUser, []string{"a"}, teamroster.CauseMention},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cause := teamroster.CauseUser
			if tc.by != teamroster.ByUser {
				cause = teamroster.CausePost
			}
			got := openingSpeakers(tc.g, tc.mentions, tc.everyone, tc.by, cause, "ask")
			if !same(members(got), tc.want...) {
				t.Fatalf("speakers = %v, want %v", members(got), tc.want)
			}
			for _, s := range got {
				if s.Cause != tc.cause || s.By != tc.by || s.Ceiling != "ask" {
					t.Fatalf("slot = %+v", s)
				}
			}
		})
	}
}

func TestCallNextMovesAWaitingMemberUp(t *testing.T) {
	queue := []teamroster.Slot{{Member: "a"}, {Member: "b"}, {Member: "c"}}
	got := callNext(queue, []teamroster.Slot{{Member: "c", Cause: teamroster.CauseHand}, {Member: "d"}})
	if !same(members(got), "c", "d", "a", "b") || got[0].Cause != teamroster.CauseHand {
		t.Fatalf("queue = %+v", got)
	}
	if len(queue) != 3 || queue[0].Member != "a" {
		t.Fatal("callNext changed its input")
	}
}

func TestOnlyAgentCalledSlotsAreRelaysAndGetParked(t *testing.T) {
	rs := teamroster.RoundState{Queue: []teamroster.Slot{
		{Member: "a", By: teamroster.ByUser}, {Member: "b", By: "x"}, {Member: "c", By: teamroster.ByUser}, {Member: "d", By: "y"},
	}, Parked: []teamroster.Slot{{Member: "e", By: "z"}}}
	if moved := parkRelays(&rs); moved != 2 {
		t.Fatalf("parked %d", moved)
	}
	if !same(members(rs.Queue), "a", "c") || !same(members(rs.Parked), "e", "b", "d") {
		t.Fatalf("queue %v parked %v", members(rs.Queue), members(rs.Parked))
	}
	if RelayLimit(teamroster.Group{Members: []string{"a", "b"}}) != 6 || RelayLimit(teamroster.Group{Members: []string{"a", "b"}, RelayLimit: 2}) != 2 {
		t.Fatal("relay limit")
	}
}
