package control

import (
	"context"
	"errors"
	"slices"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/session/team"
	"reasonix/internal/state/memory"
	"reasonix/internal/state/teamroster"
)

// approvalStrictness orders the approval modes from the least a turn may do
// unattended to the most; an unknown mode ranks with ask.
func approvalStrictness(mode string) int {
	switch normalizeToolApprovalMode(mode) {
	case ToolApprovalReadOnly:
		return 0
	case ToolApprovalDontAsk:
		return 1
	case ToolApprovalAuto:
		return 3
	case ToolApprovalYolo:
		return 4
	}
	return 2
}

// stricterApproval is whichever of two approval modes lets a turn do less on
// its own; an empty mode imposes nothing.
func stricterApproval(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	case approvalStrictness(b) < approvalStrictness(a):
		return b
	}
	return a
}

// teamTurn is the team side of the turn in flight, read from the inbox items
// it is running: the exchanges it serves, the replies it reads, the group turn
// it is, and the strictest ceiling any of them carries.
type teamTurn struct {
	id       string
	serving  []string
	consumed []string
	steps    []string
	ceiling  string
}

func (c *Controller) activeInboxItems() []string {
	c.inbox.mu.Lock()
	ids := make([]string, 0, len(c.inbox.activeItemIDs))
	for id := range c.inbox.activeItemIDs {
		ids = append(ids, id)
	}
	c.inbox.mu.Unlock()
	slices.Sort(ids)
	return ids
}

func (c *Controller) currentTeamTurn() teamTurn {
	if c.team.bound() == nil {
		return teamTurn{}
	}
	ids := c.activeInboxItems()
	t := teamTurn{id: strings.Join(ids, ",")}
	if len(ids) == 0 {
		return t
	}
	st, err := c.ensureInbox()
	if err != nil {
		return t
	}
	for _, id := range ids {
		_, env, err := st.ReadItem(id)
		if err != nil {
			continue
		}
		x := env.Extra
		switch team.DeliveryKind(x[team.ExtraKind]) {
		case team.DeliverUserMessage, team.DeliverSchedule, team.DeliverSecret:
		case team.DeliverMessage:
			t.serving = append(t.serving, x[team.ExtraExchange])
		case team.DeliverReply:
			t.consumed = append(t.consumed, x[team.ExtraExchange])
			if s := x[team.ExtraServing]; s != "" {
				t.serving = append(t.serving, strings.Split(s, ",")...)
			}
		case team.DeliverGroupTurn:
			t.steps = append(t.steps, x[team.ExtraStep])
		default:
			continue
		}
		t.ceiling = stricterApproval(t.ceiling, x[team.ExtraCeiling])
	}
	return t
}

// teamDirectory is the controller as the team tools see it: it adds who is
// calling, from which seat and turn, to what the model asked for.
type teamDirectory struct{ c *Controller }

var errTeamUnbound = tool.Refusal{Code: "team.disabled", Message: "this session is not an agent on the user's team"}

func (d teamDirectory) caller(ctx context.Context) (*team.Service, team.Caller, error) {
	if err := team.CheckPlatform(); err != nil {
		return nil, team.Caller{}, teamRefusal(err)
	}
	seat := d.c.team.bound()
	if seat == nil {
		return nil, team.Caller{}, errTeamUnbound
	}
	svc, ref := seat.Team()
	if svc == nil {
		return nil, team.Caller{}, errTeamUnbound
	}
	t := d.c.currentTeamTurn()
	if t.id == "" {
		t.id = delegation.TurnIdentity(ctx)
	}
	var step string
	if len(t.steps) > 0 {
		step = t.steps[len(t.steps)-1]
	}
	ceiling := stricterApproval(d.c.effectiveApprovalMode(), t.ceiling)
	if !d.c.PlanPhase().AdmitsSideEffects() {
		ceiling = ToolApprovalReadOnly
	}
	return svc, team.Caller{
		Agent: ref.Agent, Group: ref.Group, Turn: t.id, Step: step, Serving: t.serving,
		Ceiling: ceiling,
	}, nil
}

func (d teamDirectory) ListAgents(ctx context.Context) (string, error) {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return "", err
	}
	out, err := svc.ListAgents(c)
	return out, teamRefusal(err)
}

func (d teamDirectory) MessageAgent(ctx context.Context, m tool.TeamMessage) (tool.TeamReceipt, error) {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return tool.TeamReceipt{}, err
	}
	r, err := svc.MessageAgent(ctx, c, team.MessageRequest{To: m.To, Message: m.Message, ReplyTo: teamroster.ReplyTo(m.ReplyTo), Steer: m.Now, RequestID: m.RequestID})
	if err != nil {
		return tool.TeamReceipt{}, teamRefusal(err)
	}
	return tool.TeamReceipt{Exchange: r.Exchange, To: r.To, ToName: r.ToName, State: string(r.State)}, nil
}

func agentChange(s tool.TeamAgentSpec) team.AgentChange {
	ch := team.AgentChange{Name: s.Name, Role: s.Role, Instructions: s.Instructions, Model: s.Model, Folder: s.Folder}
	if s.Glyph != nil || s.Color != nil {
		var a teamroster.Avatar
		if s.Glyph != nil {
			a.Glyph = *s.Glyph
		}
		if s.Color != nil {
			a.Color = *s.Color
		}
		ch.Avatar = &a
	}
	return ch
}

func agentView(a teamroster.Agent) tool.TeamAgentView {
	return tool.TeamAgentView{ID: a.ID, Name: a.Name, Role: a.Role, Main: a.Main}
}

func (d teamDirectory) CreateAgent(ctx context.Context, s tool.TeamAgentSpec) (tool.TeamAgentView, error) {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return tool.TeamAgentView{}, err
	}
	if err := svc.CheckCreateAgent(c); err != nil {
		return tool.TeamAgentView{}, teamRefusal(err)
	}
	if err := d.approveFolder(ctx, derefOr(s.Name, "new agent"), s.Folder); err != nil {
		return tool.TeamAgentView{}, err
	}
	a, err := svc.CreateAgent(ctx, c, agentChange(s))
	return agentView(a), teamRefusal(err)
}

// approveFolder holds a model-chosen working folder to the person's answer.
// Returning an agent to its own folder narrows what it reaches and asks no one.
func (d teamDirectory) approveFolder(ctx context.Context, agent string, folder *string) error {
	if folder == nil || *folder == "" {
		return nil
	}
	return d.c.confirmTeamChange(ctx, agent, *folder)
}

func derefOr(s *string, fallback string) string {
	if s == nil || *s == "" {
		return fallback
	}
	return *s
}

func (d teamDirectory) UpdateAgent(ctx context.Context, target string, s tool.TeamAgentSpec) (tool.TeamAgentView, error) {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return tool.TeamAgentView{}, err
	}
	a, err := svc.AgentForUpdate(c, target)
	if err != nil {
		return tool.TeamAgentView{}, teamRefusal(err)
	}
	change := agentChange(s)
	if change.RequiresApproval(a) {
		if err := d.c.confirmTeamConfiguration(ctx, a.ID, a.Name, s); err != nil {
			return tool.TeamAgentView{}, err
		}
		c.ConfigurationApproved = true
	}
	a, err = svc.UpdateAgent(ctx, c, a.ID, change)
	return agentView(a), teamRefusal(err)
}

func (d teamDirectory) StopAgent(ctx context.Context, target string) (bool, error) {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return false, err
	}
	stopped, err := svc.StopAgent(c, target)
	return stopped, teamRefusal(err)
}

func (d teamDirectory) CreateGroup(ctx context.Context, s tool.TeamGroupSpec) (string, error) {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return "", err
	}
	g, err := svc.CreateGroup(ctx, c, s.Name, s.Notice, s.Members, s.Admin, teamroster.Mode(s.Mode))
	return g.ID, teamRefusal(err)
}

func (d teamDirectory) UpdateGroup(ctx context.Context, group string, ch tool.TeamGroupChange) ([]string, error) {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return nil, err
	}
	id, err := svc.GroupID(group)
	if err != nil {
		return nil, teamRefusal(err)
	}
	gc := team.GroupChange{Name: ch.Name, Notice: ch.Notice, Add: ch.Add, Remove: ch.Remove, Admin: ch.Admin}
	if ch.Mode != nil {
		m := teamroster.Mode(*ch.Mode)
		gc.Mode = &m
	}
	changed, err := svc.UpdateGroup(ctx, c, id, gc)
	return changed, teamRefusal(err)
}

func (d teamDirectory) DeleteGroup(ctx context.Context, group string) error {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return err
	}
	id, err := svc.GroupID(group)
	if err != nil {
		return teamRefusal(err)
	}
	return teamRefusal(svc.DeleteGroup(ctx, c, id))
}

func (d teamDirectory) PostToGroup(ctx context.Context, group, text string, mentions []string) error {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return err
	}
	id, err := svc.GroupID(group)
	if err != nil {
		return teamRefusal(err)
	}
	_, err = svc.AgentPost(ctx, c, id, text, mentions)
	return teamRefusal(err)
}

func (d teamDirectory) PassTurn(ctx context.Context) error {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return err
	}
	return teamRefusal(svc.PassTurn(c))
}

func (d teamDirectory) HandTurn(ctx context.Context, to []string) error {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return err
	}
	return teamRefusal(svc.HandTurn(c, to))
}

func (d teamDirectory) ReadGroup(ctx context.Context, group string, before int64, limit int) (string, error) {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return "", err
	}
	id, err := svc.GroupID(group)
	if err != nil {
		return "", teamRefusal(err)
	}
	out, err := svc.ReadGroup(c, id, before, limit)
	return out, teamRefusal(err)
}

// teamCodes names each refusal the team can give. A field error carries its
// field in the message; its code says only what kind of field error it is.
var teamCodes = []struct {
	err  error
	code string
}{
	{team.ErrUnsupportedPlatform, "team.disabled"},
	{team.ErrConfigurationApproval, "team.approval_required"},
	{teamroster.ErrAgentCapacity, "team.limit"},
	{teamroster.ErrGroupCapacity, "team.limit"},
	{teamroster.ErrSecretCredentials, "team.credentials_unavailable"},
	{team.ErrSelf, "team.self"},
	{team.ErrEmptyMessage, "team.invalid"},
	{team.ErrMessageTooLong, "team.too_long"},
	{team.ErrUpstream, "team.upstream"},
	{team.ErrDepth, "team.depth"},
	{team.ErrOpenLimit, "team.limit"},
	{team.ErrCreateLimit, "team.limit"},
	{team.ErrRateLimited, "team.rate_limited"},
	{team.ErrPrivateOnly, "team.private_only"},
	{team.ErrUnavailable, "team.unavailable"},
	{team.ErrNotInGroup, "team.not_in_group"},
	{team.ErrNotYourTurn, "team.not_your_turn"},
	{team.ErrHandSize, "team.invalid"},
	{team.ErrNotMain, "team.not_main"},
	{team.ErrSelfOnly, "team.self_only"},
	{team.ErrNotAdmin, "team.not_admin"},
	{team.ErrArchived, "team.archived"},
	{teamroster.ErrUnknownAgent, "team.unknown_agent"},
	{teamroster.ErrUnknownGroup, "team.unknown_group"},
	{teamroster.ErrNotMember, "team.not_member"},
	{teamroster.ErrGroupSize, "team.group_size"},
	{teamroster.ErrNameTaken, "team.name_taken"},
	{teamroster.ErrLastMain, "team.last_main"},
	{teamroster.ErrRequestReused, "team.request_reused"},
	{teamroster.ErrReadOnly, "team.read_only"},
	{teamroster.ErrConflict, "team.conflict"},
	{teamroster.ErrTooLong, "team.too_long"},
	{teamroster.ErrEmpty, "team.invalid"},
	{teamroster.ErrInvalid, "team.invalid"},
}

// teamRefusal gives a team error the identity the model reads. An error the
// team does not name is the host's own failure and passes through as it is.
func teamRefusal(err error) error {
	if err == nil {
		return nil
	}
	for _, c := range teamCodes {
		if errors.Is(err, c.err) {
			return tool.Refusal{Code: c.code, Message: err.Error()}
		}
	}
	return err
}

func (d teamDirectory) SharedMemory(ctx context.Context, tier, group string, write bool) (tool.Tool, error) {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return nil, err
	}
	store, err := svc.SharedMemory(c, team.SharedTier(tier), group, write)
	if err != nil {
		return nil, teamRefusal(err)
	}
	if write {
		logGroup := c.Group
		if tier == string(team.TierGroup) && group != "" {
			logGroup, err = svc.GroupID(group)
			if err != nil {
				return nil, teamRefusal(err)
			}
		}
		return teamMemoryWriter{Tool: memory.NewRememberTool(store), controller: d.c, service: svc, agent: c.Agent, group: logGroup}, nil
	}
	return memory.NewRecallTool(store), nil
}

func (d teamDirectory) RequestSecret(ctx context.Context, name, purpose string) (string, error) {
	svc, c, err := d.caller(ctx)
	if err != nil {
		return "", err
	}
	out, err := svc.RequestSecret(ctx, c, name, purpose)
	return out, teamRefusal(err)
}
