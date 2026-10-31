package serve

import (
	"os"
	"strings"

	"reasonix/internal/session/control"
	"reasonix/internal/session/team"
	"reasonix/internal/state/teamroster"
)

// teamView is the team as the window draws it: the roster, and what each
// agent's open sessions are doing now.
type teamView struct {
	Revision uint64          `json:"revision"`
	ReadOnly bool            `json:"readOnly,omitempty"`
	Agents   []teamAgentView `json:"agents"`
	Groups   []teamGroupView `json:"groups"`
	Prefs    teamPrefsView   `json:"prefs"`
}

// teamPrefsView is what the user chose for the whole team.
type teamPrefsView struct {
	MemoryReview bool `json:"memoryReview"`
}

type teamAvatarView struct {
	Glyph string `json:"glyph,omitempty"`
	Color string `json:"color,omitempty"`
	Image string `json:"image,omitempty"`
}

// teamActivityView is what an agent is doing, read off its open sessions and
// the messages it is waiting on. Runtime names the session a "needs you"
// prompt is waiting in.
type teamActivityView struct {
	State   string   `json:"state"`
	On      []string `json:"on,omitempty"`
	Runtime string   `json:"runtime,omitempty"`
}

// teamUsageView is what an agent's conversations have spent, summed over its
// own chat, its earlier parts and its seats in groups.
type teamUsageView struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	CacheHitTokens   int `json:"cacheHitTokens"`
	Requests         int `json:"requests"`
}

type teamAgentView struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Role           string           `json:"role,omitempty"`
	Instructions   string           `json:"instructions,omitempty"`
	Avatar         teamAvatarView   `json:"avatar"`
	Model          string           `json:"model,omitempty"`
	Folder         string           `json:"folder,omitempty"`
	Permission     string           `json:"permission,omitempty"`
	Main           bool             `json:"main,omitempty"`
	Pinned         bool             `json:"pinned,omitempty"`
	Hidden         bool             `json:"hidden,omitempty"`
	Runtime        string           `json:"runtime,omitempty"`
	LastActivityAt int64            `json:"lastActivityAt,omitempty"`
	Preview        string           `json:"preview,omitempty"`
	Activity       teamActivityView `json:"activity"`
	Usage          teamUsageView    `json:"usage"`
}

type teamRoundView struct {
	Speaking string   `json:"speaking,omitempty"`
	Queue    []string `json:"queue"`
	Working  []string `json:"working"`
	Parked   int      `json:"parked,omitempty"`
	Relays   int      `json:"relays,omitempty"`
	Limit    int      `json:"limit"`
}

type teamGroupView struct {
	ID             string        `json:"id"`
	Name           string        `json:"name,omitempty"`
	Notice         string        `json:"notice,omitempty"`
	Members        []string      `json:"members"`
	Admin          string        `json:"admin,omitempty"`
	Mode           string        `json:"mode"`
	RelayLimit     int           `json:"relayLimit,omitempty"`
	Folder         string        `json:"folder,omitempty"`
	Pinned         bool          `json:"pinned,omitempty"`
	Hidden         bool          `json:"hidden,omitempty"`
	Archived       bool          `json:"archived,omitempty"`
	LastSeq        int64         `json:"lastSeq,omitempty"`
	LastActivityAt int64         `json:"lastActivityAt,omitempty"`
	Preview        string        `json:"preview,omitempty"`
	Round          teamRoundView `json:"round"`
}

// view assembles the team as it stands.
func (t *hubTeam) view() (teamView, error) {
	r, err := t.store.Load()
	if err != nil {
		return teamView{}, err
	}
	book, err := t.store.Exchanges()
	if err != nil {
		return teamView{}, err
	}
	out := teamView{Revision: r.Revision, ReadOnly: t.service() == nil, Agents: []teamAgentView{}, Groups: []teamGroupView{},
		Prefs: teamPrefsView{MemoryReview: r.Prefs.MemoryReviewOn && !r.Prefs.MemoryReviewOff}}
	for _, a := range r.Agents {
		v := t.agentView(a, book)
		v.Usage = t.agentUsage(r, a)
		out.Agents = append(out.Agents, v)
	}
	for _, g := range r.Groups {
		out.Groups = append(out.Groups, t.groupView(g))
	}
	return out, nil
}

func (t *hubTeam) agentView(a teamroster.Agent, book []teamroster.Exchange) teamAgentView {
	v := teamAgentView{
		ID: a.ID, Name: a.Name, Role: a.Role, Instructions: a.Instructions,
		Avatar: teamAvatarView{Glyph: a.Avatar.Glyph, Color: a.Avatar.Color, Image: photoURL(a.Avatar.Image)}, Model: a.InitialModel,
		Folder: a.Folder, Permission: a.Permission, Main: a.Main, Pinned: a.Pinned, Hidden: a.Hidden,
	}
	if rt, ok := t.liveRuntime(team.SeatRef{Agent: a.ID}); ok {
		v.Runtime = rt.ID
		if ref := rt.Server.Controller().ModelRef(); ref != "" {
			v.Model = ref
		}
	}
	if a.SessionPath != "" {
		if info, err := os.Stat(a.SessionPath); err == nil {
			v.LastActivityAt = info.ModTime().UnixMilli()
			v.Preview = t.preview(a.SessionPath)
		}
	}
	v.Activity = t.activity(a.ID, book)
	return v
}

// activity is read in order of what the user most needs to know: a session of
// this agent waiting on them, then one running, then work it handed on and is
// waiting to get back.
func (t *hubTeam) activity(agent string, book []teamroster.Exchange) teamActivityView {
	t.mu.Lock()
	var ids []string
	for seat, id := range t.seats {
		if seat.Agent == agent {
			ids = append(ids, id)
		}
	}
	t.mu.Unlock()
	running := false
	for _, id := range ids {
		rt := t.h.Get(id)
		if rt == nil {
			continue
		}
		ctrl, ok := rt.Server.Controller().(*control.Controller)
		if !ok {
			continue
		}
		if ctrl.PendingPrompt() {
			return teamActivityView{State: "needs_you", Runtime: id}
		}
		running = running || ctrl.Running()
	}
	if running {
		return teamActivityView{State: "working"}
	}
	var on []string
	for _, x := range book {
		if x.From == agent && x.ReplyTo == teamroster.ReplyToSender && !x.State.Settled() {
			on = append(on, x.To)
		}
	}
	if len(on) > 0 {
		return teamActivityView{State: "waiting_on", On: on}
	}
	return teamActivityView{State: "idle"}
}

func (t *hubTeam) groupView(g teamroster.Group) teamGroupView {
	v := teamGroupView{
		ID: g.ID, Name: g.Name, Notice: g.Notice, Members: append([]string{}, g.Members...), Admin: g.Admin,
		Mode: string(g.Mode), RelayLimit: g.RelayLimit, Folder: g.Folder, Pinned: g.Pinned, Hidden: g.Hidden, Archived: g.Archived,
		Round: teamRoundView{Queue: []string{}, Working: []string{}, Limit: team.RelayLimit(g)},
	}
	if lines, err := t.lastGroupLine(g.ID); err == nil && len(lines) == 1 {
		v.LastSeq, v.LastActivityAt = lines[0].Seq, lines[0].At.UnixMilli()
		v.Preview = strings.TrimSpace(lines[0].Text)
	}
	if rs, err := t.groupRound(g.ID); err == nil {
		if rs.Speaking != nil {
			v.Round.Speaking = rs.Speaking.Member
		}
		for _, s := range rs.Queue {
			v.Round.Queue = append(v.Round.Queue, s.Member)
		}
		v.Round.Working = append(v.Round.Working, rs.Working...)
		v.Round.Parked, v.Round.Relays = len(rs.Parked), rs.Relays
	}
	return v
}

// agentUsage sums the usage reports of every conversation agent a has had.
func (t *hubTeam) agentUsage(r teamroster.Roster, a teamroster.Agent) teamUsageView {
	paths := []string{a.SessionPath}
	for _, p := range a.Parts {
		paths = append(paths, p.SessionPath)
	}
	for _, g := range r.Groups {
		paths = append(paths, g.MemberSessions[a.ID])
	}
	var u teamUsageView
	for _, p := range paths {
		if p == "" {
			continue
		}
		if rep, ok := t.usage(p); ok {
			u.PromptTokens += rep.Usage.PromptTokens
			u.CompletionTokens += rep.Usage.CompletionTokens
			u.CacheHitTokens += rep.Usage.CacheHitTokens
			u.Requests += rep.Usage.RequestCount
		}
	}
	return u
}
