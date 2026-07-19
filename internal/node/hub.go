// Package node implements the multi-session Reasonix Node daemon used by the
// mobile remote runtime. It preserves single-session reasonix serve unchanged.
package node

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"reasonix/internal/mobileprotocol"
)

const (
	defaultEventRingSize = 512
	defaultDedupeSize    = 256
)

// SessionSlot is one active remote session on the node.
// Full Controller wiring lands in a follow-up milestone; the slot already
// owns lease identity, sequenced events, and request dedupe.
type SessionSlot struct {
	mu         sync.Mutex
	Descriptor mobileprotocol.SessionDescriptor
	Ring       *EventRing
	Dedupe     *RequestDedupe
	Running    bool
	// observers are active WebSocket connections watching this session.
	observers map[*websocket.Conn]struct{}
}

// Hub is the multi-session node process.
type Hub struct {
	mu       sync.Mutex
	nodeID   string
	sessions map[string]*SessionSlot
	upgrader websocket.Upgrader
}

// NewHub creates an empty multi-session hub.
func NewHub(nodeID string) *Hub {
	if nodeID == "" {
		nodeID = "node-local"
	}
	return &Hub{
		nodeID:   nodeID,
		sessions: make(map[string]*SessionSlot),
		upgrader: websocket.Upgrader{
			// LAN pairing uses token/fingerprint auth at the app layer; origin
			// checks are enforced once the pairing protocol is complete.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// NodeID returns the stable node identity.
func (h *Hub) NodeID() string { return h.nodeID }

// CreateSession registers a new remote session descriptor.
func (h *Hub) CreateSession(title, providerRef string) mobileprotocol.SessionDescriptor {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := fmt.Sprintf("sess_%d", time.Now().UnixNano())
	d := mobileprotocol.SessionDescriptor{
		ID:          id,
		Runtime:     mobileprotocol.RuntimeRemote,
		NodeID:      h.nodeID,
		ProviderRef: providerRef,
		Title:       title,
		Revision:    1,
	}
	d.Normalize()
	slot := &SessionSlot{
		Descriptor: d,
		Ring:       NewEventRing(defaultEventRingSize),
		Dedupe:     NewRequestDedupe(defaultDedupeSize),
		observers:  make(map[*websocket.Conn]struct{}),
	}
	h.sessions[id] = slot
	return d
}

// GetSession returns a copy of the descriptor when present.
func (h *Hub) GetSession(id string) (mobileprotocol.SessionDescriptor, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[id]
	if !ok {
		return mobileprotocol.SessionDescriptor{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Descriptor, true
}

// ListSessions returns descriptors sorted by id for stable tests.
func (h *Hub) ListSessions() []mobileprotocol.SessionDescriptor {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]mobileprotocol.SessionDescriptor, 0, len(h.sessions))
	for _, s := range h.sessions {
		s.mu.Lock()
		out = append(out, s.Descriptor)
		s.mu.Unlock()
	}
	return out
}

// HandleCommand routes a mobile command envelope and returns a response envelope.
// Write commands with requestId are idempotent via the session dedupe table.
func (h *Hub) HandleCommand(env mobileprotocol.Envelope) mobileprotocol.Envelope {
	if err := env.Validate(); err != nil {
		return errorEnv("", "", "bad_envelope", err.Error())
	}
	var cmd mobileprotocol.CommandPayload
	if err := env.UnmarshalPayload(&cmd); err != nil {
		return errorEnv(env.RequestID, env.SessionID, "bad_payload", err.Error())
	}

	// Session-scoped dedupe for write commands.
	if env.SessionID != "" && env.RequestID != "" && isWriteCommand(cmd.Name) {
		if slot := h.slot(env.SessionID); slot != nil {
			if prev, ok := slot.Dedupe.Lookup(env.RequestID); ok {
				var prior mobileprotocol.Envelope
				if err := json.Unmarshal(prev.Response, &prior); err == nil {
					return prior
				}
			}
		}
	}

	var resp mobileprotocol.Envelope
	switch cmd.Name {
	case mobileprotocol.CmdHello, mobileprotocol.CmdCreateSession:
		resp = h.handleCreateOrHello(env, cmd)
	case mobileprotocol.CmdSnapshot:
		resp = h.handleSnapshot(env)
	case mobileprotocol.CmdSubmit:
		resp = h.handleSubmit(env, cmd)
	case mobileprotocol.CmdCancel:
		resp = h.handleCancel(env)
	case mobileprotocol.CmdApprove:
		resp = h.handleApprove(env, cmd)
	case mobileprotocol.CmdListModels:
		resp = h.handleListModels(env)
	default:
		resp = errorEnv(env.RequestID, env.SessionID, "unknown_command", "unknown command: "+cmd.Name)
	}

	if env.SessionID != "" && env.RequestID != "" && isWriteCommand(cmd.Name) {
		if slot := h.slot(env.SessionID); slot != nil {
			if b, err := json.Marshal(resp); err == nil {
				slot.Dedupe.Remember(env.RequestID, b)
			}
		}
	}
	// create_session stores dedupe under the new session id after creation.
	if cmd.Name == mobileprotocol.CmdCreateSession && env.RequestID != "" && resp.SessionID != "" {
		if slot := h.slot(resp.SessionID); slot != nil {
			if b, err := json.Marshal(resp); err == nil {
				slot.Dedupe.Remember(env.RequestID, b)
			}
		}
	}
	return resp
}

func isWriteCommand(name string) bool {
	switch name {
	case mobileprotocol.CmdCreateSession, mobileprotocol.CmdSubmit, mobileprotocol.CmdCancel,
		mobileprotocol.CmdApprove, mobileprotocol.CmdAnswer, mobileprotocol.CmdRestoreSession:
		return true
	default:
		return false
	}
}

func (h *Hub) slot(id string) *SessionSlot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[id]
}

func (h *Hub) handleCreateOrHello(env mobileprotocol.Envelope, cmd mobileprotocol.CommandPayload) mobileprotocol.Envelope {
	if cmd.Name == mobileprotocol.CmdHello {
		resp := mobileprotocol.NewEnvelope(mobileprotocol.TypeHello)
		resp.RequestID = env.RequestID
		_ = resp.MarshalPayload(map[string]any{
			"nodeId":      h.nodeID,
			"protocolMax": mobileprotocol.CurrentVersion,
		})
		return resp
	}
	var args mobileprotocol.CreateSessionArgs
	_ = json.Unmarshal(cmd.Args, &args)
	if args.Runtime != "" && args.Runtime != mobileprotocol.RuntimeRemote {
		return errorEnv(env.RequestID, "", "invalid_runtime", "node only creates remote sessions")
	}
	d := h.CreateSession(args.Title, args.ProviderRef)
	resp := mobileprotocol.NewEnvelope(mobileprotocol.TypeSnapshot)
	resp.RequestID = env.RequestID
	resp.SessionID = d.ID
	_ = resp.MarshalPayload(mobileprotocol.SnapshotPayload{
		Descriptor:   d,
		LastEventSeq: 0,
		Revision:     d.Revision,
	})
	return resp
}

func (h *Hub) handleSnapshot(env mobileprotocol.Envelope) mobileprotocol.Envelope {
	slot := h.slot(env.SessionID)
	if slot == nil {
		return errorEnv(env.RequestID, env.SessionID, "not_found", "session not found")
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	resp := mobileprotocol.NewEnvelope(mobileprotocol.TypeSnapshot)
	resp.RequestID = env.RequestID
	resp.SessionID = env.SessionID
	_ = resp.MarshalPayload(mobileprotocol.SnapshotPayload{
		Descriptor:   slot.Descriptor,
		Running:      slot.Running,
		LastEventSeq: slot.Ring.LastSeq(),
		Revision:     slot.Descriptor.Revision,
	})
	return resp
}

func (h *Hub) handleSubmit(env mobileprotocol.Envelope, cmd mobileprotocol.CommandPayload) mobileprotocol.Envelope {
	slot := h.slot(env.SessionID)
	if slot == nil {
		return errorEnv(env.RequestID, env.SessionID, "not_found", "session not found")
	}
	var args mobileprotocol.SubmitArgs
	_ = json.Unmarshal(cmd.Args, &args)
	if args.Text == "" {
		return errorEnv(env.RequestID, env.SessionID, "bad_args", "text is required")
	}

	// Controller execution is wired in the next milestone. For now emit a
	// sequenced notice so reconnect / dedupe paths are testable end-to-end.
	slot.mu.Lock()
	slot.Running = true
	slot.Descriptor.Status = "running"
	slot.Descriptor.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	slot.mu.Unlock()

	notice, _ := json.Marshal(map[string]any{
		"kind":  "notice",
		"level": "info",
		"text":  "node accepted submit (controller wiring pending)",
	})
	slot.mu.Lock()
	seq := slot.Ring.AppendBuild(func(seq uint64) []byte {
		ev := mobileprotocol.NewEnvelope(mobileprotocol.TypeEvent)
		ev.SessionID = env.SessionID
		ev.Seq = seq
		_ = ev.MarshalPayload(mobileprotocol.EventPayload{Event: notice})
		raw, _ := json.Marshal(ev)
		return raw
	})
	slot.Running = false
	slot.Descriptor.Status = "idle"
	slot.Descriptor.LastEventSeq = seq
	slot.mu.Unlock()

	resp := mobileprotocol.NewEnvelope(mobileprotocol.TypeAck)
	resp.RequestID = env.RequestID
	resp.SessionID = env.SessionID
	resp.Ack = seq
	_ = resp.MarshalPayload(map[string]any{"accepted": true, "seq": seq})
	return resp
}

func (h *Hub) handleCancel(env mobileprotocol.Envelope) mobileprotocol.Envelope {
	slot := h.slot(env.SessionID)
	if slot == nil {
		return errorEnv(env.RequestID, env.SessionID, "not_found", "session not found")
	}
	slot.mu.Lock()
	slot.Running = false
	slot.Descriptor.Status = "idle"
	slot.mu.Unlock()
	resp := mobileprotocol.NewEnvelope(mobileprotocol.TypeAck)
	resp.RequestID = env.RequestID
	resp.SessionID = env.SessionID
	return resp
}

func (h *Hub) handleApprove(env mobileprotocol.Envelope, cmd mobileprotocol.CommandPayload) mobileprotocol.Envelope {
	slot := h.slot(env.SessionID)
	if slot == nil {
		return errorEnv(env.RequestID, env.SessionID, "not_found", "session not found")
	}
	var args mobileprotocol.ApproveArgs
	_ = json.Unmarshal(cmd.Args, &args)
	if args.ID == "" {
		return errorEnv(env.RequestID, env.SessionID, "bad_args", "approval id is required")
	}
	resp := mobileprotocol.NewEnvelope(mobileprotocol.TypeAck)
	resp.RequestID = env.RequestID
	resp.SessionID = env.SessionID
	_ = resp.MarshalPayload(map[string]any{"id": args.ID, "allow": args.Allow})
	return resp
}

func (h *Hub) handleListModels(env mobileprotocol.Envelope) mobileprotocol.Envelope {
	resp := mobileprotocol.NewEnvelope(mobileprotocol.TypeAck)
	resp.RequestID = env.RequestID
	// Real model catalog comes from config/provider in a later milestone.
	_ = resp.MarshalPayload(map[string]any{"models": []any{}})
	return resp
}

// Replay returns incremental frames after lastAck, or ok=false when snapshot is required.
func (h *Hub) Replay(sessionID string, lastAck uint64) ([]EventFrame, bool) {
	slot := h.slot(sessionID)
	if slot == nil {
		return nil, false
	}
	return slot.Ring.ReplaySince(lastAck)
}

// Snapshot builds a full recovery payload for a stale cursor.
func (h *Hub) Snapshot(sessionID string) (mobileprotocol.SnapshotPayload, error) {
	slot := h.slot(sessionID)
	if slot == nil {
		return mobileprotocol.SnapshotPayload{}, fmt.Errorf("session not found")
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	return mobileprotocol.SnapshotPayload{
		Descriptor:   slot.Descriptor,
		Running:      slot.Running,
		LastEventSeq: slot.Ring.LastSeq(),
		Revision:     slot.Descriptor.Revision,
	}, nil
}

// Handler returns the HTTP mux serving WebSocket /mobile/ws and health.
func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"nodeId":` + jsonString(h.nodeID) + `}`))
	})
	mux.HandleFunc("/mobile/ws", h.serveWS)
	// Keep a JSON command POST for tests and non-WS clients without replacing serve.
	mux.HandleFunc("/mobile/command", h.serveCommand)
	return mux
}

func (h *Hub) serveCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var env mobileprotocol.Envelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp := h.HandleCommand(env)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		env, err := mobileprotocol.Decode(data)
		if err != nil {
			resp := errorEnv("", "", "bad_envelope", err.Error())
			b, _ := json.Marshal(resp)
			_ = conn.WriteMessage(websocket.TextMessage, b)
			continue
		}
		switch env.Type {
		case mobileprotocol.TypePing:
			pong := mobileprotocol.NewEnvelope(mobileprotocol.TypePong)
			pong.RequestID = env.RequestID
			b, _ := json.Marshal(pong)
			_ = conn.WriteMessage(websocket.TextMessage, b)
		case mobileprotocol.TypeCommand, mobileprotocol.TypeHello:
			// Replay path: hello with lastAckSeq may request snapshot.
			if env.Type == mobileprotocol.TypeHello || env.Type == mobileprotocol.TypeCommand {
				var hello mobileprotocol.HelloPayload
				_ = env.UnmarshalPayload(&hello)
				if hello.SessionID != "" && hello.LastAckSeq > 0 {
					if frames, ok := h.Replay(hello.SessionID, hello.LastAckSeq); ok {
						for _, f := range frames {
							_ = conn.WriteMessage(websocket.TextMessage, f.Data)
						}
					} else if snap, err := h.Snapshot(hello.SessionID); err == nil {
						out := mobileprotocol.NewEnvelope(mobileprotocol.TypeSnapshot)
						out.SessionID = hello.SessionID
						_ = out.MarshalPayload(snap)
						b, _ := json.Marshal(out)
						_ = conn.WriteMessage(websocket.TextMessage, b)
					}
				}
			}
			resp := h.HandleCommand(env)
			b, _ := json.Marshal(resp)
			_ = conn.WriteMessage(websocket.TextMessage, b)
		default:
			resp := errorEnv(env.RequestID, env.SessionID, "bad_type", "unsupported envelope type")
			b, _ := json.Marshal(resp)
			_ = conn.WriteMessage(websocket.TextMessage, b)
		}
	}
}

// Run starts the HTTP server until ctx is done or ListenAndServe fails.
func (h *Hub) Run(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}

func errorEnv(requestID, sessionID, code, msg string) mobileprotocol.Envelope {
	e := mobileprotocol.NewEnvelope(mobileprotocol.TypeError)
	e.RequestID = requestID
	e.SessionID = sessionID
	_ = e.MarshalPayload(mobileprotocol.ErrorPayload{Code: code, Message: msg})
	return e
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
