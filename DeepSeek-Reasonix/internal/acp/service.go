package acp

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/plugin"
)

// SessionParams is everything a Factory needs to assemble one ACP session's
// controller. Sink is owned by this package (an updateSink bound to the session
// id) and must be wired into the controller's event sink; the controller's
// interactive approval (see control.Controller.EnableInteractiveApproval) then
// routes "ask" decisions back through that sink as ApprovalRequest events, which
// the sink forwards to the client over session/request_permission.
//
// The Factory picks the model (ACP's session/new carries no model selection).
// Cwd roots the session's file tools and bash (built via builtin.Workspace).
// MCPServers are the stdio MCP servers the client asked the agent to connect for
// this session.
type SessionParams struct {
	Cwd        string
	MCPServers []plugin.Spec
	Sink       event.Sink
}

// Factory builds the per-session controller. The composition root (the cli's
// `reasonix acp` command) implements it by reusing setup()'s assembly: a
// Provider for Model, a tool Registry rooted at Cwd via builtin.Workspace, a
// per-session MCP host from MCPServers, the event Sink, all wired into a
// control.Controller. The returned controller owns its own cleanup (Close stops
// MCP subprocesses), so the service calls ctrl.Close() on teardown.
type Factory interface {
	NewSession(ctx context.Context, p SessionParams) (*control.Controller, error)
}

// AgentInfo identifies this agent to clients in the initialize reply.
type AgentInfo struct {
	Name    string
	Version string
}

// Serve runs an ACP agent on r/w (stdin/stdout in production) until the input
// ends or ctx is cancelled. It owns the JSON-RPC connection and the session
// registry; the Factory supplies the kernel wiring. This is the single entry
// point the `reasonix acp` command calls.
//
// stdout is the JSON-RPC channel: callers must keep all other output (logs,
// diagnostics) off w and on stderr, or the wire corrupts.
func Serve(ctx context.Context, r io.Reader, w io.Writer, factory Factory, info AgentInfo) error {
	conn := NewConn(r, w)
	svc := &service{
		conn:     conn,
		factory:  factory,
		info:     info,
		sessions: make(map[string]*acpSession),
	}
	conn.Handle("initialize", svc.initialize)
	conn.Handle("session/new", svc.sessionNew)
	conn.Handle("session/load", svc.sessionLoad)
	conn.Handle("session/prompt", svc.sessionPrompt)
	conn.HandleNotify("session/cancel", svc.sessionCancel)
	defer svc.closeAll()
	return conn.Serve(ctx)
}

// service holds the connection-wide ACP state: the factory, agent identity, and
// the live session registry.
type service struct {
	conn    *Conn
	factory Factory
	info    AgentInfo

	mu       sync.Mutex
	sessions map[string]*acpSession
}

// acpSession is one open session: its controller, the on-disk transcript path
// (empty when persistence is off), and the cancel func of the in-flight turn
// (nil when idle) so session/cancel can abort it.
type acpSession struct {
	id         string
	ctrl       *control.Controller
	transcript string

	mu     sync.Mutex
	cancel context.CancelFunc
}

func (s *acpSession) setCancel(c context.CancelFunc) {
	s.mu.Lock()
	s.cancel = c
	s.mu.Unlock()
}

func (s *acpSession) abort() {
	s.mu.Lock()
	c := s.cancel
	s.mu.Unlock()
	if c != nil {
		c()
	}
}

// initialize advertises the agent's capability set: sessions can be resumed via
// session/load (transcripts are keyed by session id under the session dir),
// prompts carry inline resource text (embeddedContext) but not image/audio, and
// MCP is stdio-only (no http/sse) — the latter three matching main.
func (s *service) initialize(_ context.Context, _ json.RawMessage) (any, error) {
	return InitializeResult{
		ProtocolVersion: ProtocolVersion,
		AgentCapabilities: AgentCapabilities{
			LoadSession: true,
			PromptCapabilities: PromptCapabilities{
				Image:           false,
				Audio:           false,
				EmbeddedContext: true,
			},
			MCPCapabilities: MCPCapabilities{HTTP: false, SSE: false},
		},
		AgentInfo:   Implementation{Name: s.info.Name, Version: s.info.Version},
		AuthMethods: []any{},
	}, nil
}

// sessionNew opens a session: it mints an id, builds the session's sink bound to
// that id, asks the Factory to assemble the controller, switches the controller
// to interactive approval (so tool gates surface as ApprovalRequest events the
// sink forwards), and registers it.
func (s *service) sessionNew(ctx context.Context, raw json.RawMessage) (any, error) {
	var p SessionNewParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, &RPCError{Code: ErrInvalidParams, Message: "session/new: " + err.Error()}
		}
	}

	id, err := newSessionID()
	if err != nil {
		return nil, &RPCError{Code: ErrInternal, Message: "session/new: " + err.Error()}
	}

	sink := newUpdateSink(s.conn, id)
	ctrl, err := s.factory.NewSession(ctx, SessionParams{
		Cwd:        p.Cwd,
		MCPServers: mcpSpecs(p.MCPServers),
		Sink:       sink,
	})
	if err != nil {
		return nil, &RPCError{Code: ErrInternal, Message: "session/new: " + err.Error()}
	}
	ctrl.EnableInteractiveApproval()
	sink.bindApprove(ctrl.Approve)

	sess := &acpSession{id: id, ctrl: ctrl}
	// Pin a transcript file keyed by session id when the controller has a session
	// dir, so every turn auto-saves there, session/prompt can hand the path back,
	// and session/load can find it again by id across process restarts.
	if dir := ctrl.SessionDir(); dir != "" {
		sess.transcript = transcriptPath(dir, id)
		ctrl.SetSessionPath(sess.transcript)
	}

	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()

	return SessionNewResult{SessionID: id}, nil
}

// sessionLoad resumes a previously-saved session by id: it builds a controller
// (rooted at the requested cwd), seeds it from the on-disk transcript, replays
// the conversation to the client as session/update notifications, and registers
// it for subsequent prompts. A session already live in this process is replayed
// from memory without rebuilding.
func (s *service) sessionLoad(ctx context.Context, raw json.RawMessage) (any, error) {
	var p SessionLoadParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/load: " + err.Error()}
	}
	if p.SessionID == "" {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/load: missing sessionId"}
	}

	if sess := s.session(p.SessionID); sess != nil {
		newUpdateSink(s.conn, p.SessionID).replay(sess.ctrl.History())
		return SessionLoadResult{}, nil
	}

	sink := newUpdateSink(s.conn, p.SessionID)
	ctrl, err := s.factory.NewSession(ctx, SessionParams{
		Cwd:        p.Cwd,
		MCPServers: mcpSpecs(p.MCPServers),
		Sink:       sink,
	})
	if err != nil {
		return nil, &RPCError{Code: ErrInternal, Message: "session/load: " + err.Error()}
	}
	ctrl.EnableInteractiveApproval()
	sink.bindApprove(ctrl.Approve)

	dir := ctrl.SessionDir()
	if dir == "" {
		ctrl.Close()
		return nil, &RPCError{Code: ErrInternal, Message: "session/load: persistence is disabled"}
	}
	path := transcriptPath(dir, p.SessionID)
	loaded, err := agent.LoadSession(path)
	if err != nil {
		ctrl.Close()
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/load: unknown session " + p.SessionID}
	}
	ctrl.Resume(loaded, path)

	sess := &acpSession{id: p.SessionID, ctrl: ctrl, transcript: path}
	s.mu.Lock()
	s.sessions[p.SessionID] = sess
	s.mu.Unlock()

	sink.replay(ctrl.History())
	return SessionLoadResult{}, nil
}

// transcriptPath is where a session's transcript lives — keyed by id so
// session/load can recover it. Distinct from the cli's timestamp-labelled
// chat/run session files (those are addressed by a picker, not by id).
func transcriptPath(dir, id string) string {
	return filepath.Join(dir, id+".jsonl")
}

// sessionPrompt runs one turn. It flattens the prompt blocks to text and runs the
// session's controller synchronously under a per-turn cancelable context (so
// session/cancel can stop it), then reports why the turn ended. The controller
// streams the turn's events to the session's sink as it runs.
func (s *service) sessionPrompt(ctx context.Context, raw json.RawMessage) (any, error) {
	var p SessionPromptParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/prompt: " + err.Error()}
	}
	sess := s.session(p.SessionID)
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/prompt: unknown session " + p.SessionID}
	}
	text := FlattenPrompt(p.Prompt)
	if text == "" {
		return nil, &RPCError{Code: ErrInvalidParams, Message: "session/prompt: empty prompt"}
	}

	runCtx, cancel := context.WithCancel(ctx)
	sess.setCancel(cancel)
	runErr := sess.ctrl.Run(runCtx, text)
	sess.setCancel(nil)
	cancel()

	// Persist after the turn (best-effort) so a crash loses at most this prompt;
	// save even on cancel/error since the partial conversation is still resumable.
	_ = sess.ctrl.Snapshot()

	stop := StopEndTurn
	if runErr != nil {
		if runCtx.Err() != nil {
			stop = StopCancelled
		} else {
			stop = StopError
		}
	}
	res := SessionPromptResult{StopReason: stop}
	if sess.transcript != "" {
		res.TranscriptPath = &sess.transcript
	}
	return res, nil
}

// sessionCancel aborts a session's in-flight turn, if any. It is a notification:
// no reply, and an unknown session is silently ignored.
func (s *service) sessionCancel(_ context.Context, raw json.RawMessage) {
	var p SessionCancelParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return
	}
	if sess := s.session(p.SessionID); sess != nil {
		sess.abort()
	}
}

func (s *service) session(id string) *acpSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

// closeAll tears down every open session (aborting any in-flight turn and
// stopping its MCP subprocesses) when the connection ends.
func (s *service) closeAll() {
	s.mu.Lock()
	sessions := s.sessions
	s.sessions = make(map[string]*acpSession)
	s.mu.Unlock()
	for _, sess := range sessions {
		sess.abort()
		sess.ctrl.Close()
	}
}

// mcpSpecs converts ACP stdio MCP server declarations to plugin.Spec. ACP's
// session/new only carries stdio servers (the agent advertises http/sse off).
func mcpSpecs(in []MCPServerSpec) []plugin.Spec {
	if len(in) == 0 {
		return nil
	}
	out := make([]plugin.Spec, 0, len(in))
	for _, m := range in {
		out = append(out, plugin.Spec{
			Name:    m.Name,
			Type:    "stdio",
			Command: m.Command,
			Args:    m.Args,
			Env:     m.Env,
		})
	}
	return out
}

// newSessionID returns a random RFC 4122 v4 UUID string used to address a session.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
