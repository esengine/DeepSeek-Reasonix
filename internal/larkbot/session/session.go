package session

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

type PermissionMode string

const (
	PermissionReadOnly    PermissionMode = "read-only"
	PermissionInteractive PermissionMode = "interactive"
	PermissionBypass      PermissionMode = "bypass"
)

type sessionHandle struct {
	ctrl     *control.Controller
	sink     *SinkAdapter
	chatID   string
	chatType string
	lastUsed time.Time
}

type Options struct {
	GroupPermission PermissionMode
	DMPermission    PermissionMode
	SessionTTL      time.Duration
	MaxSessions     int
}

type Router struct {
	mu       sync.RWMutex
	sessions map[string]*sessionHandle
	opts     Options
}

func NewRouter(opts Options) *Router {
	if opts.SessionTTL <= 0 {
		opts.SessionTTL = 1 * time.Hour
	}
	return &Router{
		sessions: map[string]*sessionHandle{},
		opts:     opts,
	}
}

func (r *Router) GetOrCreate(ctx context.Context, chatID, chatType string) (*control.Controller, *SinkAdapter, error) {
	r.mu.RLock()
	sh, ok := r.sessions[chatID]
	r.mu.RUnlock()

	if ok {
		sh.lastUsed = time.Now()
		return sh.ctrl, sh.sink, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	sh, ok = r.sessions[chatID]
	if ok {
		sh.lastUsed = time.Now()
		return sh.ctrl, sh.sink, nil
	}

	r.evictIfNeededLocked()

	sink := &SinkAdapter{}
	bo := boot.Options{
		RequireKey: false,
		Sink:       sink,
		Stderr:     io.Discard,
	}

	ctrl, err := boot.Build(ctx, bo)
	if err != nil {
		return nil, nil, fmt.Errorf("build controller: %w", err)
	}

	perm := r.resolvePermission(chatType)
	r.applyPermission(ctrl, perm)

	ctrl.EnableInteractiveApproval()

	sh = &sessionHandle{
		ctrl:     ctrl,
		sink:     sink,
		chatID:   chatID,
		chatType: chatType,
		lastUsed: time.Now(),
	}
	r.sessions[chatID] = sh

	slog.Info("lark session created", "chat_id", chatID, "chat_type", chatType, "permission", perm)
	return sh.ctrl, sh.sink, nil
}

func (r *Router) resolvePermission(chatType string) PermissionMode {
	switch chatType {
	case "group":
		return r.opts.GroupPermission
	case "p2p":
		return r.opts.DMPermission
	default:
		return r.opts.DMPermission
	}
}

func (r *Router) applyPermission(ctrl *control.Controller, perm PermissionMode) {
	switch perm {
	case PermissionReadOnly:
		ctrl.SetPlanMode(true)
	case PermissionInteractive:
		ctrl.SetPlanMode(false)
	case PermissionBypass:
		ctrl.SetPlanMode(false)
		ctrl.SetBypass(true)
	}
}

func (r *Router) Get(chatID string) (*control.Controller, *SinkAdapter) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sh, ok := r.sessions[chatID]
	if !ok {
		return nil, nil
	}
	sh.lastUsed = time.Now()
	return sh.ctrl, sh.sink
}

func (r *Router) RemoveAndClose(chatID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sh, ok := r.sessions[chatID]
	if !ok {
		return
	}
	if sh.ctrl != nil {
		sh.ctrl.Close()
	}
	delete(r.sessions, chatID)
	slog.Info("lark session removed", "chat_id", chatID)
}

func (r *Router) SweepExpired(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-r.opts.SessionTTL)
	var expired []string
	for id, sh := range r.sessions {
		if sh.lastUsed.Before(cutoff) {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		if sh := r.sessions[id]; sh != nil && sh.ctrl != nil {
			sh.ctrl.Close()
		}
		delete(r.sessions, id)
		slog.Info("lark session expired", "chat_id", id)
	}
}

func (r *Router) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}

func (r *Router) evictIfNeededLocked() {
	if r.opts.MaxSessions <= 0 {
		return
	}
	if len(r.sessions) < r.opts.MaxSessions {
		return
	}

	var oldestID string
	var oldestTime time.Time
	for id, sh := range r.sessions {
		if oldestID == "" || sh.lastUsed.Before(oldestTime) {
			oldestID = id
			oldestTime = sh.lastUsed
		}
	}
	if oldestID != "" {
		if sh := r.sessions[oldestID]; sh != nil && sh.ctrl != nil {
			sh.ctrl.Close()
		}
		delete(r.sessions, oldestID)
		slog.Info("lark session evicted (limit reached)", "chat_id", oldestID)
	}
}

func (r *Router) SwitchModel(ctx context.Context, chatID, modelRef string) error {
	r.mu.Lock()
	sh, ok := r.sessions[chatID]
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("no active session for this chat")
	}
	if sh.ctrl.Running() {
		return fmt.Errorf("cannot switch model while a turn is running")
	}

	chatType := sh.chatType
	prevPath := sh.ctrl.SessionPath()
	_ = sh.ctrl.Snapshot()
	carried := sh.ctrl.History()

	sink := &SinkAdapter{}
	bo := boot.Options{
		Model:      modelRef,
		RequireKey: false,
		Sink:       sink,
		Stderr:     io.Discard,
	}

	newCtrl, err := boot.Build(ctx, bo)
	if err != nil {
		return fmt.Errorf("switch model: %w", err)
	}

	perm := r.resolvePermission(chatType)
	r.applyPermission(newCtrl, perm)
	newCtrl.EnableInteractiveApproval()

	newPath := agent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	if len(carried) > 0 {
		newCtrl.Resume(&agent.Session{Messages: carried}, newPath)
	} else if newPath != "" {
		newCtrl.SetSessionPath(newPath)
	}

	r.mu.Lock()
	sh.ctrl.Close()
	sh.ctrl = newCtrl
	sh.sink = sink
	r.mu.Unlock()

	slog.Info("lark session model switched", "chat_id", chatID, "model", modelRef)
	return nil
}

func (r *Router) CloseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, sh := range r.sessions {
		if sh.ctrl != nil {
			sh.ctrl.Close()
		}
		delete(r.sessions, id)
	}
}

type SinkAdapter struct {
	mu     sync.Mutex
	queue  []event.Event
	waitCh chan struct{}
}

func (s *SinkAdapter) Emit(ev event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = append(s.queue, ev)
	if s.waitCh != nil {
		close(s.waitCh)
		s.waitCh = nil
	}
}

func (s *SinkAdapter) Drain() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.queue
	s.queue = nil
	return out
}

func (s *SinkAdapter) WaitForEvent(ctx context.Context) ([]event.Event, error) {
	s.mu.Lock()
	if len(s.queue) > 0 {
		out := s.queue
		s.queue = nil
		s.mu.Unlock()
		return out, nil
	}
	ch := make(chan struct{})
	s.waitCh = ch
	s.mu.Unlock()

	select {
	case <-ch:
		return s.Drain(), nil
	case <-ctx.Done():
		s.mu.Lock()
		s.waitCh = nil
		s.mu.Unlock()
		return nil, ctx.Err()
	}
}
