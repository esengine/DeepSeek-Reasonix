package team

import (
	"sync"
	"time"

	"reasonix/internal/state/teamroster"
)

// Options assembles a Service. OnChange, when set, is told after a call that
// may have changed the team, so a window showing it can read it again.
type Options struct {
	Store    teamroster.Store
	Panes    Panes
	Now      func() time.Time
	OnChange func()
}

// Service is the team's orchestration in the one process that holds the
// team's writer lease.
type Service struct {
	store    teamroster.Store
	panes    Panes
	now      func() time.Time
	onChange func()

	mu    sync.Mutex
	sends sendLimits

	rounds roundBook
}

// New returns a Service over o.
func New(o Options) *Service {
	o.Store = WithSecretCredentials(o.Store)
	now := o.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: o.Store, panes: o.Panes, now: now, onChange: o.OnChange}
}

func (s *Service) changed() {
	if s.onChange != nil {
		s.onChange()
	}
}

// Caller is the turn a team tool was called from, as the host knows it. Step
// is the group turn it is, when it is one.
type Caller struct {
	Agent   string
	Group   string
	Turn    string
	Step    string
	Serving []string
	Ceiling string
	// ConfigurationApproved means the host obtained fresh human approval for this exact change.
	ConfigurationApproved bool
}

// MessageRequest is what message_agent asks for. To is an agent id or its
// exact name.
type MessageRequest struct {
	To        string
	Message   string
	ReplyTo   teamroster.ReplyTo
	Steer     bool
	RequestID string
}

// SendState is what became of a message_agent call.
type SendState string

const (
	SendDelivered SendState = "delivered"
	SendDuplicate SendState = "duplicate"
)

// MessageReceipt is message_agent's answer to the caller.
type MessageReceipt struct {
	Exchange string
	To       string
	ToName   string
	State    SendState
}

// Projection is what a seat's next turn is owed about the team, rendered from
// the roster as it stands: who the agent is, who else is on the team, and in a
// group seat, the group.
type Projection struct {
	Identity string
	Roster   string
	Group    string
	Memory   string
}

// Projection renders seat's projection. The same roster gives the same bytes,
// so the controller can tell an unchanged team from a changed one by content.
// A group seat whose agent has left the group, or whose group is gone, projects
// no group.
func (s *Service) Projection(seat SeatRef) (Projection, error) {
	r, err := s.store.Load()
	if err != nil {
		return Projection{}, err
	}
	a, ok := r.Agent(seat.Agent)
	if !ok {
		return Projection{}, teamroster.ErrUnknownAgent
	}
	p := Projection{Identity: renderIdentity(*a), Roster: renderRoster(r, a.ID)}
	inGroup := false
	if g, ok := r.Group(seat.Group); ok && g.IsMember(a.ID) {
		p.Group, inGroup = renderGroup(r, *g), true
	}
	p.Memory = s.renderMemory(seat, inGroup)
	return p, nil
}
