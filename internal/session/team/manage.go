package team

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/state/teamroster"
)

// MaxCreatesPerTurn bounds how many agents one turn may create.
const MaxCreatesPerTurn = 3

// AgentChange is what create_agent and update_agent set; a nil field is left
// as it is.
type AgentChange struct {
	Name         *string
	Role         *string
	Instructions *string
	Model        *string
	Folder       *string
	Avatar       *teamroster.Avatar
}

// GroupChange is what update_group sets. Admin "" removes the admin.
type GroupChange struct {
	Name   *string
	Notice *string
	Add    []string
	Remove []string
	Admin  *string
	Mode   *teamroster.Mode
}

// ListAgents renders the team as the calling agent sees it.
func (s *Service) ListAgents(c Caller) (string, error) {
	r, err := s.store.Load()
	if err != nil {
		return "", err
	}
	self, ok := r.Agent(c.Agent)
	if !ok {
		return "", teamroster.ErrUnknownAgent
	}
	head := fmt.Sprintf("You are %s (%s)", self.Name, self.ID)
	if self.Main {
		head += ", a main agent"
	}
	return head + ".\n" + renderRoster(r, self.ID), nil
}

func (s *Service) caller(r *teamroster.Roster, c Caller) (*teamroster.Agent, error) {
	a, ok := r.Agent(c.Agent)
	if !ok {
		return nil, teamroster.ErrUnknownAgent
	}
	return a, nil
}

func agentByRef(r *teamroster.Roster, ref string) (*teamroster.Agent, bool) {
	if a, ok := r.Agent(ref); ok {
		return a, true
	}
	return r.AgentNamed(ref)
}

// CreateAgent adds an agent a main agent asked for. Its model is only a
// starting choice; the agent's own session owns it once that exists.
func (s *Service) CreateAgent(ctx context.Context, c Caller, ch AgentChange) (teamroster.Agent, error) {
	defer s.changed()
	var out teamroster.Agent
	charged := false
	_, err := s.store.Update(ctx, func(r *teamroster.Roster) error {
		self, err := authorizeCreate(r, c)
		if err != nil {
			return err
		}
		if !charged && !s.chargeCreate(c.Turn) {
			return ErrCreateLimit
		}
		charged = true
		a := teamroster.Agent{CreatedBy: self.ID, CreatedAt: s.now()}
		applyAgent(&a, ch)
		out = r.AddAgent(a)
		return nil
	})
	if err != nil && charged {
		s.refundCreate(c.Turn)
	}
	return out, err
}

func (s *Service) refundCreate(turn string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sends.creates[turn] > 0 {
		s.sends.creates[turn]--
	}
}

func (s *Service) chargeCreate(turn string) bool {
	if turn == "" {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sends.creates == nil {
		s.sends.creates = map[string]int{}
	}
	if s.sends.creates[turn] >= MaxCreatesPerTurn {
		return false
	}
	if len(s.sends.creates) >= maxTrackedTurns {
		clear(s.sends.creates)
	}
	s.sends.creates[turn]++
	return true
}

func applyAgent(a *teamroster.Agent, ch AgentChange) {
	if ch.Name != nil {
		a.Name = *ch.Name
	}
	if ch.Role != nil {
		a.Role = *ch.Role
	}
	if ch.Instructions != nil {
		a.Instructions = *ch.Instructions
	}
	if ch.Model != nil && a.SessionPath == "" {
		a.InitialModel = *ch.Model
	}
	if ch.Folder != nil {
		a.Folder = *ch.Folder
	}
	if ch.Avatar != nil {
		a.Avatar.Glyph, a.Avatar.Color = ch.Avatar.Glyph, ch.Avatar.Color
	}
}

// UpdateAgent changes target ("" is the caller itself). Any agent may change
// itself; changing another takes a main agent. A model change reaches only an
// agent that has not started its conversation yet.
func (s *Service) UpdateAgent(ctx context.Context, c Caller, target string, ch AgentChange) (teamroster.Agent, error) {
	defer s.changed()
	var out teamroster.Agent
	_, err := s.store.Update(ctx, func(r *teamroster.Roster) error {
		a, err := authorizeUpdate(r, c, target)
		if err != nil {
			return err
		}
		if ch.RequiresApproval(*a) && !c.ConfigurationApproved {
			return ErrConfigurationApproval
		}
		applyAgent(a, ch)
		out = *a
		return nil
	})
	return out, err
}

// StopAgent cancels what target is running in its own chat; only a main agent
// may, and never on itself.
func (s *Service) StopAgent(c Caller, target string) (bool, error) {
	r, err := s.store.Load()
	if err != nil {
		return false, err
	}
	self, err := s.caller(&r, c)
	if err != nil {
		return false, err
	}
	if !self.Main {
		return false, ErrNotMain
	}
	a, ok := agentByRef(&r, target)
	if !ok {
		return false, teamroster.ErrUnknownAgent
	}
	if a.ID == self.ID {
		return false, ErrSelf
	}
	return s.Stop(a.ID), nil
}

// CreateGroup opens a group a main agent asked for. The caller joins it and is
// its admin unless it names another member.
func (s *Service) CreateGroup(ctx context.Context, c Caller, name, notice string, members []string, admin string, mode teamroster.Mode) (teamroster.Group, error) {
	defer s.changed()
	var out teamroster.Group
	_, err := s.store.Update(ctx, func(r *teamroster.Roster) error {
		self, err := s.caller(r, c)
		if err != nil {
			return err
		}
		if !self.Main {
			return ErrNotMain
		}
		g := teamroster.Group{Name: name, Notice: notice, Members: []string{self.ID}, Admin: self.ID, Mode: mode, CreatedBy: self.ID, CreatedAt: s.now()}
		for _, m := range members {
			a, ok := agentByRef(r, m)
			if !ok {
				return teamroster.ErrUnknownAgent
			}
			if !g.IsMember(a.ID) {
				g.Members = append(g.Members, a.ID)
			}
		}
		if admin != "" {
			a, ok := agentByRef(r, admin)
			if !ok {
				return teamroster.ErrUnknownAgent
			}
			g.Admin = a.ID
		}
		if g.Mode == "" {
			g.Mode = teamroster.ModeAdmin
		}
		out = r.AddGroup(g)
		return nil
	})
	return out, err
}

// UpdateGroup changes group for a main agent, or for the group's admin; only
// a main agent may leave a group without an admin. It returns what changed.
func (s *Service) UpdateGroup(ctx context.Context, c Caller, group string, ch GroupChange) ([]string, error) {
	defer s.changed()
	var changed []string
	_, err := s.store.Update(ctx, func(r *teamroster.Roster) error {
		self, err := s.caller(r, c)
		if err != nil {
			return err
		}
		g, ok := r.Group(group)
		if !ok || g.Archived {
			return teamroster.ErrUnknownGroup
		}
		if !self.Main && g.Admin != self.ID {
			return ErrNotAdmin
		}
		if ch.Admin != nil && *ch.Admin == "" && !self.Main {
			return ErrNotMain
		}
		changed, err = applyGroup(r, g, ch)
		return err
	})
	return changed, err
}

func applyGroup(r *teamroster.Roster, g *teamroster.Group, ch GroupChange) ([]string, error) {
	var changed []string
	if ch.Name != nil {
		g.Name, changed = *ch.Name, append(changed, "name")
	}
	if ch.Notice != nil {
		g.Notice, changed = *ch.Notice, append(changed, "notice")
	}
	for _, ref := range ch.Add {
		a, ok := agentByRef(r, ref)
		if !ok {
			return nil, teamroster.ErrUnknownAgent
		}
		if !g.IsMember(a.ID) {
			g.Members, changed = append(g.Members, a.ID), append(changed, "added "+a.Name)
		}
	}
	for _, ref := range ch.Remove {
		a, ok := agentByRef(r, ref)
		if !ok || !g.IsMember(a.ID) {
			return nil, teamroster.ErrNotMember
		}
		g.Members = slices.DeleteFunc(g.Members, func(m string) bool { return m == a.ID })
		delete(g.MemberSessions, a.ID)
		changed = append(changed, "removed "+a.Name)
		if g.Admin == a.ID {
			g.Admin = ""
		}
	}
	if ch.Admin != nil {
		g.Admin = ""
		if *ch.Admin != "" {
			a, ok := agentByRef(r, *ch.Admin)
			if !ok {
				return nil, teamroster.ErrUnknownAgent
			}
			g.Admin = a.ID
		}
		changed = append(changed, "admin")
	}
	if ch.Mode != nil {
		g.Mode, changed = *ch.Mode, append(changed, "who answers")
	}
	if g.Admin == "" && g.Mode == teamroster.ModeAdmin {
		g.Mode, changed = teamroster.ModeEveryone, append(changed, "everyone answers now that the group has no admin")
	}
	return changed, nil
}

// DeleteGroup archives group for a main agent: its history stays and it takes
// no new posts. Removing it for good is the user's call.
func (s *Service) DeleteGroup(ctx context.Context, c Caller, group string) error {
	defer s.changed()
	_, err := s.store.Update(ctx, func(r *teamroster.Roster) error {
		self, err := s.caller(r, c)
		if err != nil {
			return err
		}
		if !self.Main {
			return ErrNotMain
		}
		g, ok := r.Group(group)
		if !ok || g.Archived {
			return teamroster.ErrUnknownGroup
		}
		g.Archived, g.Hidden = true, true
		return nil
	})
	return err
}

// groupRef resolves a group by id or exact name.
func groupRef(r teamroster.Roster, ref string) (string, bool) {
	if g, ok := r.Group(ref); ok {
		return g.ID, true
	}
	for _, g := range r.Groups {
		if g.Name != "" && strings.EqualFold(g.Name, strings.TrimSpace(ref)) {
			return g.ID, true
		}
	}
	return "", false
}

// GroupID resolves a group named by id or exact name.
func (s *Service) GroupID(ref string) (string, error) {
	r, err := s.store.Load()
	if err != nil {
		return "", err
	}
	id, ok := groupRef(r, ref)
	if !ok {
		return "", teamroster.ErrUnknownGroup
	}
	return id, nil
}
