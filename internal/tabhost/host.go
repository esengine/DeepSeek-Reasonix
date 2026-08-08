package tabhost

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

// ErrTabNotFound is returned when a tab id is unknown.
var ErrTabNotFound = errors.New("tabhost: tab not found")

// ErrSessionPathInUse is returned when another tab already holds the session path.
var ErrSessionPathInUse = errors.New("tabhost: session path already open")

// Builder constructs a SessionAPI for a new tab. Tests inject fakes; production
// uses DefaultBuilder (boot.Build).
type Builder func(opts CreateTabOpts, sink event.Sink) (control.SessionAPI, error)

// Host owns many independent Controllers (one per Tab).
type Host struct {
	mu     sync.Mutex
	tabs   map[string]*Tab
	order  []string
	active string
	build  Builder
	bus    *EventBus
	max    int // 0 = unlimited
}

// Tab is one open conversation with its own controller.
type Tab struct {
	meta   TabMeta
	ctrl   control.SessionAPI
	sink   *tabSink
	leases *control.SessionLeaseKeeper
	// bindMu serializes path-changing operations on this tab only.
	bindMu sync.Mutex
}

// Option configures Host construction.
type Option func(*Host)

// WithMaxTabs caps concurrent tabs (0 = unlimited).
func WithMaxTabs(n int) Option {
	return func(h *Host) { h.max = n }
}

// New builds a Host. build must be non-nil.
func New(build Builder, opts ...Option) *Host {
	if build == nil {
		panic("tabhost: Builder is required")
	}
	h := &Host{
		tabs:  make(map[string]*Tab),
		build: build,
		bus:   NewEventBus(),
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// Bus returns the multiplexed event bus (SSE attaches here).
func (h *Host) Bus() *EventBus { return h.bus }

// CreateTab opens a new tab. The created tab becomes active.
// Controller construction runs outside the host lock so long boot.Build calls
// do not block ListTabs; session-path uniqueness is re-checked after build.
func (h *Host) CreateTab(opts CreateTabOpts) (TabMeta, error) {
	if opts.Scope == "" {
		opts.Scope = ScopeProject
	}
	if opts.Scope == ScopeProject && opts.WorkspaceRoot == "" {
		return TabMeta{}, fmt.Errorf("tabhost: workspaceRoot required for project scope")
	}
	if opts.TopicID == "" {
		opts.TopicID = "main"
	}
	if opts.TopicTitle == "" {
		opts.TopicTitle = "Session"
	}

	h.mu.Lock()
	if h.max > 0 && len(h.tabs) >= h.max {
		h.mu.Unlock()
		return TabMeta{}, fmt.Errorf("tabhost: tab limit %d reached", h.max)
	}
	if opts.SessionPath != "" {
		if id := h.tabIDForSessionPathLocked(opts.SessionPath); id != "" {
			h.mu.Unlock()
			return TabMeta{}, fmt.Errorf("%w: %s", ErrSessionPathInUse, opts.SessionPath)
		}
	}
	// Reserve a slot id before unlocking so concurrent creates don't share ids.
	id := newTabID()
	// Placeholder prevents double-use of id; filled after build.
	h.tabs[id] = &Tab{meta: TabMeta{ID: id, Scope: opts.Scope, WorkspaceRoot: opts.WorkspaceRoot}}
	h.order = append(h.order, id)
	h.mu.Unlock()

	sink := &tabSink{bus: h.bus, tabID: id}
	ctrl, buildErr := h.build(opts, sink)

	leases := control.NewSessionLeaseKeeper()
	meta := TabMeta{
		ID:                id,
		Scope:             opts.Scope,
		WorkspaceRoot:     opts.WorkspaceRoot,
		WorkspaceName:     workspaceName(opts.WorkspaceRoot),
		WorkspacePath:     opts.WorkspaceRoot,
		TopicID:           opts.TopicID,
		TopicTitle:        opts.TopicTitle,
		SessionPath:       opts.SessionPath,
		ReadOnly:          opts.ReadOnly,
		Label:             opts.Label,
		Ready:             buildErr == nil && ctrl != nil,
		Running:           false,
		Cancellable:       false,
		Mode:              "agent",
		CollaborationMode: "default",
		ToolApprovalMode:  "ask",
		TokenMode:         "auto",
		Active:            true,
		Cwd:               opts.WorkspaceRoot,
		Runtime:           map[string]any{"phase": "ready"},
	}
	if buildErr != nil {
		meta.Ready = false
		meta.StartupErr = buildErr.Error()
		meta.Runtime = map[string]any{"phase": "failed"}
	}

	if ctrl != nil {
		// Prefer EnsureSessionPath so every tab has a durable transcript path.
		if c, ok := ctrl.(*control.Controller); ok {
			c.EnableInteractiveApproval()
			if c.SessionPath() == "" {
				c.EnsureSessionPath()
			}
		}
		if p := ctrl.SessionPath(); p != "" {
			meta.SessionPath = p
			if err := leases.Rebind(p); err != nil {
				ctrl.Close()
				h.mu.Lock()
				h.removeTabLocked(id)
				h.mu.Unlock()
				return TabMeta{}, fmt.Errorf("tabhost: session lease: %w", err)
			}
		}
		if opts.Label == "" {
			meta.Label = ctrl.Label()
		}
	}

	// Re-check path uniqueness after build (EnsureSessionPath minted a path).
	h.mu.Lock()
	defer h.mu.Unlock()
	if meta.SessionPath != "" {
		if other := h.tabIDForSessionPathLocked(meta.SessionPath); other != "" && other != id {
			if ctrl != nil {
				leases.Release()
				ctrl.Close()
			}
			h.removeTabLocked(id)
			return TabMeta{}, fmt.Errorf("%w: %s", ErrSessionPathInUse, meta.SessionPath)
		}
	}
	// Mark previous active false
	for _, t := range h.tabs {
		if t != nil {
			t.meta.Active = false
		}
	}
	tab := &Tab{meta: meta, ctrl: ctrl, sink: sink, leases: leases}
	h.tabs[id] = tab
	h.active = id
	return meta, nil
}

// ListTabs returns tab metadata in open order.
func (h *Host) ListTabs() []TabMeta {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]TabMeta, 0, len(h.order))
	for _, id := range h.order {
		t := h.tabs[id]
		if t == nil {
			continue
		}
		m := t.meta
		m.Active = id == h.active
		if t.ctrl != nil {
			st := t.ctrl.RuntimeStatus()
			m.Running = st.Running || st.PendingPrompt || st.BackgroundJobs > 0
			m.PendingPrompt = st.PendingPrompt
			m.BackgroundJobs = st.BackgroundJobs
			m.CancelRequested = st.CancelRequested
			m.Cancellable = st.Cancellable
			if p := t.ctrl.SessionPath(); p != "" {
				m.SessionPath = p
			}
		}
		out = append(out, m)
	}
	return out
}

// SetActiveTab focuses a tab without cancelling others.
func (h *Host) SetActiveTab(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.tabs[id]; !ok {
		return ErrTabNotFound
	}
	h.active = id
	return nil
}

// ActiveTabID returns the focused tab id (empty if none).
func (h *Host) ActiveTabID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active
}

// Get returns a tab's SessionAPI and a per-tab bind mutex for path rebinds.
func (h *Host) Get(id string) (control.SessionAPI, *sync.Mutex, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	t, ok := h.tabs[id]
	if !ok {
		return nil, nil, ErrTabNotFound
	}
	if t.ctrl == nil {
		return nil, nil, fmt.Errorf("tabhost: tab %s has no controller: %s", id, t.meta.StartupErr)
	}
	return t.ctrl, &t.bindMu, nil
}

// SubmitHTTP runs a user turn on tab id via SubmitHTTP (serve/Electron path).
// It does not wait for completion; callers observe Bus events.
func (h *Host) SubmitHTTP(id, input string) error {
	ctrl, _, err := h.Get(id)
	if err != nil {
		return err
	}
	// Prefer HTTP submit when available (blocks shell bang-path).
	type httpSubmitter interface {
		SubmitHTTP(input string)
	}
	if hs, ok := ctrl.(httpSubmitter); ok {
		hs.SubmitHTTP(input)
		return nil
	}
	type submitter interface {
		Submit(input string)
	}
	if s, ok := ctrl.(submitter); ok {
		s.Submit(input)
		return nil
	}
	return fmt.Errorf("tabhost: controller does not support Submit")
}

// RebindSessionLease moves the tab's lease to a new session path (after resume/new).
func (h *Host) RebindSessionLease(id, path string) error {
	h.mu.Lock()
	t, ok := h.tabs[id]
	if !ok {
		h.mu.Unlock()
		return ErrTabNotFound
	}
	leases := t.leases
	if path != "" {
		if other := h.tabIDForSessionPathLocked(path); other != "" && other != id {
			h.mu.Unlock()
			return fmt.Errorf("%w: %s", ErrSessionPathInUse, path)
		}
	}
	h.mu.Unlock()
	if leases == nil {
		return nil
	}
	if err := leases.Rebind(path); err != nil {
		return err
	}
	h.mu.Lock()
	if t2 := h.tabs[id]; t2 != nil {
		t2.meta.SessionPath = path
	}
	h.mu.Unlock()
	return nil
}

// CloseTab tears down a tab.
func (h *Host) CloseTab(id string) error {
	h.mu.Lock()
	t, ok := h.tabs[id]
	if !ok {
		h.mu.Unlock()
		return ErrTabNotFound
	}
	h.removeTabLocked(id)
	ctrl := t.ctrl
	leases := t.leases
	h.mu.Unlock()

	if ctrl != nil {
		_ = ctrl.Snapshot()
		ctrl.Close()
	}
	if leases != nil {
		leases.Release()
	}
	return nil
}

// CloseAll closes every tab (process shutdown).
func (h *Host) CloseAll() {
	h.mu.Lock()
	ids := append([]string(nil), h.order...)
	h.mu.Unlock()
	for _, id := range ids {
		_ = h.CloseTab(id)
	}
}

func (h *Host) removeTabLocked(id string) {
	delete(h.tabs, id)
	next := h.order[:0]
	for _, x := range h.order {
		if x != id {
			next = append(next, x)
		}
	}
	h.order = next
	if h.active == id {
		h.active = ""
		if len(h.order) > 0 {
			h.active = h.order[len(h.order)-1]
		}
	}
}

func (h *Host) tabIDForSessionPathLocked(path string) string {
	want, err := filepath.Abs(path)
	if err != nil {
		want = path
	}
	for id, t := range h.tabs {
		if t == nil {
			continue
		}
		sp := t.meta.SessionPath
		if t.ctrl != nil {
			if p := t.ctrl.SessionPath(); p != "" {
				sp = p
			}
		}
		if sp == "" {
			continue
		}
		got, err := filepath.Abs(sp)
		if err != nil {
			got = sp
		}
		if got == want {
			return id
		}
	}
	return ""
}

func newTabID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "tab_" + hex.EncodeToString(b[:])
}

func workspaceName(root string) string {
	if root == "" {
		return "global"
	}
	base := filepath.Base(root)
	if base == "." || base == string(filepath.Separator) {
		return root
	}
	return base
}
