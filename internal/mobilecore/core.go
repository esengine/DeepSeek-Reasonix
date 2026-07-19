// Package mobilecore is the on-device Reasonix agent SDK compiled for mobile
// via gomobile bind. It exposes a JSON-string API so Kotlin/Swift Capacitor
// plugins stay thin.
//
// Cache-first rules:
//   - Local tool capabilities freeze at CreateSession and never reorder.
//   - Dynamic device state must be injected as user-turn data by the caller,
//     never by mutating system prompt or tool schemas here.
//
// Provider request serialization reuses the shared Go provider packages; this
// package must not reimplement wire formats in TypeScript/Kotlin/Swift.
package mobilecore

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"reasonix/internal/mobileprotocol"
)

// Core is the process-wide mobile SDK entry. Methods are safe for concurrent
// use from the native bridge.
type Core struct {
	mu       sync.Mutex
	sessions map[string]*localSession
}

type localSession struct {
	desc   mobileprotocol.SessionDescriptor
	events []json.RawMessage
	seq    uint64
	// frozen model/provider refs — immutable for the life of the session.
	modelRef    string
	providerRef string
}

// New constructs an empty Core.
func New() *Core {
	return &Core{sessions: make(map[string]*localSession)}
}

// defaultCore is used by package-level functions that gomobile prefers.
var defaultCore = New()

// CreateSessionJSON creates a local session from a JSON CreateSessionArgs body.
// Returns a JSON SessionDescriptor or a JSON error object.
func CreateSessionJSON(argsJSON string) string {
	return defaultCore.CreateSessionJSON(argsJSON)
}

// CreateSessionJSON is the instance method used by tests and non-gomobile hosts.
func (c *Core) CreateSessionJSON(argsJSON string) string {
	var args mobileprotocol.CreateSessionArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return errJSON("bad_args", err.Error())
	}
	if args.Runtime == "" {
		args.Runtime = mobileprotocol.RuntimeLocal
	}
	if args.Runtime != mobileprotocol.RuntimeLocal {
		return errJSON("invalid_runtime", "mobilecore only creates local sessions")
	}
	id := fmt.Sprintf("local_%d", time.Now().UnixNano())
	d := mobileprotocol.SessionDescriptor{
		ID:          id,
		Runtime:     mobileprotocol.RuntimeLocal,
		ProviderRef: args.ProviderRef,
		Title:       args.Title,
		Revision:    1,
	}
	d.Normalize()
	// Freeze local capability order at creation (cache-sensitive catalog).
	d.Capabilities = append([]string(nil), mobileprotocol.LocalCapabilities...)

	c.mu.Lock()
	c.sessions[id] = &localSession{
		desc:        d,
		modelRef:    args.ModelRef,
		providerRef: args.ProviderRef,
	}
	c.mu.Unlock()

	b, _ := json.Marshal(d)
	return string(b)
}

// SubmitJSON submits a user turn. argsJSON is SubmitArgs; sessionID is required.
func SubmitJSON(sessionID, argsJSON, requestID string) string {
	return defaultCore.SubmitJSON(sessionID, argsJSON, requestID)
}

// SubmitJSON implements the local submit path. Full provider streaming is wired
// in the local-chat milestone; this validates contracts and records events.
func (c *Core) SubmitJSON(sessionID, argsJSON, requestID string) string {
	_ = requestID // reserved for bridge-level dedupe
	var args mobileprotocol.SubmitArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return errJSON("bad_args", err.Error())
	}
	if strings.TrimSpace(args.Text) == "" {
		return errJSON("bad_args", "text is required")
	}
	c.mu.Lock()
	s, ok := c.sessions[sessionID]
	if !ok {
		c.mu.Unlock()
		return errJSON("not_found", "session not found")
	}
	s.seq++
	seq := s.seq
	s.desc.LastEventSeq = seq
	s.desc.Status = "idle"
	s.desc.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	ev, _ := json.Marshal(map[string]any{
		"kind": "notice",
		"text": "mobilecore accepted submit (provider stream pending)",
		"seq":  seq,
	})
	s.events = append(s.events, ev)
	c.mu.Unlock()

	ack := map[string]any{"accepted": true, "seq": seq, "requestId": requestID}
	b, _ := json.Marshal(ack)
	return string(b)
}

// SnapshotJSON returns a SnapshotPayload for the session.
func SnapshotJSON(sessionID string) string {
	return defaultCore.SnapshotJSON(sessionID)
}

// SnapshotJSON returns recovery state for the local session.
func (c *Core) SnapshotJSON(sessionID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sessions[sessionID]
	if !ok {
		return errJSON("not_found", "session not found")
	}
	hist, _ := json.Marshal(s.events)
	snap := mobileprotocol.SnapshotPayload{
		Descriptor:   s.desc,
		History:      hist,
		LastEventSeq: s.desc.LastEventSeq,
		Revision:     s.desc.Revision,
	}
	b, _ := json.Marshal(snap)
	return string(b)
}

// ListModelsJSON returns the configured model catalog placeholder.
// Real enumeration reuses config/provider packages in a later milestone.
func ListModelsJSON() string {
	return `{"models":[]}`
}

// ProbeProviderJSON validates provider endpoint policy before storing a key.
// HTTPS is required by default; http is only allowed for localhost/LAN when
// allowInsecureHttp is true.
func ProbeProviderJSON(argsJSON string) string {
	return defaultCore.ProbeProviderJSON(argsJSON)
}

// ProbeProviderJSON implements provider URL policy checks.
func (c *Core) ProbeProviderJSON(argsJSON string) string {
	var args mobileprotocol.ProbeProviderArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return errJSON("bad_args", err.Error())
	}
	if args.ProviderRef == "" && args.BaseURL == "" {
		return errJSON("bad_args", "providerRef or baseUrl is required")
	}
	if args.BaseURL != "" {
		if err := validateProviderURL(args.BaseURL, args.AllowInsecureHTTP); err != nil {
			return errJSON("insecure_url", err.Error())
		}
	}
	b, _ := json.Marshal(map[string]any{
		"ok":          true,
		"providerRef": args.ProviderRef,
		// Actual network probe lands with provider wiring.
		"probed": false,
	})
	return string(b)
}

// LocalCapabilitiesJSON returns the frozen local capability list.
func LocalCapabilitiesJSON() string {
	b, _ := json.Marshal(mobileprotocol.LocalCapabilities)
	return string(b)
}

// CancelJSON marks a local turn cancel request.
func CancelJSON(sessionID, requestID string) string {
	return defaultCore.CancelJSON(sessionID, requestID)
}

// CancelJSON implements cancel for a local session.
func (c *Core) CancelJSON(sessionID, requestID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sessions[sessionID]
	if !ok {
		return errJSON("not_found", "session not found")
	}
	s.desc.Status = "idle"
	b, _ := json.Marshal(map[string]any{"ok": true, "requestId": requestID})
	return string(b)
}

func validateProviderURL(raw string, allowInsecure bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if !allowInsecure {
			return fmt.Errorf("http requires explicit allowInsecureHttp")
		}
		host := strings.ToLower(u.Hostname())
		if host == "localhost" || host == "127.0.0.1" || host == "::1" || isPrivateHost(host) {
			return nil
		}
		return fmt.Errorf("http only allowed for localhost/LAN")
	default:
		return fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
}

func isPrivateHost(host string) bool {
	// Conservative hostname check; full IP CIDR validation lands with networking.
	return strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "192.168.") ||
		strings.HasPrefix(host, "172.16.") ||
		strings.HasPrefix(host, "172.17.") ||
		strings.HasPrefix(host, "172.18.") ||
		strings.HasPrefix(host, "172.19.") ||
		strings.HasPrefix(host, "172.2") ||
		strings.HasPrefix(host, "172.30.") ||
		strings.HasPrefix(host, "172.31.")
}

func errJSON(code, msg string) string {
	b, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": msg}})
	return string(b)
}
