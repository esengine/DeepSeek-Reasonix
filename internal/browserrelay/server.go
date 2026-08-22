// Package browserrelay implements a local WebSocket bridge that lets an AI
// agent drive a Chrome tab through a browser extension (the "relay"). The
// server binds to 127.0.0.1, generates a random token at startup, and accepts
// one extension connection at a time. Built-in browser_* tools in
// internal/tool/builtin use this package to send CDP commands over the wire.
package browserrelay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// DefaultListenAddr is the default listen address. The desktop sets it to
// 127.0.0.1:23002 for a fixed port; the library default of :0 uses a random
// port for test isolation.
var DefaultListenAddr = "127.0.0.1:0"

// authGrace is how long an unauthenticated WebSocket connection may hold the
// single extension slot before being dropped (CSWSH placeholder protection).
const authGrace = 10 * time.Second

// State describes the relay connection state.
type State int

const (
	StateDisconnected State = iota
	StateConnected
	StateAuthorized
)

func (s State) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnected:
		return "connected"
	case StateAuthorized:
		return "authorized"
	default:
		return "unknown"
	}
}

// Status is a snapshot of the relay server's state.
type Status struct {
	Running       bool   `json:"running"`
	State         string `json:"state"`
	Addr          string `json:"addr,omitempty"`
	TokenPrefix   string `json:"token_prefix,omitempty"`
	ExtensionInfo string `json:"extension_info,omitempty"`
}

// Server is a local WebSocket bridge for browser extension relay.
type Server struct {
	mu       sync.Mutex
	writeMu  sync.Mutex // serializes WebSocket writes (gorilla requires one writer)
	ln       net.Listener
	token    string
	addr     string
	conn     *websocket.Conn
	state    State
	extInfo  string // extension version/name reported during auth
	shutdown context.CancelFunc
	wg       sync.WaitGroup

	// pending maps command IDs to response channels for synchronous CDP calls.
	nextID  uint64
	pending map[uint64]chan *cdpResponse
}

// cdpResponse carries a CDP command result or error back to the caller.
type cdpResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// message is the wire format between server and extension.
type message struct {
	Type    string          `json:"type"`
	Token   string          `json:"token,omitempty"`
	ID      uint64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Command string          `json:"command,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
	Info    string          `json:"info,omitempty"`
	Version string          `json:"version,omitempty"`

	// Tab command fields (used by tab_command messages)
	TabID  int    `json:"tabId,omitempty"`
	TabURL string `json:"url,omitempty"`

	// CDP event fields
	EventMethod string          `json:"event_method,omitempty"`
	EventParams json.RawMessage `json:"event_params,omitempty"`
}

// DefaultServer is the package-level server instance used by built-in tools.
// Desktop startup sets it so browser_* tools can reach it.
var DefaultServer *Server

// Send is a convenience wrapper around DefaultServer.Send.
func Send(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if DefaultServer == nil {
		return nil, errors.New("browser relay not initialized")
	}
	return DefaultServer.Send(ctx, method, params)
}

// SendTabCommand is a convenience wrapper around DefaultServer.SendTabCommand.
func SendTabCommand(ctx context.Context, command string, params json.RawMessage) (json.RawMessage, error) {
	if DefaultServer == nil {
		return nil, errors.New("browser relay not initialized")
	}
	return DefaultServer.SendTabCommand(ctx, command, params)
}

// SendTabCommandWithID is a convenience wrapper around DefaultServer.SendTabCommandWithID.
func SendTabCommandWithID(ctx context.Context, command string, tabID int, params json.RawMessage) (json.RawMessage, error) {
	if DefaultServer == nil {
		return nil, errors.New("browser relay not initialized")
	}
	return DefaultServer.SendTabCommandWithID(ctx, command, tabID, params)
}

// SendTabCommandWithURL is a convenience wrapper around DefaultServer.SendTabCommandWithURL.
func SendTabCommandWithURL(ctx context.Context, command string, url string, params json.RawMessage) (json.RawMessage, error) {
	if DefaultServer == nil {
		return nil, errors.New("browser relay not initialized")
	}
	return DefaultServer.SendTabCommandWithURL(ctx, command, url, params)
}

// GetStatus is a convenience wrapper around DefaultServer.Status.
func GetStatus() Status {
	if DefaultServer == nil {
		return Status{}
	}
	return DefaultServer.Status()
}

// NewServer creates an unstarted relay server.
func NewServer() *Server {
	return &Server{
		pending: make(map[uint64]chan *cdpResponse),
		state:   StateDisconnected,
	}
}

// Start generates a random token and begins listening on 127.0.0.1.
// The token is persisted to ~/.reasonix/browser-relay.json so it stays the same
// across restarts — the extension only needs to be configured once.
func (s *Server) Start(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ln != nil {
		return s.addr, nil
	}

	token, err := loadOrCreateToken()
	if err != nil {
		return "", fmt.Errorf("token: %w", err)
	}
	s.token = token

	ln, err := net.Listen("tcp", DefaultListenAddr)
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}
	s.ln = ln
	s.addr = ln.Addr().String()

	ctx, cancel := context.WithCancel(ctx)
	s.shutdown = cancel

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWS)

	srv := &http.Server{
		Handler: mux,
		// ReadHeaderTimeout prevents slow-loris attacks.
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("browserrelay: server error", "err", err)
		}
	}()

	// Store shutdown func for graceful stop.
	s.shutdown = func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}

	slog.Info("browserrelay: server started", "addr", s.addr)
	return s.addr, nil
}

// Stop gracefully shuts down the server and closes any active connection.
func (s *Server) Stop() {
	s.mu.Lock()
	shutdown := s.shutdown
	conn := s.conn
	s.conn = nil
	s.ln = nil
	s.state = StateDisconnected
	s.mu.Unlock()

	if shutdown != nil {
		shutdown()
	}
	if conn != nil {
		conn.Close()
	}
	s.wg.Wait()
}

// Status returns a snapshot of the current server state.
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := ""
	if len(s.token) > 8 {
		prefix = s.token[:8] + "..."
	} else if s.token != "" {
		prefix = s.token[:min(len(s.token), 8)] + "..."
	}
	status := Status{
		Running:     s.ln != nil,
		State:       s.state.String(),
		Addr:        s.addr,
		TokenPrefix: prefix,
	}
	if s.state == StateAuthorized {
		status.ExtensionInfo = s.extInfo
	}
	return status
}

// Token returns the authentication token for this server instance.
func (s *Server) Token() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

// RotateToken generates a new auth token, persists it, and drops any active
// connection — a rotated token invalidates the extension's current session.
func (s *Server) RotateToken() (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	if err := persistToken(tokenFilePath(), token); err != nil {
		return "", err
	}

	s.mu.Lock()
	s.token = token
	conn := s.conn
	s.conn = nil
	s.state = StateDisconnected
	s.extInfo = ""
	for id, ch := range s.pending {
		ch <- &cdpResponse{Error: "token rotated"}
		delete(s.pending, id)
	}
	s.mu.Unlock()

	if conn != nil {
		s.sendClose(conn, 4000, "token rotated")
		conn.Close()
	}
	slog.Info("browserrelay: token rotated")
	return token, nil
}

// Addr returns the listening address.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Send executes a CDP command synchronously. It blocks until the extension
// returns a result or the context is cancelled.
func (s *Server) Send(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	return s.sendCommand(ctx, "cdp_command", method, params)
}

// SendTabCommand executes a tab management command (list_pages, select_page, etc.)
// through the extension's Chrome API, not CDP.
func (s *Server) SendTabCommand(ctx context.Context, command string, params json.RawMessage) (json.RawMessage, error) {
	return s.sendCommand(ctx, "tab_command", command, params)
}

// SendTabCommandWithID sends a tab management command with a specific tab ID.
// Used for select_page, close_page, and other tab-specific commands.
func (s *Server) SendTabCommandWithID(ctx context.Context, command string, tabID int, params json.RawMessage) (json.RawMessage, error) {
	return s.sendMessage(ctx, &message{
		Type:    "tab_command",
		Method:  command,
		Command: command,
		Params:  params,
		TabID:   tabID,
	})
}

// SendTabCommandWithURL sends a tab management command with a URL parameter.
// Used for new_page.
func (s *Server) SendTabCommandWithURL(ctx context.Context, command string, url string, params json.RawMessage) (json.RawMessage, error) {
	return s.sendMessage(ctx, &message{
		Type:    "tab_command",
		Method:  command,
		Command: command,
		Params:  params,
		TabURL:  url,
	})
}

// sendCommand is the generic synchronous command sender.
func (s *Server) sendCommand(ctx context.Context, msgType, method string, params json.RawMessage) (json.RawMessage, error) {
	msg := &message{
		Type:   msgType,
		Method: method,
		Params: params,
	}
	// Tab commands are dispatched by the extension on the `command` field,
	// CDP commands on `method`. Set both so tab_command messages reach it.
	if msgType == "tab_command" {
		msg.Command = method
	}
	return s.sendMessage(ctx, msg)
}

// sendMessage sends a pre-built message and waits for the response.
// sendJSON writes a message to the extension connection. Writes are
// serialized because gorilla/websocket allows only one concurrent writer;
// command replies, auth responses and the token-rotation close can otherwise
// race on the same connection.
func (s *Server) sendJSON(conn *websocket.Conn, msg any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return conn.WriteJSON(msg)
}

// sendClose writes a control close frame, serialized like sendJSON.
func (s *Server) sendClose(conn *websocket.Conn, code int, text string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(code, text))
}

func (s *Server) sendMessage(ctx context.Context, msg *message) (json.RawMessage, error) {
	s.mu.Lock()
	if s.state != StateAuthorized || s.conn == nil {
		s.mu.Unlock()
		return nil, errors.New("browser not connected: extension not authorized")
	}
	id := s.nextID
	s.nextID++
	ch := make(chan *cdpResponse, 1)
	s.pending[id] = ch
	msg.ID = id
	conn := s.conn
	s.mu.Unlock()

	// Send command to extension.
	if err := s.sendJSON(conn, msg); err != nil {
		s.handleDisconnect()
		return nil, fmt.Errorf("send %s: %w", msg.Type, err)
	}

	// Wait for response.
	select {
	case resp := <-ch:
		if resp.Error != "" {
			return nil, fmt.Errorf("command error: %s", resp.Error)
		}
		return resp.Result, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return nil, ctx.Err()
	}
}

// handleWS upgrades HTTP to WebSocket and manages the extension connection.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		// Safe: the server only binds to 127.0.0.1, so no external origin can
		// reach it. Accepting any origin is harmless in this local-only context.
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("browserrelay: ws upgrade failed", "err", err)
		return
	}

	s.mu.Lock()
	// Only one extension connection at a time.
	if s.conn != nil {
		s.mu.Unlock()
		s.sendClose(conn, 4000, "already connected")
		conn.Close()
		return
	}
	s.conn = conn
	s.state = StateConnected
	s.mu.Unlock()

	// Require authentication within a grace period; otherwise the connection
	// is dropped so a stray client cannot hold the single slot forever.
	conn.SetReadDeadline(time.Now().Add(authGrace))

	slog.Info("browserrelay: extension connected", "remote", conn.RemoteAddr())

	// Read loop.
	for {
		var msg message
		if err := conn.ReadJSON(&msg); err != nil {
			slog.Info("browserrelay: connection closed", "err", err)
			s.handleDisconnect()
			return
		}

		switch msg.Type {
		case "auth":
			s.handleAuth(conn, msg)
		case "cdp_result":
			s.handleCDPResult(msg)
		case "cdp_event":
			s.handleCDPEvent(msg)
		case "tab_result":
			s.handleCDPResult(msg) // same logic: route by ID
		default:
			slog.Warn("browserrelay: unknown message type", "type", msg.Type)
		}
	}
}

// handleAuth validates the extension's token.
func (s *Server) handleAuth(conn *websocket.Conn, msg message) {
	s.mu.Lock()
	valid := s.token != "" && msg.Token == s.token
	if valid {
		s.state = StateAuthorized
		s.extInfo = msg.Info
		// Authenticated: lift the auth grace deadline.
		conn.SetReadDeadline(time.Time{})
	}
	s.mu.Unlock()

	if valid {
		s.sendJSON(conn, message{Type: "auth_ok", Version: "1"})
		slog.Info("browserrelay: extension authorized", "info", msg.Info)
	} else {
		s.sendJSON(conn, message{Type: "auth_error", Error: "invalid token"})
		slog.Warn("browserrelay: auth failed: invalid token")
		conn.Close()
	}
}

// handleCDPResult routes a CDP response to the pending caller.
func (s *Server) handleCDPResult(msg message) {
	s.mu.Lock()
	ch, ok := s.pending[msg.ID]
	delete(s.pending, msg.ID)
	s.mu.Unlock()

	if ok {
		ch <- &cdpResponse{
			Result: msg.Result,
			Error:  msg.Error,
		}
	}
}

// handleCDPEvent processes CDP events from the extension.
// Currently logged; future versions may relay to the agent.
func (s *Server) handleCDPEvent(msg message) {
	slog.Debug("browserrelay: CDP event",
		"method", msg.EventMethod,
		"has_params", msg.EventParams != nil,
	)
}

// handleDisconnect cleans up after the extension disconnects.
func (s *Server) handleDisconnect() {
	s.mu.Lock()
	s.conn = nil
	s.state = StateDisconnected
	s.extInfo = ""
	// Fail all pending commands.
	for id, ch := range s.pending {
		ch <- &cdpResponse{Error: "connection lost"}
		delete(s.pending, id)
	}
	s.mu.Unlock()
}

// tokenFile is the path to the persisted token file.
func tokenFilePath() string {
	home := os.Getenv("REASONIX_HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
		if home != "" {
			home = filepath.Join(home, ".reasonix")
		}
	}
	if home == "" {
		home = "/tmp"
	}
	return filepath.Join(home, "browser-relay.json")
}

// persistToken atomically writes the token to its JSON file.
func persistToken(path, token string) error {
	os.MkdirAll(filepath.Dir(path), 0700)
	payload, _ := json.Marshal(map[string]string{"token": token})
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadOrCreateToken loads a persisted token or generates a new one.
func loadOrCreateToken() (string, error) {
	path := tokenFilePath()
	data, err := os.ReadFile(path)
	if err == nil {
		var stored struct {
			Token string `json:"token"`
		}
		if json.Unmarshal(data, &stored) == nil && stored.Token != "" {
			return stored.Token, nil
		}
	}
	// Generate new token and persist.
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	if err := persistToken(path, token); err != nil {
		return "", err
	}
	return token, nil
}

// generateToken creates a 32-byte random hex string.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
