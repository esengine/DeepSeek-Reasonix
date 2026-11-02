package team

import (
	"fmt"
	"slices"

	"reasonix/internal/state/teamroster"
)

// openingSpeakers is who answers a post: the members it mentions, otherwise
// everyone or the admin as the post and the group's mode decide. The poster
// never answers its own post.
func openingSpeakers(g teamroster.Group, mentions []string, everyone bool, by string, cause teamroster.SlotCause, ceiling string) []teamroster.Slot {
	var ids []string
	switch {
	case len(mentions) > 0:
		ids, cause = mentions, teamroster.CauseMention
	case everyone || g.Mode == teamroster.ModeEveryone:
		if g.Admin != "" {
			ids = append(ids, g.Admin)
		}
		ids = append(ids, g.Members...)
	case g.Admin != "":
		ids = []string{g.Admin}
	}
	var out []teamroster.Slot
	for _, id := range ids {
		if id == by || !g.IsMember(id) || slices.ContainsFunc(out, func(s teamroster.Slot) bool { return s.Member == id }) {
			continue
		}
		out = append(out, teamroster.Slot{Member: id, Cause: cause, By: by, Ceiling: ceiling})
	}
	return out
}

// callNext puts slots at the head of queue, in order. A member already waiting
// moves up rather than waiting twice.
func callNext(queue, slots []teamroster.Slot) []teamroster.Slot {
	out := append([]teamroster.Slot{}, slots...)
	for _, q := range queue {
		if !slices.ContainsFunc(out, func(s teamroster.Slot) bool { return s.Member == q.Member }) {
			out = append(out, q)
		}
	}
	return out
}

// isRelay reports whether a slot was called by an agent rather than the user;
// only those count against the group's relay limit.
func isRelay(s teamroster.Slot) bool { return s.By != "" && s.By != teamroster.ByUser }

// RelayLimit is the group's limit on agent-called turns between two messages
// from the user.
func RelayLimit(g teamroster.Group) int {
	if g.RelayLimit > 0 {
		return g.RelayLimit
	}
	return 3 * len(g.Members)
}

// parkRelays moves every agent-called slot out of the queue and into parked,
// leaving the ones the user called.
func parkRelays(rs *teamroster.RoundState) int {
	var keep []teamroster.Slot
	moved := 0
	for _, s := range rs.Queue {
		if isRelay(s) {
			rs.Parked = append(rs.Parked, s)
			moved++
			continue
		}
		keep = append(keep, s)
	}
	rs.Queue = keep
	return moved
}

func stepKey(group string, step int64) string { return fmt.Sprintf("turn-%s-%d", group, step) }
