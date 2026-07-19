// Package mobileprotocol defines the versioned mobile transport contract shared
// by mobilecore, reasonix node, and the Capacitor client. It is intentionally
// independent of HTTP serve and desktop Wails so existing frontends stay stable.
package mobileprotocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// CurrentVersion is the envelope schema version understood by this build.
const CurrentVersion = 1

// Envelope type discriminators.
const (
	TypeHello    = "hello"
	TypeCommand  = "command"
	TypeEvent    = "event"
	TypeAck      = "ack"
	TypeSnapshot = "snapshot"
	TypeError    = "error"
	TypePing     = "ping"
	TypePong     = "pong"
)

// Runtime identifiers. Frozen after session creation.
const (
	RuntimeLocal  = "local"
	RuntimeRemote = "remote"
)

// Command names carried in Envelope.Type == TypeCommand payloads.
const (
	CmdCreateSession  = "create_session"
	CmdRestoreSession = "restore_session"
	CmdSubmit         = "submit"
	CmdCancel         = "cancel"
	CmdAnswer         = "answer"
	CmdApprove        = "approve"
	CmdSnapshot       = "snapshot"
	CmdListModels     = "list_models"
	CmdProbeProvider  = "probe_provider"
	CmdSubscribe      = "subscribe"
	CmdHello          = "hello"
)

// LocalCapabilities is the frozen tool surface for on-device sessions.
// Order is stable and must not be reordered casually (cache-sensitive when
// exposed as provider tools).
var LocalCapabilities = []string{
	"web_read",
	"attachment_read",
	"image_input",
	"http_mcp",
}

// RemoteCapabilities is the full node tool surface (informational catalog).
var RemoteCapabilities = []string{
	"shell",
	"git",
	"filesystem",
	"web_read",
	"attachment_read",
	"image_input",
	"http_mcp",
	"stdio_mcp",
	"background_jobs",
	"approval",
}

// Envelope is the versioned mobile wire frame.
//
// JSON field names are stable. New optional fields must use omitempty.
type Envelope struct {
	Version   int             `json:"version"`
	Type      string          `json:"type"`
	RequestID string          `json:"requestId,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Seq       uint64          `json:"seq,omitempty"`
	Ack       uint64          `json:"ack,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// Validate performs cheap structural checks before routing.
func (e Envelope) Validate() error {
	if e.Version <= 0 {
		return fmt.Errorf("mobileprotocol: version must be positive")
	}
	if e.Version > CurrentVersion {
		return fmt.Errorf("mobileprotocol: unsupported version %d (max %d)", e.Version, CurrentVersion)
	}
	if e.Type == "" {
		return fmt.Errorf("mobileprotocol: type is required")
	}
	return nil
}

// NewEnvelope builds a versioned envelope of the given type.
func NewEnvelope(typ string) Envelope {
	return Envelope{Version: CurrentVersion, Type: typ}
}

// MarshalPayload encodes v into the envelope payload.
func (e *Envelope) MarshalPayload(v any) error {
	if v == nil {
		e.Payload = nil
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	e.Payload = b
	return nil
}

// UnmarshalPayload decodes the envelope payload into v.
func (e Envelope) UnmarshalPayload(v any) error {
	if len(e.Payload) == 0 || string(e.Payload) == "null" {
		return nil
	}
	return json.Unmarshal(e.Payload, v)
}

// SessionDescriptor is the durable session index shared by mobile and node.
// Runtime is immutable after create; migration creates a new descriptor.
type SessionDescriptor struct {
	ID           string   `json:"id"`
	Runtime      string   `json:"runtime"` // local | remote
	NodeID       string   `json:"nodeId,omitempty"`
	ProviderRef  string   `json:"providerRef,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Revision     int64    `json:"revision"`
	LastEventSeq uint64   `json:"lastEventSeq"`
	Title        string   `json:"title,omitempty"`
	Status       string   `json:"status,omitempty"` // idle | running | pending_approval | failed
	UpdatedAt    string   `json:"updatedAt,omitempty"`
}

// Normalize fills defaults for a newly created descriptor.
func (d *SessionDescriptor) Normalize() {
	if d.Runtime == "" {
		d.Runtime = RuntimeLocal
	}
	if d.Capabilities == nil {
		if d.Runtime == RuntimeRemote {
			d.Capabilities = append([]string(nil), RemoteCapabilities...)
		} else {
			d.Capabilities = append([]string(nil), LocalCapabilities...)
		}
	}
	if d.Status == "" {
		d.Status = "idle"
	}
	if d.UpdatedAt == "" {
		d.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
}

// ValidRuntime reports whether runtime is a known immutable mode.
func ValidRuntime(runtime string) bool {
	return runtime == RuntimeLocal || runtime == RuntimeRemote
}

// HelloPayload is exchanged when a client connects to a node.
type HelloPayload struct {
	ClientID    string `json:"clientId,omitempty"`
	DeviceName  string `json:"deviceName,omitempty"`
	AppVersion  string `json:"appVersion,omitempty"`
	LastAckSeq  uint64 `json:"lastAckSeq,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
	NodeToken   string `json:"nodeToken,omitempty"`
	ProtocolMax int    `json:"protocolMax,omitempty"`
}

// CommandPayload wraps a named command and its arguments.
type CommandPayload struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// CreateSessionArgs creates a new session on local or remote runtime.
type CreateSessionArgs struct {
	Runtime     string `json:"runtime"`
	ProviderRef string `json:"providerRef,omitempty"`
	Title       string `json:"title,omitempty"`
	// ModelRef freezes the model at creation for local sessions.
	ModelRef string `json:"modelRef,omitempty"`
}

// SubmitArgs is a user message submission.
type SubmitArgs struct {
	Text    string   `json:"text"`
	Display string   `json:"display,omitempty"`
	Images  []string `json:"images,omitempty"` // app-local attachment ids
}

// ApproveArgs answers a tool approval prompt.
type ApproveArgs struct {
	ID      string `json:"id"`
	Allow   bool   `json:"allow"`
	Session bool   `json:"session,omitempty"`
	Persist bool   `json:"persist,omitempty"`
}

// AnswerArgs answers an ask_user prompt.
type AnswerArgs struct {
	ID      string          `json:"id"`
	Answers json.RawMessage `json:"answers"`
}

// ProbeProviderArgs checks connectivity for a provider endpoint.
type ProbeProviderArgs struct {
	ProviderRef string `json:"providerRef"`
	BaseURL     string `json:"baseUrl,omitempty"`
	// AllowInsecureHTTP permits http:// only for localhost/LAN after explicit user opt-in.
	AllowInsecureHTTP bool `json:"allowInsecureHttp,omitempty"`
}

// ErrorPayload is returned on TypeError frames.
type ErrorPayload struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Retry   bool   `json:"retry,omitempty"`
}

// SnapshotPayload is a full session recovery payload when event cursor is stale.
type SnapshotPayload struct {
	Descriptor      SessionDescriptor `json:"descriptor"`
	History         json.RawMessage   `json:"history,omitempty"`
	PartialTurn     json.RawMessage   `json:"partialTurn,omitempty"`
	Todos           json.RawMessage   `json:"todos,omitempty"`
	Running         bool              `json:"running,omitempty"`
	PendingApproval json.RawMessage   `json:"pendingApproval,omitempty"`
	LastEventSeq    uint64            `json:"lastEventSeq"`
	Revision        int64             `json:"revision"`
}

// EventPayload carries a wired session event (eventwire-compatible object).
type EventPayload struct {
	Event json.RawMessage `json:"event"`
}

// Encode is a convenience helper for tests and CLI dumps.
func Encode(e Envelope) ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(e)
}

// Decode parses and validates an envelope.
func Decode(data []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return Envelope{}, err
	}
	if err := e.Validate(); err != nil {
		return Envelope{}, err
	}
	return e, nil
}
