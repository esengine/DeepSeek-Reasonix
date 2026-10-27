package tool

import (
	"context"
	"sync"
)

// PeerView is another session on this machine that can be written to.
type PeerView struct {
	Key     string
	Title   string
	Project string
}

// PeerReceipt says a message was handed to the receiver's spool, not read.
type PeerReceipt struct {
	ID string
	To PeerView
}

// PeerDirectory is what the peer tools ask of the session that owns them.
type PeerDirectory interface {
	Peers() []PeerView
	SendPeerMessage(to, text string) (PeerReceipt, error)
}

// DispatchReceipt says a task was written into a session of another project.
type DispatchReceipt struct {
	Project string
	Key     string
	ID      string
}

// ProjectDispatcher is what the dispatch tool asks of the session that owns it:
// hand a task to a session in another of the user's projects.
type ProjectDispatcher interface {
	DispatchToProject(ctx context.Context, project, text string) (DispatchReceipt, error)
}

// OpenedProject is what became of one folder the model asked to have opened.
// Code is the refusal identity when it was not; an empty Code with Added false
// means the folder was already in the list.
type OpenedProject struct {
	Path  string
	Added bool
	Code  string
}

// ProjectOpener is what the open_projects tool asks of the session that owns
// it: add folders to the user's project list as trusted, after the user has
// answered a card that names every one of them.
type ProjectOpener interface {
	OpenProjects(ctx context.Context, paths []string) ([]OpenedProject, error)
}

// PeerLink joins the peer tools, built with the registry, to the controller
// that exists only afterwards. Until the controller binds it the tools answer
// that no session is listening.
type PeerLink struct {
	mu  sync.RWMutex
	dir PeerDirectory
}

func (l *PeerLink) Bind(d PeerDirectory) {
	l.mu.Lock()
	l.dir = d
	l.mu.Unlock()
}

func (l *PeerLink) Directory() (PeerDirectory, bool) {
	if l == nil {
		return nil, false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.dir, l.dir != nil
}
