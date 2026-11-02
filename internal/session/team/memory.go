package team

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/contract/hostframe"
	"reasonix/internal/state/memory"
	"reasonix/internal/state/teamroster"
)

// SharedTier names a memory several agents share. Each agent's own memory is
// the session's ordinary store; these sit beside it.
type SharedTier string

const (
	TierTeam  SharedTier = "team"
	TierGroup SharedTier = "group"
)

// sharedShown bounds how many shared memories one tier lists at a turn's tail.
const sharedShown = 20

// AgentMemoryDir is where an agent's own memory lives under the team's
// directory: one memory across every folder and group it works in.
func AgentMemoryDir(teamDir, agent string) string {
	if teamDir == "" || agent == "" {
		return ""
	}
	return filepath.Join(teamDir, "agents", agent, "memory")
}

// SharedStore is where tier's memory lives under the team's directory; group
// is the group's id for the group tier.
func SharedStore(teamDir string, tier SharedTier, group string) memory.Store {
	if tier == TierGroup {
		return memory.Store{Dir: filepath.Join(teamDir, "memory", "groups", group)}
	}
	return memory.Store{Dir: filepath.Join(teamDir, "memory", "team")}
}

func (s *Service) teamMemory() memory.Store { return SharedStore(s.store.Dir, TierTeam, "") }

func (s *Service) groupMemory(id string) memory.Store { return SharedStore(s.store.Dir, TierGroup, id) }

// SharedMemory is the store for tier as caller c may use it. Anyone reads the
// team's memory and only a main agent or a group's admin writes it; a group's
// memory is its members' alone, the seat's group unless one is named.
func (s *Service) SharedMemory(c Caller, tier SharedTier, group string, write bool) (memory.Store, error) {
	r, err := s.store.Load()
	if err != nil {
		return memory.Store{}, err
	}
	a, ok := r.Agent(c.Agent)
	if !ok {
		return memory.Store{}, teamroster.ErrUnknownAgent
	}
	switch tier {
	case TierTeam:
		if write && !a.Main && !adminOfAny(r, a.ID) {
			return memory.Store{}, ErrNotAdmin
		}
		return s.teamMemory(), nil
	case TierGroup:
		if group == "" {
			group = c.Group
		}
		if group == "" {
			return memory.Store{}, ErrNotInGroup
		}
		id, ok := groupRef(r, group)
		if !ok {
			return memory.Store{}, teamroster.ErrUnknownGroup
		}
		g, _ := r.Group(id)
		if !g.IsMember(a.ID) {
			return memory.Store{}, teamroster.ErrNotMember
		}
		return s.groupMemory(id), nil
	}
	return memory.Store{}, &teamroster.FieldError{Field: "tier", Err: teamroster.ErrInvalid}
}

func adminOfAny(r teamroster.Roster, agent string) bool {
	for _, g := range r.Groups {
		if g.Admin == agent && !g.Archived {
			return true
		}
	}
	return false
}

// renderMemory lists what the team, and the seat's group, remember: title and
// hook, newest first, with the tool that reads the rest. No shared memory at
// all renders nothing.
func (s *Service) renderMemory(seat SeatRef, inGroup bool) string {
	var b strings.Builder
	write := func(heading string, store memory.Store) {
		facts, err := store.List()
		if err != nil || len(facts) == 0 {
			return
		}
		sort.SliceStable(facts, func(i, j int) bool {
			if !facts[i].UpdatedAt.Equal(facts[j].UpdatedAt) {
				return facts[i].UpdatedAt.After(facts[j].UpdatedAt)
			}
			return facts[i].Name < facts[j].Name
		})
		b.WriteString(heading + "\n")
		for i, m := range facts {
			if i == sharedShown {
				fmt.Fprintf(&b, "- … %d more; list them with recall_shared\n", len(facts)-sharedShown)
				break
			}
			fmt.Fprintf(&b, "- %s: %s\n", m.Name, m.Description)
		}
	}
	write("What the team remembers (tier team):", s.teamMemory())
	if inGroup {
		write("What this group remembers (tier group):", s.groupMemory(seat.Group))
	}
	if b.Len() == 0 {
		return ""
	}
	b.WriteString("Read a memory in full with recall_shared; save or correct one with remember_shared. Your own memory stays with the memory and remember tools.")
	return hostframe.Text(b.String()).String()
}
