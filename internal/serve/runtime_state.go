package serve

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
)

type runtimeSessionView struct {
	SessionPath string                     `json:"sessionPath"`
	Current     bool                       `json:"current"`
	State       event.RuntimeStateSnapshot `json:"state"`
}

func runtimeStateAndStatus(ctrl control.SessionAPI) (event.RuntimeStateSnapshot, control.RuntimeStatus) {
	state, status := runtimeStateOf(ctrl), ctrl.RuntimeStatus()
	if state.SchemaVersion == 1 {
		status.Running, status.PendingPrompt, status.BackgroundJobs, status.CancelRequested, status.Cancellable = state.Running, state.PendingPrompt, state.BackgroundJobs, state.CancelRequested, state.Cancellable
	}
	return state, status
}

type runtimeStatesView struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Epoch         string               `json:"epoch"`
	Revision      uint64               `json:"revision"`
	Sessions      []runtimeSessionView `json:"sessions"`
}
type serveRuntimeProjection struct {
	mu       sync.Mutex
	snapshot runtimeStatesView
}

func runtimeStateOf(ctrl control.SessionAPI) event.RuntimeStateSnapshot {
	if reader, ok := ctrl.(control.RuntimeStateReader); ok {
		return reader.RuntimeStateSnapshot()
	}
	status := ctrl.RuntimeStatus()
	phase := "idle"
	if status.Running {
		phase = "executing"
	}
	return event.RuntimeStateSnapshot{Phase: phase, Running: status.Running, PendingPrompt: status.PendingPrompt,
		BackgroundJobs: status.BackgroundJobs, CancelRequested: status.CancelRequested, Cancellable: status.Cancellable}
}

// runtimeStates is a memory-only reconciliation surface, including detached
// controllers. It does not list transcripts, generate titles, or fetch balance.
func (s *Server) runtimeStates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.runtimeStatesSnapshot())
}

func (s *Server) runtimeStatesSnapshot() runtimeStatesView {
	r := &s.runtimeProjection
	r.mu.Lock()
	defer r.mu.Unlock()
	s.bindMu.Lock()
	current := s.ctl()
	controllers := []control.SessionAPI{current}
	s.detachedMu.Lock()
	for _, detached := range s.detached {
		if detached.ctrl != current {
			controllers = append(controllers, detached.ctrl)
		}
	}
	s.detachedMu.Unlock()
	result := runtimeStatesView{SchemaVersion: 1, Epoch: r.snapshot.Epoch, Revision: r.snapshot.Revision, Sessions: []runtimeSessionView{}}
	for _, ctrl := range controllers {
		if ctrl == nil {
			continue
		}
		result.Sessions = append(result.Sessions, runtimeSessionView{SessionPath: agent.CanonicalSessionPath(ctrl.SessionPath()), Current: ctrl == current, State: runtimeStateOf(ctrl)})
	}
	s.bindMu.Unlock()
	sort.Slice(result.Sessions, func(i, j int) bool {
		return result.Sessions[i].State.RuntimeEpoch < result.Sessions[j].State.RuntimeEpoch
	})
	if result.Epoch == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			panic(err)
		}
		result.Epoch = hex.EncodeToString(id[:])
	}
	if !reflect.DeepEqual(result, r.snapshot) {
		result.Revision++
		r.snapshot = result
	}
	result.Sessions = append([]runtimeSessionView{}, r.snapshot.Sessions...)
	return result
}

func (b *Broadcaster) RuntimeStateChanged(snapshot event.RuntimeStateSnapshot) {
	b.publishRuntimeState(b.CurrentSession(), snapshot)
}
func (b *Broadcaster) publishRuntimeState(path string, snapshot event.RuntimeStateSnapshot) {
	path = agent.CanonicalSessionPath(path)
	b.mu.Lock()
	defer b.mu.Unlock()
	frame, err := json.Marshal(eventwire.Event{Kind: "runtime_state", SessionPath: path, SessionCurrent: path == b.current, RuntimeState: &snapshot})
	if err != nil {
		return
	}
	for ch, sub := range b.subs {
		if !sub.all && path != "" && path != b.current {
			continue
		}
		enqueueSubscriberWireFrame(ch, frame, "runtime_state")
	}
}

func (s *sessionTagSink) RuntimeStateChanged(snapshot event.RuntimeStateSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || !s.runtimeActive {
		s.pendingRuntimeState = &snapshot
		return
	}
	s.bc.publishRuntimeState(s.path, snapshot)
}

// A per-session status query must resolve its own controller, not the foreground.
func (s *Server) ownedRuntimeStatusView(path string) (map[string]any, bool) {
	path = agent.CanonicalSessionPath(path)
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	ctrl := s.ctl()
	if ctrl == nil || agent.CanonicalSessionPath(ctrl.SessionPath()) != path {
		s.detachedMu.Lock()
		detached := s.detached[path]
		ctrl = nil
		if detached != nil {
			ctrl = detached.ctrl
		}
		s.detachedMu.Unlock()
	}
	if ctrl == nil {
		return nil, false
	}
	state := runtimeStateOf(ctrl)
	return map[string]any{"runtimeState": state, "running": state.Running, "pendingPrompt": state.PendingPrompt,
		"backgroundJobs": state.BackgroundJobs, "cancelRequested": state.CancelRequested, "cancellable": state.Cancellable,
		"sessionPath": path, "takenOver": false, "label": ctrl.Label(), "plan": ctrl.PlanMode(), "toolApprovalMode": ctrl.ToolApprovalMode(), "goal": ctrl.Goal()}, true
}
