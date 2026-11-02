package control

import (
	"log/slog"
	"sync"

	"reasonix/internal/contract/tool"
	"reasonix/internal/session/team"
)

// Tags of the team's standing blocks.
const (
	agentIdentityTag = "agent-identity"
	teamRosterTag    = "team-roster"
	groupRosterTag   = "group-roster"
	teamMemoryTag    = "team-memory"
)

// TeamSeat is the seat on the user's team a session speaks through, bound by
// the host that opened it. It answers from the team's canonical state, so an
// edit made anywhere is the next turn's projection without anyone announcing it.
type TeamSeat interface {
	Projection() (team.Projection, error)
	Team() (*team.Service, team.SeatRef)
}

// teamSeat is the controller's side of its seat: the binding, and the model's
// copy of each standing block the seat projects.
type teamSeat struct {
	mu       sync.Mutex
	seat     TeamSeat
	identity projectionDebt
	roster   projectionDebt
	group    projectionDebt
	memory   projectionDebt
}

// TeamOptions seats a session on the user's team: Link reaches the team tools
// the build registered, Seat is the agent and group the session speaks for.
type TeamOptions struct {
	Link *tool.TeamLink
	Seat TeamSeat
}

func (c *Controller) startTeam(o TeamOptions) {
	if o.Seat != nil {
		c.BindTeamSeat(o.Seat)
	}
	if o.Link != nil {
		o.Link.Bind(teamDirectory{c})
	}
}

// BindTeamSeat makes this session speak for seat; nil unbinds it. The host
// binds before the first turn, so even a turn another agent started carries
// who this agent is.
func (c *Controller) BindTeamSeat(seat TeamSeat) {
	c.team.mu.Lock()
	c.team.seat = seat
	c.team.mu.Unlock()
}

// BoundTeamSeat is the seat this session speaks for, nil for an ordinary one,
// so a rebuild seats its replacement where this one sat.
func (c *Controller) BoundTeamSeat() TeamSeat { return c.team.bound() }

func (t *teamSeat) bound() TeamSeat {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.seat
}

// owed renders the seat's standing blocks this turn still owes. They ride
// every turn, synthetic ones included: an agent's first turn is often a
// message from another agent, and it must not start without its identity.
func (t *teamSeat) owed() []turnBlock {
	seat := t.bound()
	if seat == nil {
		return nil
	}
	p, err := seat.Projection()
	if err != nil {
		slog.Warn("controller: project the team seat", "err", err)
		return nil
	}
	group := p.Group
	if group == "" && t.group.visible(groupRosterTag) {
		group = groupLeft
	}
	shared := p.Memory
	if shared == "" && t.memory.visible(teamMemoryTag) {
		shared = sharedForgotten
	}
	return []turnBlock{
		{agentIdentityTag, wrapTurnBlock(agentIdentityTag, t.identity.owed(agentIdentityTag, p.Identity))},
		{teamRosterTag, wrapTurnBlock(teamRosterTag, t.roster.owed(teamRosterTag, p.Roster))},
		{groupRosterTag, wrapTurnBlock(groupRosterTag, t.group.owed(groupRosterTag, group))},
		{teamMemoryTag, wrapTurnBlock(teamMemoryTag, t.memory.owed(teamMemoryTag, shared))},
	}
}

// sharedForgotten retracts a shared-memory list the model still reads once
// nothing is left in it.
const sharedForgotten = "Nothing is shared in the team's or this group's memory any more."

// groupLeft retracts a group block the model still reads once this seat's
// agent is no longer in the group, so the old one stops reading as current.
const groupLeft = "You are no longer a member of this group, or it no longer exists. Nothing you write here reaches its members."
