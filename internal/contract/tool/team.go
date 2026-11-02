package tool

import (
	"context"
	"sync"
)

// TeamMessage is what message_agent asks to send.
type TeamMessage struct {
	To        string
	Message   string
	ReplyTo   string
	Now       bool
	RequestID string
}

// TeamReceipt is message_agent's answer: where the message went and whether it
// was new or a retry of one already sent.
type TeamReceipt struct {
	Exchange string
	To       string
	ToName   string
	State    string
}

// TeamAgentSpec is what create_agent and update_agent set; a nil field is
// left as it is.
type TeamAgentSpec struct {
	Name         *string
	Role         *string
	Instructions *string
	Model        *string
	Folder       *string
	Glyph        *string
	Color        *string
}

// TeamAgentView is an agent as a team tool reports it.
type TeamAgentView struct {
	ID   string
	Name string
	Role string
	Main bool
}

// TeamGroupSpec is what create_group asks for.
type TeamGroupSpec struct {
	Name    string
	Notice  string
	Members []string
	Admin   string
	Mode    string
}

// TeamGroupChange is what update_group sets. Admin "" removes the admin.
type TeamGroupChange struct {
	Name   *string
	Notice *string
	Add    []string
	Remove []string
	Admin  *string
	Mode   *string
}

// TeamDirectory is what the team tools ask of the agent session that owns
// them. The session knows which agent, group and turn a call comes from; the
// tools pass only what the model asked for.
type TeamDirectory interface {
	ListAgents(ctx context.Context) (string, error)
	MessageAgent(ctx context.Context, m TeamMessage) (TeamReceipt, error)
	CreateAgent(ctx context.Context, spec TeamAgentSpec) (TeamAgentView, error)
	UpdateAgent(ctx context.Context, target string, spec TeamAgentSpec) (TeamAgentView, error)
	StopAgent(ctx context.Context, target string) (bool, error)
	CreateGroup(ctx context.Context, spec TeamGroupSpec) (string, error)
	UpdateGroup(ctx context.Context, group string, ch TeamGroupChange) ([]string, error)
	DeleteGroup(ctx context.Context, group string) error
	PostToGroup(ctx context.Context, group, text string, mentions []string) error
	PassTurn(ctx context.Context) error
	HandTurn(ctx context.Context, to []string) error
	ReadGroup(ctx context.Context, group string, before int64, limit int) (string, error)
	// SharedMemory is the memory tool bound to a store agents share — tier
	// "team" or "group" — for writing when write is set.
	SharedMemory(ctx context.Context, tier, group string, write bool) (Tool, error)
}

// TeamLink joins the team tools, built with the registry, to the controller
// that exists only afterwards.
type TeamLink struct {
	mu  sync.RWMutex
	dir TeamDirectory
}

func (l *TeamLink) Bind(d TeamDirectory) {
	l.mu.Lock()
	l.dir = d
	l.mu.Unlock()
}

func (l *TeamLink) Directory() (TeamDirectory, bool) {
	if l == nil {
		return nil, false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.dir, l.dir != nil
}
