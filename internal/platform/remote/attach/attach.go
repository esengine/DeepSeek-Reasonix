// Package attach turns a configured SSH host into a reachable kernel: it dials
// the connection, makes sure a `reasonix serve` is running for a workspace on
// the other side, and forwards it to a local address a frontend can call.
//
// One connection carries every workspace opened on that host, and one forward
// carries every pane opened on that workspace, so both are reference-counted
// and the last holder out tears them down. The remote serve is left running:
// it outlives the link by design, and the next connect reuses it.
package attach

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"reasonix/internal/contract/config"
	"reasonix/internal/platform/releaseasset"
	"reasonix/internal/platform/remote"
	"reasonix/internal/platform/remote/bootstrap"
	"reasonix/internal/platform/remote/forward"
)

// Broker is this machine's provider broker: the loopback address it listens on
// here, and the token a remote kernel authenticates to it with. Set, every
// workspace opened through this pool resolves providers back over the tunnel,
// so the far host needs neither an API key nor egress of its own. Zero leaves
// each host resolving providers from its own config.
type Broker struct {
	Addr  string
	Token string
}

func (b Broker) configured() bool { return b.Addr != "" && b.Token != "" }

// Options is what every attach on this pool shares: who answers a credential
// prompt, and where a remote install comes from when the host has no reasonix.
type Options struct {
	Prompts     Prompts
	Broker      Broker
	Install     string // auto|npm|upload|never; empty => the host entry's own
	LocalBinary string // this process's binary, for a same-platform upload
	Version     string // this release, for a verified cross-platform download
	FetchBinary func(ctx context.Context, version, goos, goarch string) ([]byte, error)
	// ResolveDownload names the release archive so a host can fetch its own
	// kernel, which is the route that spends nobody's uplink.
	ResolveDownload func(ctx context.Context, version, goos, goarch string) (releaseasset.CLIDownload, error)
	// Dial builds a host's connection. Nil resolves it from configuration —
	// which is the only part of an attach that needs a configured machine, so
	// substituting it is what lets the rest be exercised against a real one.
	Dial func(host string, prompts Prompts) (*remote.Client, error)
}

// Call is what one attach decides for itself: where the progress of a first
// connect is reported, and who watches the link's state afterwards.
type Call struct {
	Progress func(step, detail string)
	OnStatus func(remote.StatusEvent)
}

// Endpoint is one remote workspace, reachable over a local address until it is
// released. Token is what the remote kernel's gate expects.
type Endpoint struct {
	Host      string
	Workspace string
	Addr      string
	Token     string

	once    sync.Once
	release func()
}

// Release drops this holder's claim on the workspace and its connection. Safe
// to call more than once — a pane closing twice must not free a live link.
func (e *Endpoint) Release() {
	if e == nil {
		return
	}
	e.once.Do(func() {
		if e.release != nil {
			e.release()
		}
	})
}

// Pool keeps one supervised connection per host and hands out endpoints riding
// it. Its context outlives any single attach: a supervisor bound to the caller
// that happened to dial first would die when that pane closed.
type Pool struct {
	ctx  context.Context
	opts Options

	mu    sync.Mutex
	links map[string]*link
	// follow serialises re-pointing kernels at a moved broker.
	follow sync.Mutex
}

func NewPool(ctx context.Context, opts Options) *Pool {
	return &Pool{ctx: ctx, opts: opts, links: map[string]*link{}}
}

// gate is a value several callers wait on while the first one produces it.
type gate struct {
	ready chan struct{}
	err   error
}

func (g *gate) wait(ctx context.Context) error {
	select {
	case <-g.ready:
		return g.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type link struct {
	gate
	host    string
	client  *remote.Client
	install string // the host entry's own strategy, read once at dial
	// provider is where this host's model credentials come from, read at dial
	// beside install. A host set to resolve its own gets no broker forward.
	provider string
	refs     int
	spaces   map[string]*space
	state    HostState
	// One -R per host, not per workspace: the broker is one process on this
	// side, and every kernel over there reaches it through the same forward.
	brokerOnce sync.Once
	brokerAddr string
	brokerErr  error
}

// brokerForwardName is the reserved name of a link's broker forward. Fixed, so
// a second workspace finds the one the first published rather than opening a
// second listener onto the same broker.
const brokerForwardName = "provider-broker"

// broker publishes this machine's provider broker on the host's loopback and
// reports the address a kernel there should call. Empty when this pool has no
// broker to publish, which is what leaves that host on its own credentials.
func (p *Pool) broker(l *link) (string, error) {
	if !p.brokers(l) {
		return "", nil
	}
	l.brokerOnce.Do(func() {
		l.brokerAddr, l.brokerErr = l.client.Forwards().Add(forward.Spec{
			Name:       brokerForwardName,
			Direction:  forward.Remote,
			BindAddr:   "127.0.0.1:0",
			TargetAddr: p.opts.Broker.Addr,
		})
		if l.brokerErr != nil {
			l.brokerErr = fmt.Errorf("publish the provider broker on %s: %w", l.host, l.brokerErr)
		}
	})
	if l.brokerErr != nil {
		return "", l.brokerErr
	}
	// Where the forward is now, not where it first landed: a reconnect asks for
	// the old port back and is given another when that one was taken.
	if bound, ok := boundBroker(l.client); ok {
		return bound, nil
	}
	return l.brokerAddr, nil
}

// boundBroker is the remote address the broker forward is listening on.
func boundBroker(client *remote.Client) (string, bool) {
	for _, e := range client.Forwards().List() {
		if e.Spec.Name == brokerForwardName && e.Up && e.BoundAddr != "" {
			return e.BoundAddr, true
		}
	}
	return "", false
}

// followBroker points every kernel on l at the broker's forward after the link
// comes back. The kernels outlive the link; the port they were told may not.
func (p *Pool) followBroker(l *link, client *remote.Client) {
	if !p.brokers(l) {
		return
	}
	// One at a time, each reading the port as it stands when it runs: two
	// reconnects finishing out of order must not leave the older port written.
	p.follow.Lock()
	defer p.follow.Unlock()
	addr, ok := boundBroker(client)
	if !ok {
		return
	}
	p.mu.Lock()
	workspaces := make([]string, 0, len(l.spaces))
	for _, s := range l.spaces {
		workspaces = append(workspaces, s.workspace)
	}
	p.mu.Unlock()
	broker := bootstrap.Broker{Addr: addr, Token: p.opts.Broker.Token}
	for _, ws := range workspaces {
		if err := bootstrap.RepointBroker(p.ctx, client, ws, broker); err != nil {
			p.record(l, func(st *HostState) { st.Err = err.Error() })
		}
	}
}

// brokers reports whether a connect to l publishes this machine's broker.
func (p *Pool) brokers(l *link) bool {
	return p.opts.Broker.configured() && l.provider != config.RemoteProviderRemote
}

// HostState is what a frontend shows for one machine: where its link stands,
// and — while a first connect is still running — which step it reached. The
// pool records it whether or not a caller asked to watch, because the window
// that displays it is not always the one that opened the connection.
type HostState struct {
	Status  remote.Status
	Attempt int
	Step    string
	Detail  string
	Err     string
	Panes   int
}

// States reports every host this pool holds a link for.
func (p *Pool) States() map[string]HostState {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]HostState, len(p.links))
	for host, l := range p.links {
		state := l.state
		for _, s := range l.spaces {
			state.Panes += s.refs
		}
		out[host] = state
	}
	return out
}

// record folds one link's state change in under the pool's lock.
func (p *Pool) record(l *link, apply func(*HostState)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	apply(&l.state)
}

type space struct {
	gate
	// key is what spaces is keyed by — what the caller asked for. The remote
	// kernel resolves ~ and answers with an absolute path, so the two part ways
	// and deleting by the resolved one would leave the entry behind.
	key       string
	workspace string
	addr      string
	token     string
	forward   string
	refs      int
}

// Attach makes host's workspace reachable locally. ctx bounds this attach, but
// not the connection it may create: an established link belongs to the pool.
// A dial already in flight is not interruptible — DialTimeout bounds that one.
func (p *Pool) Attach(ctx context.Context, host, workspace string, call Call) (*Endpoint, error) {
	host, workspace = strings.TrimSpace(host), strings.TrimSpace(workspace)
	if host == "" {
		return nil, errors.New("attach: no host named")
	}
	l, dialer := p.holdLink(host)
	var unsubscribe func()
	if dialer {
		unsubscribe = p.dial(l, call)
	}
	if err := l.wait(ctx); err != nil {
		p.dropLink(l)
		return nil, err
	}
	if !dialer && call.OnStatus != nil {
		// Only a late caller subscribes here. The one that dialed did so before
		// Start, or it would have missed the connect it was waiting on.
		unsubscribe = l.client.Subscribe(call.OnStatus)
	}
	s, server := p.holdSpace(l, workspace)
	if server {
		p.serve(ctx, l, s, workspace, call)
	}
	if err := s.wait(ctx); err != nil {
		p.dropSpace(l, s, unsubscribe)
		return nil, err
	}
	return &Endpoint{
		Host:      host,
		Workspace: s.workspace,
		Addr:      s.addr,
		Token:     s.token,
		release:   func() { p.dropSpace(l, s, unsubscribe) },
	}, nil
}

// Close tears down every connection this pool holds.
func (p *Pool) Close() {
	p.mu.Lock()
	links := make([]*link, 0, len(p.links))
	for _, l := range p.links {
		links = append(links, l)
	}
	p.links = map[string]*link{}
	p.mu.Unlock()
	for _, l := range links {
		if l.client != nil {
			_ = l.client.Close()
		}
	}
}

// holdLink takes a reference on host's connection, creating the entry if this
// is the first. The second return says whether this caller must produce it.
func (p *Pool) holdLink(host string) (*link, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if l := p.links[host]; l != nil {
		l.refs++
		return l, false
	}
	l := &link{
		gate:   gate{ready: make(chan struct{})},
		host:   host,
		refs:   1,
		spaces: map[string]*space{},
	}
	p.links[host] = l
	return l, true
}

// dial produces the connection every waiter on this link is blocked for, and
// returns the caller's own status subscription.
func (p *Pool) dial(l *link, call Call) (unsubscribe func()) {
	fail := func(err error) {
		l.err = err
		// Dropped from the table before the waiters wake: a cached failure would
		// answer every later attach with an error nobody can retry past.
		p.forgetLink(l)
	}
	defer close(l.ready)
	cfg, err := config.Load()
	if err != nil {
		fail(err)
		return nil
	}
	if entry, ok := cfg.RemoteHost(l.host); ok {
		l.install = entry.ServeInstallMode()
		l.provider = entry.ProviderMode()
	}
	build := p.opts.Dial
	if build == nil {
		build = func(host string, prompts Prompts) (*remote.Client, error) { return Dial(cfg, host, prompts) }
	}
	client, err := build(l.host, p.opts.Prompts)
	if err != nil {
		fail(err)
		return nil
	}
	// The pool's own watcher, separate from the caller's: a window that opens a
	// second pane an hour later reads this, having subscribed to nothing.
	client.Subscribe(func(ev remote.StatusEvent) {
		p.record(l, func(st *HostState) {
			st.Status, st.Attempt, st.Err = ev.Status, ev.Attempt, ""
			if ev.Err != nil {
				st.Err = ev.Err.Error()
			}
		})
		if ev.Status == remote.StatusConnected && ev.Attempt > 0 {
			go p.followBroker(l, client)
		}
	})
	if call.OnStatus != nil {
		unsubscribe = client.Subscribe(call.OnStatus)
	}
	if err := client.Start(p.ctx); err != nil {
		_ = client.Close()
		fail(err)
		return unsubscribe
	}
	l.client = client
	return unsubscribe
}

func (p *Pool) forgetLink(l *link) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.links[l.host] == l {
		delete(p.links, l.host)
	}
}

func (p *Pool) dropLink(l *link) {
	p.mu.Lock()
	l.refs--
	last := l.refs <= 0
	if last && p.links[l.host] == l {
		delete(p.links, l.host)
	}
	client := l.client
	p.mu.Unlock()
	if last && client != nil {
		_ = client.Close()
	}
}

func (p *Pool) holdSpace(l *link, workspace string) (*space, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s := l.spaces[workspace]; s != nil {
		s.refs++
		return s, false
	}
	s := &space{
		gate:      gate{ready: make(chan struct{})},
		key:       workspace,
		workspace: workspace,
		refs:      1,
	}
	l.spaces[workspace] = s
	return s, true
}

// serve brings up the remote kernel for one workspace and binds the forward
// that reaches it.
func (p *Pool) serve(ctx context.Context, l *link, s *space, workspace string, call Call) {
	defer close(s.ready)
	install := p.opts.Install
	if install == "" {
		install = l.install
	}
	// Before the launch, not after: the address goes on the serve command line,
	// and a kernel already running cannot be told a new one.
	brokerAddr, err := p.broker(l)
	if err != nil {
		s.err = err
		p.forgetSpace(l, s)
		return
	}
	res, err := bootstrap.EnsureServe(ctx, l.client, bootstrap.Options{
		Workspace:       workspace,
		Broker:          bootstrap.Broker{Addr: brokerAddr, Token: p.opts.Broker.Token},
		Install:         install,
		LocalBinary:     p.opts.LocalBinary,
		LocalGOOS:       runtime.GOOS,
		LocalGOARCH:     runtime.GOARCH,
		ProductVersion:  p.opts.Version,
		FetchBinary:     p.opts.FetchBinary,
		ResolveDownload: p.opts.ResolveDownload,
		MinVersion:      bootstrap.PaneFloor(brokerAddr != ""),
		Progress: func(step, detail string) {
			p.record(l, func(st *HostState) { st.Step, st.Detail = step, detail })
			if call.Progress != nil {
				call.Progress(step, detail)
			}
		},
	})
	if err != nil {
		s.err = err
		p.forgetSpace(l, s)
		return
	}
	// Named per workspace: a second one on this host must not replace the
	// first one's forward, which one shared reserved name would do.
	name := "serve:" + res.State.Workspace
	bound, err := l.client.Forwards().Add(forward.Spec{
		Name:       name,
		Direction:  forward.Local,
		BindAddr:   "127.0.0.1:0",
		TargetAddr: res.State.Addr,
	})
	if err != nil {
		s.err = fmt.Errorf("forward remote serve: %w", err)
		p.forgetSpace(l, s)
		return
	}
	p.record(l, func(st *HostState) { st.Step, st.Detail = "", "" })
	// The kernel's own spelling, not the file layer's: this is what goes back
	// to that kernel when a pane opens a workspace on it.
	s.workspace, s.addr, s.token, s.forward = res.Workspace, bound, res.Token, name
}

func (p *Pool) forgetSpace(l *link, s *space) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if l.spaces[s.key] == s {
		delete(l.spaces, s.key)
	}
}

// dropSpace releases one holder's claim. Every attach took a reference on both
// the workspace and the link, so every release gives back both.
func (p *Pool) dropSpace(l *link, s *space, unsubscribe func()) {
	if unsubscribe != nil {
		unsubscribe()
	}
	p.mu.Lock()
	s.refs--
	last := s.refs <= 0
	if last && l.spaces[s.key] == s {
		delete(l.spaces, s.key)
	}
	client := l.client
	p.mu.Unlock()
	if last && s.forward != "" && client != nil {
		_ = client.Forwards().Remove(s.forward)
	}
	p.dropLink(l)
}
