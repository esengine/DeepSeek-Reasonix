package control

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/peermail"
)

// Each is an identity the dispatch tool reports; none is matched by its words.
var (
	ErrDispatchDisabled       = errors.New(`the user has not turned on "Let the model hand a task to another session" (Settings > Tools > Messages between sessions, which also needs "enabled"; [peers] dispatch in the user config)`)
	ErrDispatchUnavailable    = errors.New("this session has no window to open another session in (it is a terminal or protocol session): open the project in Reasonix Studio or the web UI to dispatch from it")
	ErrDispatchNotLocal       = errors.New("this pane is not one of this machine's own sessions (it is driven over SSH or by another window), so it cannot open a session here")
	ErrDispatchUnknownProject = errors.New("no project of yours answers to that name")
	ErrDispatchAmbiguous      = errors.New("more than one project answers to that name")
	ErrDispatchNested         = errors.New("this session is as many dispatches deep as your ceiling allows (peers.max_dispatch_depth in the user config; 0 removes it)")
	ErrDispatchUntrusted      = errors.New("the user declined to trust that project, so no task is sent there; they can change it by opening the project in a window and trusting it")
	ErrDispatchAwaitingTrust  = errors.New("the user has not yet answered whether to trust that project; its window is open and shows the question, and your task was not sent (do not retry in a loop: ask the user to answer it there, then dispatch again)")
	ErrDispatchSameProject    = errors.New("that is this session's own project: no other window is needed, do the work here or use a sub-agent")
	ErrDispatchFailed         = errors.New("could not open a session in that project")
	ErrDispatchLimit          = errors.New("this session already has as many dispatched sessions open as your ceiling allows (peers.max_dispatched_sessions)")
)

// DispatchResult is what a dispatch hands back: where the task went.
type DispatchResult struct {
	Project string
	Key     string
	ID      string
}

// ProjectDispatcher opens, or reuses, a session in another project of the user
// and writes the task into it. The host that owns the panes supplies it.
type ProjectDispatcher func(ctx context.Context, project, text string) (DispatchResult, error)

// dispatchState is the controller's side of cross-project dispatch.
type dispatchState struct {
	hook   atomic.Pointer[ProjectDispatcher]
	opener atomic.Pointer[ProjectOpener]
	// mu guards the relation: a partner is a session this one opened or the one
	// that opened it, paired by a secret the host keeps in memory. The spool only
	// carries messages; who sent one is proved with the secret, never believed.
	mu     sync.Mutex
	opened bool
	depth  int
	pairs  map[string]*dispatchPair
}

// dispatchWindow is how many accepted sequence numbers a pairing remembers; one
// older than the newest by more than this is refused unseen.
const dispatchWindow = 4096

// dispatchPair is one pairing: the secret, the next number to sign with, and the
// numbers taken from the partner. It lives as long as the secret does, in memory
// only, so a message copied out of the spool cannot be replayed into this pairing
// however many others have been read since.
type dispatchPair struct {
	secret []byte
	sent   uint64
	seen   map[uint64]bool
	floor  uint64
}

// accept records seq as taken, or refuses it when it was taken or has aged out.
func (p *dispatchPair) accept(seq uint64) bool {
	if seq == 0 || seq <= p.floor || p.seen[seq] {
		return false
	}
	if p.seen == nil {
		p.seen = map[uint64]bool{}
	}
	p.seen[seq] = true
	if len(p.seen) > dispatchWindow {
		var newest uint64
		for n := range p.seen {
			newest = max(newest, n)
		}
		p.floor = newest - dispatchWindow
		for n := range p.seen {
			if n <= p.floor {
				delete(p.seen, n)
			}
		}
	}
	return true
}

// SetProjectDispatcher gives the dispatch tool somewhere to go; a controller
// without one (a terminal session, a test) answers ErrDispatchUnavailable.
func (c *Controller) SetProjectDispatcher(d ProjectDispatcher) {
	if d == nil {
		c.dispatch.hook.Store(nil)
		return
	}
	c.dispatch.hook.Store(&d)
}

// MarkOpenedByDispatch records that the session filed under parent opened this
// one to hand it a task, with the secret the host made for the pair.
func (c *Controller) MarkOpenedByDispatch(parent string, secret []byte, depth int) {
	c.dispatch.mu.Lock()
	defer c.dispatch.mu.Unlock()
	c.dispatch.opened, c.dispatch.depth = true, depth
	c.rememberPairLocked(parent, secret)
}

// OpenedByDispatch reports whether MarkOpenedByDispatch was called.
func (c *Controller) OpenedByDispatch() bool {
	c.dispatch.mu.Lock()
	defer c.dispatch.mu.Unlock()
	return c.dispatch.opened
}

// DispatchDepth is how many dispatches deep this session is: 0 for one the user
// opened, one more than its opener's for one a dispatch opened.
func (c *Controller) DispatchDepth() int {
	c.dispatch.mu.Lock()
	defer c.dispatch.mu.Unlock()
	return c.dispatch.depth
}

// NoteDispatchedSession records a session this one opened for a task and the
// secret the host made for the pair.
func (c *Controller) NoteDispatchedSession(child string, secret []byte) {
	c.dispatch.mu.Lock()
	defer c.dispatch.mu.Unlock()
	c.rememberPairLocked(child, secret)
}

func (c *Controller) rememberPairLocked(key string, secret []byte) {
	if key == "" || len(secret) == 0 {
		return
	}
	if c.dispatch.pairs == nil {
		c.dispatch.pairs = map[string]*dispatchPair{}
	}
	c.dispatch.pairs[key] = &dispatchPair{secret: append([]byte(nil), secret...)}
}

// signDispatch numbers and signs a message to a dispatch partner, and leaves it
// alone when the target is not one.
func (c *Controller) signDispatch(m *peermail.Message) {
	c.dispatch.mu.Lock()
	defer c.dispatch.mu.Unlock()
	pair := c.dispatch.pairs[m.To.SessionID]
	if pair == nil {
		return
	}
	pair.sent++
	m.Seq = pair.sent
	m.Proof = peermail.Proof(pair.secret, *m)
}

// dispatchPartner reports whether the message proves it came from a session this
// one dispatched to or was dispatched by, and that it is the first time it is
// taken: a proof is valid once per sequence number.
func (c *Controller) dispatchPartner(m peermail.Message) bool {
	c.dispatch.mu.Lock()
	defer c.dispatch.mu.Unlock()
	pair := c.dispatch.pairs[m.From.SessionID]
	return pair != nil && peermail.ProofValid(pair.secret, m) && pair.accept(m.Seq)
}

// PeerKey is the address other sessions write to this one at, empty until the
// session has a file.
func (c *Controller) PeerKey() string {
	p := c.peers
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.key
}

// TrustOfWorkspace is the user's recorded answer about another project's folder;
// an unreadable record is undecided.
func (c *Controller) TrustOfWorkspace(root string) config.WorkspaceTrust {
	trust, _ := config.NewProjectGrantStore(c.posture.Home).Trust(root)
	return trust
}

// DispatchToProject hands a task to a session in another of the user's projects.
func (c *Controller) DispatchToProject(ctx context.Context, project, text string) (DispatchResult, error) {
	cfg := config.LoadForEdit(config.UserConfigPath())
	if !cfg.PeersDispatch() {
		return DispatchResult{}, ErrDispatchDisabled
	}
	if limit := cfg.PeersMaxDispatchDepth(); limit > 0 && c.DispatchDepth() >= limit {
		return DispatchResult{}, ErrDispatchNested
	}
	hook := c.dispatch.hook.Load()
	if hook == nil {
		return DispatchResult{}, ErrDispatchUnavailable
	}
	return (*hook)(ctx, project, text)
}

func (d peerDirectory) DispatchToProject(ctx context.Context, project, text string) (tool.DispatchReceipt, error) {
	r, err := d.c.DispatchToProject(ctx, project, text)
	if err != nil {
		return tool.DispatchReceipt{}, fmtPeerError(err)
	}
	return tool.DispatchReceipt{Project: r.Project, Key: r.Key, ID: r.ID}, nil
}

// RefreshPeers reads this session's spool now rather than at the next tick, so
// a message just sent to it can be seen at once.
func (c *Controller) RefreshPeers() { c.peerIngest() }
