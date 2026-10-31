package team

import (
	"context"
	"slices"

	"reasonix/internal/state/teamroster"
)

// The calls below are the user's own, through the window: no agent makes
// them, so no role is checked, and only they reach a permission, a main agent
// or a relay limit.

// UserAgentChange is what the user sets on an agent; a nil field is left as it
// is.
type UserAgentChange struct {
	AgentChange
	Permission *string
	Main       *bool
	Pinned     *bool
	Hidden     *bool
}

// UserGroupChange is what the user sets on a group; a nil field is left as it
// is. Members replaces the member list whole.
type UserGroupChange struct {
	Name       *string
	Notice     *string
	Members    []string
	Admin      *string
	Mode       *teamroster.Mode
	RelayLimit *int
	Folder     *string
	Pinned     *bool
	Hidden     *bool
}

// Bootstrap gives an empty team its first main agent, named name. A team that
// has agents is left alone, so asking twice creates one.
func (s *Service) Bootstrap(ctx context.Context, name, role string) (teamroster.Roster, error) {
	defer s.changed()
	return s.store.Update(ctx, func(r *teamroster.Roster) error {
		if len(r.Agents) == 0 {
			r.AddAgent(teamroster.Agent{Name: name, Role: role, Main: true, Avatar: teamroster.Avatar{Glyph: "whale"}, CreatedBy: teamroster.ByUser, CreatedAt: s.now()})
		}
		return nil
	})
}

// UserCreateAgent adds an agent the user made.
func (s *Service) UserCreateAgent(ctx context.Context, ch UserAgentChange) (teamroster.Agent, error) {
	defer s.changed()
	var out teamroster.Agent
	_, err := s.store.Update(ctx, func(r *teamroster.Roster) error {
		a := teamroster.Agent{CreatedBy: teamroster.ByUser, CreatedAt: s.now()}
		applyUserAgent(&a, ch)
		out = r.AddAgent(a)
		return nil
	})
	return out, err
}

func applyUserAgent(a *teamroster.Agent, ch UserAgentChange) {
	applyAgent(a, ch.AgentChange)
	if ch.Permission != nil {
		a.Permission = *ch.Permission
	}
	if ch.Main != nil {
		a.Main = *ch.Main
	}
	if ch.Pinned != nil {
		a.Pinned = *ch.Pinned
	}
	if ch.Hidden != nil {
		a.Hidden = *ch.Hidden
	}
}

// UserUpdateAgent changes agent id as the user asked. Demoting the last main
// agent is refused by the roster.
func (s *Service) UserUpdateAgent(ctx context.Context, id string, ch UserAgentChange) (teamroster.Agent, error) {
	defer s.changed()
	var out teamroster.Agent
	_, err := s.store.Update(ctx, func(r *teamroster.Roster) error {
		a, ok := r.Agent(id)
		if !ok {
			return teamroster.ErrUnknownAgent
		}
		applyUserAgent(a, ch)
		out = *a
		return nil
	})
	return out, err
}

// UserDeleteAgent removes agent id from the team and from every group; a
// group it leaves with fewer than two members goes too. Its conversations stay
// on disk.
func (s *Service) UserDeleteAgent(ctx context.Context, id string) error {
	defer s.changed()
	_, err := s.store.Update(ctx, func(r *teamroster.Roster) error {
		if _, ok := r.Agent(id); !ok {
			return teamroster.ErrUnknownAgent
		}
		r.Agents = slices.DeleteFunc(r.Agents, func(a teamroster.Agent) bool { return a.ID == id })
		r.Groups = slices.DeleteFunc(r.Groups, func(g teamroster.Group) bool {
			if !g.IsMember(id) {
				return false
			}
			return len(g.Members) <= teamroster.MinMembers
		})
		for i := range r.Groups {
			g := &r.Groups[i]
			if !g.IsMember(id) {
				continue
			}
			g.Members = slices.DeleteFunc(g.Members, func(m string) bool { return m == id })
			delete(g.MemberSessions, id)
			if g.Admin == id {
				g.Admin, g.Mode = "", teamroster.ModeEveryone
			}
		}
		return nil
	})
	return err
}

// UserCreateGroup opens a group the user made.
func (s *Service) UserCreateGroup(ctx context.Context, ch UserGroupChange) (teamroster.Group, error) {
	defer s.changed()
	var out teamroster.Group
	_, err := s.store.Update(ctx, func(r *teamroster.Roster) error {
		g := teamroster.Group{Mode: teamroster.ModeEveryone, CreatedBy: teamroster.ByUser, CreatedAt: s.now()}
		applyUserGroup(&g, ch)
		if g.Admin != "" && ch.Mode == nil {
			g.Mode = teamroster.ModeAdmin
		}
		out = r.AddGroup(g)
		return nil
	})
	return out, err
}

func applyUserGroup(g *teamroster.Group, ch UserGroupChange) {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&g.Name, ch.Name)
	set(&g.Notice, ch.Notice)
	set(&g.Admin, ch.Admin)
	set(&g.Folder, ch.Folder)
	if ch.Members != nil {
		g.Members = slices.Clone(ch.Members)
		for m := range g.MemberSessions {
			if !g.IsMember(m) {
				delete(g.MemberSessions, m)
			}
		}
	}
	if ch.Mode != nil {
		g.Mode = *ch.Mode
	}
	if ch.RelayLimit != nil {
		g.RelayLimit = *ch.RelayLimit
	}
	if ch.Pinned != nil {
		g.Pinned = *ch.Pinned
	}
	if ch.Hidden != nil {
		g.Hidden = *ch.Hidden
	}
	if g.Admin == "" && g.Mode == teamroster.ModeAdmin {
		g.Mode = teamroster.ModeEveryone
	}
}

// UserUpdateGroup changes group id as the user asked.
func (s *Service) UserUpdateGroup(ctx context.Context, id string, ch UserGroupChange) (teamroster.Group, error) {
	defer s.changed()
	var out teamroster.Group
	_, err := s.store.Update(ctx, func(r *teamroster.Roster) error {
		g, ok := r.Group(id)
		if !ok {
			return teamroster.ErrUnknownGroup
		}
		applyUserGroup(g, ch)
		out = *g
		return nil
	})
	return out, err
}

// UserDeleteGroup removes group id for good; its log stays on disk.
func (s *Service) UserDeleteGroup(ctx context.Context, id string) error {
	defer s.changed()
	_, err := s.store.Update(ctx, func(r *teamroster.Roster) error {
		if _, ok := r.Group(id); !ok {
			return teamroster.ErrUnknownGroup
		}
		r.Groups = slices.DeleteFunc(r.Groups, func(g teamroster.Group) bool { return g.ID == id })
		return nil
	})
	return err
}
