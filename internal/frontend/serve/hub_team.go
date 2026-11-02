package serve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
	"reasonix/internal/session/team"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/teamroster"
)

// TeamHostOptions turns the user's team on for a hub. Dir holds the team's
// records; Workspace is where an agent works when nobody chose a folder for it,
// one folder per agent, named by its id.
type TeamHostOptions struct {
	Dir       string
	Workspace string
}

// DefaultTeamHost is the team a host turns on: its records under the state
// root, and an agent's own folder under "Reasonix Team" in the user's home,
// outside Reasonix's directories so it can be trusted. Nil when either cannot
// be resolved.
func DefaultTeamHost() *TeamHostOptions {
	if team.CheckPlatform() != nil {
		return nil
	}
	dir := config.RootsForHome("").TeamDir()
	home, err := os.UserHomeDir()
	if dir == "" || err != nil || home == "" {
		return nil
	}
	return &TeamHostOptions{Dir: dir, Workspace: filepath.Join(home, "Reasonix Team")}
}

// Bounds on the sessions the team keeps open behind the window.
const (
	teamIdleClose = 10 * time.Minute
	teamLiveCap   = 6
	teamTickEvery = 5 * time.Second
)

// hubTeam is the hub's side of the team: the service when this process holds
// the team's writer lease, and the runtimes it keeps for agents' seats.
type hubTeam struct {
	reviews   *teamReviews
	h         *Hub
	opts      TeamHostOptions
	store     teamroster.Store
	writer    atomic.Pointer[teamWriter]
	stop      chan struct{}
	done      chan struct{}
	opening   sync.Map
	mu        sync.Mutex
	seats     map[team.SeatRef]string
	holds     map[string]*teamHold
	viewCache teamViewCache
}

// teamWriter is what exists while this process holds the team's writer lease.
// Once claimed it is kept until the hub shuts down.
type teamWriter struct {
	svc     *team.Service
	release func()
}

// teamHold is why a runtime stays open: the team holds every seat it opened,
// and the window holds one it is showing.
type teamHold struct {
	seat   team.SeatRef
	window bool
	used   time.Time
}

func newHubTeam(h *Hub, opts TeamHostOptions) *hubTeam {
	t := &hubTeam{h: h, opts: opts, store: team.WithSecretCredentials(teamroster.Store{Dir: opts.Dir}), stop: make(chan struct{}), done: make(chan struct{}),
		seats: map[team.SeatRef]string{}, holds: map[string]*teamHold{}}
	t.reviews = newTeamReviews(t)
	if err := team.RegisterSecrets(t.store); err != nil {
		slog.Warn("serve: read the team's secrets", "err", err)
	}
	if err := t.claim(); err != nil && !errors.Is(err, teamroster.ErrReadOnly) {
		slog.Warn("serve: open the team", "err", err)
	}
	go t.loop()
	return t
}

// claim takes the team's writer lease if no other process holds it. Until it
// does, this window reads the team and changes nothing.
func (t *hubTeam) claim() error {
	release, err := t.store.AcquireLease()
	if err != nil {
		return err
	}
	svc := team.New(team.Options{Store: t.store, Panes: t, OnChange: t.changed})
	t.writer.Store(&teamWriter{svc: svc, release: release})
	if err := svc.Recover(context.Background()); err != nil {
		slog.Warn("serve: recover the team", "err", err)
	}
	t.changed()
	return nil
}

// service is the team's service, nil while another process holds the team.
func (t *hubTeam) service() *team.Service {
	if w := t.writer.Load(); w != nil {
		return w.svc
	}
	return nil
}

func (t *hubTeam) close() {
	close(t.stop)
	t.reviews.close()
	<-t.done
	if w := t.writer.Load(); w != nil {
		w.release()
	}
}

// changed tells the window's listeners the team moved: a roster write, a seat
// that opened or closed, a turn that started or ended.
func (t *hubTeam) changed() {
	t.h.treeChanged()
}

func (t *hubTeam) seatLock(seat team.SeatRef) *sync.Mutex {
	l, _ := t.opening.LoadOrStore(seat, &sync.Mutex{})
	return l.(*sync.Mutex)
}

func (t *hubTeam) liveRuntime(seat team.SeatRef) (*Runtime, bool) {
	t.mu.Lock()
	id, ok := t.seats[seat]
	t.mu.Unlock()
	if !ok {
		return nil, false
	}
	rt := t.h.Get(id)
	return rt, rt != nil
}

// Live implements team.Panes: the seat's runtime, only if it is open.
func (t *hubTeam) Live(seat team.SeatRef) (team.Pane, bool) {
	rt, ok := t.liveRuntime(seat)
	if !ok {
		return nil, false
	}
	return runtimePane{rt}, true
}

// Ensure implements team.Panes: the seat's runtime, opened behind the window
// when it is not open yet. A seat opens once however many ask at a time.
func (t *hubTeam) Ensure(ctx context.Context, seat team.SeatRef) (team.Pane, error) {
	if err := team.CheckPlatform(); err != nil {
		return nil, err
	}
	lock := t.seatLock(seat)
	lock.Lock()
	defer lock.Unlock()
	if rt, ok := t.liveRuntime(seat); ok {
		t.mu.Lock()
		if hold := t.holds[rt.ID]; hold != nil {
			hold.used = time.Now()
		}
		t.mu.Unlock()
		return runtimePane{rt}, nil
	}
	rt, err := t.open(ctx, seat)
	if err != nil {
		slog.Warn("serve: open a team seat", "agent", seat.Agent, "group", seat.Group, "err", err)
		return nil, err
	}
	t.mu.Lock()
	t.seats[seat] = rt.ID
	t.holds[rt.ID] = &teamHold{seat: seat, used: time.Now()}
	t.mu.Unlock()
	t.evictOverCap()
	t.changed()
	return runtimePane{rt}, nil
}

// place is where a seat's session lives: its folder, its transcript (empty
// before its first turn) and the model a first open starts on.
func (t *hubTeam) place(r teamroster.Roster, seat team.SeatRef) (root, path, model string, a *teamroster.Agent, err error) {
	a, ok := r.Agent(seat.Agent)
	if !ok {
		return "", "", "", nil, teamroster.ErrUnknownAgent
	}
	root, path = a.Folder, a.SessionPath
	if seat.Group != "" {
		g, ok := r.Group(seat.Group)
		if !ok || !g.IsMember(a.ID) {
			return "", "", "", nil, teamroster.ErrNotMember
		}
		path = g.MemberSessions[a.ID]
		if g.Folder != "" {
			root = g.Folder
		}
	}
	if root == "" {
		root = filepath.Join(t.opts.Workspace, a.ID)
	}
	if path == "" {
		model = a.InitialModel
	}
	return root, path, model, a, nil
}

func (t *hubTeam) open(ctx context.Context, seat team.SeatRef) (*Runtime, error) {
	r, err := t.store.Load()
	if err != nil {
		return nil, err
	}
	root, path, model, a, err := t.place(r, seat)
	if err != nil {
		return nil, err
	}
	private, err := t.prepareTeamFolder(r, seat, root)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", team.ErrUnavailable, err)
	}
	// A seat closed before its first turn has a recorded path and no transcript.
	resume := path
	if _, err := os.Lstat(path); path != "" && errors.Is(err, fs.ErrNotExist) {
		resume = ""
	}
	rt, err := t.h.openBound(ctx, OpenRequest{Root: root, SessionPath: resume, Model: model, team: &teamOpen{seat: seat, mode: a.Permission}}, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", team.ErrUnavailable, err)
	}
	ctrl, ok := rt.Server.Controller().(*control.Controller)
	if !ok {
		_ = t.h.closeRuntimeNow(rt.ID)
		return nil, team.ErrUnavailable
	}
	switch ctrl.WorkspaceTrust() {
	case config.WorkspaceTrusted:
	case config.WorkspaceTrustDeclined:
		_ = t.h.closeRuntimeNow(rt.ID)
		return nil, fmt.Errorf("%w: the user declined to trust %s", team.ErrUnavailable, root)
	default:
		if !private {
			_ = t.h.closeRuntimeNow(rt.ID)
			return nil, errTeamFolderUntrusted
		}
		if err := ctrl.DecideWorkspaceTrust(config.WorkspaceTrusted); err != nil {
			_ = t.h.closeRuntimeNow(rt.ID)
			return nil, fmt.Errorf("%w: %w", team.ErrUnavailable, err)
		}
	}
	switch {
	case resume != "":
	case path != "":
		ctrl.SetFreshSessionPath(path)
	default:
		path = sessionstore.NewSessionPath(SessionDirFor(root), "agent")
		ctrl.SetFreshSessionPath(path)
		if err := t.recordSession(ctx, seat, path); err != nil {
			slog.Warn("serve: record an agent's session", "err", err)
		}
	}
	rt.Server.mu.Lock()
	rt.Server.ownPosture = func(mode string) { t.setPermission(seat.Agent, mode) }
	rt.Server.mu.Unlock()
	return rt, nil
}

// setPermission records the approval mode the user gave an agent and applies
// it to every seat of that agent that is open now.
func (t *hubTeam) setPermission(agent, mode string) {
	if _, err := t.store.Update(context.Background(), func(r *teamroster.Roster) error {
		a, ok := r.Agent(agent)
		if !ok {
			return teamroster.ErrUnknownAgent
		}
		a.Permission = mode
		return nil
	}); err != nil {
		slog.Warn("serve: record an agent's approval mode", "err", err)
	}
	t.mu.Lock()
	var ids []string
	for seat, id := range t.seats {
		if seat.Agent == agent {
			ids = append(ids, id)
		}
	}
	t.mu.Unlock()
	for _, id := range ids {
		if rt := t.h.Get(id); rt != nil && rt.Local() {
			rt.Server.Controller().SetToolApprovalMode(mode)
		}
	}
	t.changed()
}

// recordSession writes where a seat's conversation lives, so the next open
// resumes it instead of starting over.
func (t *hubTeam) recordSession(ctx context.Context, seat team.SeatRef, path string) error {
	_, err := t.store.Update(ctx, func(r *teamroster.Roster) error {
		a, ok := r.Agent(seat.Agent)
		if !ok {
			return teamroster.ErrUnknownAgent
		}
		if seat.Group == "" {
			a.SessionPath = path
			return nil
		}
		g, ok := r.Group(seat.Group)
		if !ok {
			return teamroster.ErrUnknownGroup
		}
		if g.MemberSessions == nil {
			g.MemberSessions = map[string]string{}
		}
		g.MemberSessions[seat.Agent] = path
		return nil
	})
	return err
}

// teamRuntime reports whether rt is one of the team's seats. The project tree
// leaves those out whether or not the window shows them: an agent's folder is
// not one of the user's projects, nor its conversations theirs to list there.
func (h *Hub) teamRuntime(rt *Runtime) bool {
	if h.team == nil {
		return false
	}
	_, ok := h.team.seatOf(rt.ID)
	return ok
}

// sessionPaths is every conversation the team's agents hold, canonically
// spelled, so the project tree can leave them out.
func (t *hubTeam) sessionPaths() map[string]bool {
	out := map[string]bool{}
	if t == nil {
		return out
	}
	r, err := t.store.Load()
	if err != nil {
		return out
	}
	add := func(p string) {
		if p != "" {
			out[sessionstore.CanonicalSessionPath(p)] = true
		}
	}
	for _, a := range r.Agents {
		add(a.SessionPath)
		for _, p := range a.Parts {
			add(p.SessionPath)
		}
	}
	for _, g := range r.Groups {
		for _, p := range g.MemberSessions {
			add(p)
		}
	}
	return out
}

// runtimeSeat names the team seat a pane holds, so the window draws it as that
// agent rather than as a folder.
type runtimeSeat struct {
	Agent string `json:"agent"`
	Group string `json:"group,omitempty"`
}

// viewOf is rt as the window reads it, with the seat it holds for the team.
func (h *Hub) viewOf(rt *Runtime) RuntimeView {
	v := rt.view()
	if h.team != nil {
		if seat, ok := h.team.seatOf(rt.ID); ok {
			v.Team = &runtimeSeat{Agent: seat.Agent, Group: seat.Group}
		}
	}
	return v
}

// seatOf is the seat a runtime was opened for.
func (t *hubTeam) seatOf(id string) (team.SeatRef, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	hold := t.holds[id]
	if hold == nil {
		return team.SeatRef{}, false
	}
	return hold.seat, true
}

// shown reports whether the window draws a runtime as a pane: any runtime the
// team does not hold, and a team seat the window has opened.
func (t *hubTeam) shown(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	hold := t.holds[id]
	return hold == nil || hold.window
}

// holdForWindow marks a seat's runtime as one the window shows.
func (t *hubTeam) holdForWindow(id string) {
	t.mu.Lock()
	if hold := t.holds[id]; hold != nil {
		hold.window, hold.used = true, time.Now()
	}
	t.mu.Unlock()
	t.changed()
}

// releaseWindow is the window closing a seat's pane: the runtime goes back
// behind the window rather than closing, while the team still holds it.
func (t *hubTeam) releaseWindow(id string) bool {
	t.mu.Lock()
	hold := t.holds[id]
	if hold != nil {
		hold.window, hold.used = false, time.Now()
	}
	t.mu.Unlock()
	return hold != nil
}

// forget drops a closed runtime's hold and seat.
func (t *hubTeam) forget(id string) {
	t.mu.Lock()
	if hold := t.holds[id]; hold != nil {
		delete(t.seats, hold.seat)
		delete(t.holds, id)
	}
	t.mu.Unlock()
}

func (t *hubTeam) loop() {
	defer close(t.done)
	tick := time.NewTicker(teamTickEvery)
	defer tick.Stop()
	for {
		select {
		case <-t.stop:
			return
		case now := <-tick.C:
			t.tick(now)
		}
	}
}

// tick is one beat of the team's clock. A window without the lease asks for it
// again, since the process holding it may have been the one before a restart.
func (t *hubTeam) tick(now time.Time) {
	svc := t.service()
	if svc == nil {
		if err := t.claim(); err != nil && !errors.Is(err, teamroster.ErrReadOnly) {
			slog.Debug("serve: claim the team", "err", err)
		}
		return
	}
	if err := svc.Tick(context.Background()); err != nil {
		slog.Warn("serve: team round timers", "err", err)
	}
	t.closeIdle(now)
}

// idle reports a runtime with nothing running, queued or waiting on a person.
func idle(rt *Runtime) bool {
	ctrl, ok := rt.Server.Controller().(*control.Controller)
	if !ok {
		return false
	}
	return !ctrl.Running() && !ctrl.PendingPrompt() && len(ctrl.InboxSnapshot().Items) == 0
}

// closeIdle closes the team's background runtimes idle past teamIdleClose.
func (t *hubTeam) closeIdle(now time.Time) {
	for _, id := range t.background(func(h *teamHold) bool { return now.Sub(h.used) >= teamIdleClose }) {
		if rt := t.h.Get(id); rt != nil && idle(rt) {
			if err := t.h.closeRuntimeNow(id); err != nil {
				slog.Warn("serve: close an idle agent session", "err", err)
			}
		}
	}
}

// evictOverCap closes the least recently used idle background runtimes while
// more than teamLiveCap are open.
func (t *hubTeam) evictOverCap() {
	ids := t.background(func(*teamHold) bool { return true })
	for _, id := range ids {
		if len(t.background(func(*teamHold) bool { return true })) <= teamLiveCap {
			return
		}
		if rt := t.h.Get(id); rt != nil && idle(rt) {
			_ = t.h.closeRuntimeNow(id)
		}
	}
}

// background lists the runtimes held only by the team, least recently used
// first, that match keep.
func (t *hubTeam) background(keep func(*teamHold) bool) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var ids []string
	for id, hold := range t.holds {
		if !hold.window && keep(hold) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return t.holds[ids[i]].used.Before(t.holds[ids[j]].used) })
	return ids
}

// observe wraps a seat's event sink: a turn's end schedules its memory review,
// and in a group seat the round timers see its activity — any event, and when
// a tool starts and ends.
func (t *hubTeam) observe(seat team.SeatRef, inner event.Sink) event.Sink {
	return event.FuncSink(func(e event.Event) {
		inner.Emit(e)
		switch e.Kind {
		case event.TurnStarted:
			t.reviews.cancel(seat)
		case event.TurnDone:
			t.reviews.schedule(seat)
		}
		svc := t.service()
		if svc == nil || seat.Group == "" {
			return
		}
		o := team.Observation{Kind: team.ObserveEvent, At: time.Now()}
		switch e.Kind {
		case event.ToolDispatch:
			o.Kind = team.ObserveToolStarted
		case event.ToolResult:
			o.Kind = team.ObserveToolFinished
		}
		svc.Observed(seat, o)
	})
}

// seatFor is the team seat a runtime opened for req speaks through.
func (t *hubTeam) seatFor(req OpenRequest) control.TeamSeat {
	if t == nil || req.team == nil {
		return nil
	}
	svc := t.service()
	if svc == nil {
		return nil
	}
	return team.Seat{Service: svc, Ref: req.team.seat}
}

// runtimePane is a seat backed by a hub runtime. It reads the controller each
// time: a model switch replaces it.
type runtimePane struct{ rt *Runtime }

func (p runtimePane) Deliver(d team.Delivery) error {
	ctrl, ok := p.rt.Server.Controller().(*control.Controller)
	if !ok {
		return team.ErrUnavailable
	}
	return ctrl.DeliverTeam(d)
}

func (p runtimePane) Cancel() { p.rt.Server.Controller().Cancel() }
