// Package mcpinteraction carries server-initiated MCP elicitation requests
// from the plugin transport to whichever frontend is driving the call, and the
// user's decision back. It has no UI dependency: the plugin layer hands a
// Broker through the per-call context, the controller layer implements it.
package mcpinteraction

import (
	"context"
	"encoding/json"
)

// Elicitation modes defined by MCP 2026-07-28.
const (
	ModeForm = "form"
	ModeURL  = "url"
)

// User actions in answer to an elicitation request.
const (
	ActionAccept  = "accept"
	ActionDecline = "decline"
	ActionCancel  = "cancel"
)

// Request is one server-initiated elicitation: a typed form (flat primitive
// schema) or a URL the server asks the user to visit.
type Request struct {
	// ID is the host-assigned stable identifier for the pending decision,
	// echoed by the frontend in the resolve call.
	ID string
	// Server is the MCP server asking. Surfaced so the user can see who is
	// requesting before answering.
	Server string
	// Mode is "form" or "url".
	Mode string
	// Message is the server's human-readable explanation.
	Message string
	// RequestedSchema is the raw JSON schema for form mode. Flat primitives
	// only (string/number/integer/boolean/enum, defaults, required, bounds).
	RequestedSchema json.RawMessage
	// URL is the credential-free HTTP/HTTPS target for url mode.
	URL string
	// ElicitationID is the server-assigned id for url mode completion.
	ElicitationID string
}

// Result is the user's decision. Action is accept/decline/cancel; Content is
// the submitted form values, present only for accept in form mode.
type Result struct {
	Action  string
	Content map[string]any
}

// Broker delivers one elicitation to the user and blocks until they answer.
// Implementations must respect ctx cancellation. Privacy: brokers must keep
// form values, URL query strings, and free-text answers out of logs and
// telemetry; only the request kind, action, and error class may be recorded.
type Broker interface {
	Interact(ctx context.Context, req Request) (Result, error)
}

type brokerKey struct{}

// WithBroker attaches b to ctx for the duration of one MCP call. The broker
// travels with the call context — never with the shared plugin.Host — so
// concurrent tabs and turns cannot cross wires.
func WithBroker(ctx context.Context, b Broker) context.Context {
	return context.WithValue(ctx, brokerKey{}, b)
}

// FromContext returns the call's broker, or nil when the entry point has no
// decision channel (headless/bot). A nil broker means the request must be
// answered cancel — the model never guesses.
func FromContext(ctx context.Context) Broker {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(brokerKey{}).(Broker)
	return b
}

// SanitizeURLMode validates a url-mode request target. Only credential-free
// HTTP(S) URLs are presentable; anything else is refused before any UI sees it.
func SanitizeURLMode(req Request) bool {
	if req.Mode != ModeURL {
		return true
	}
	return allowedURL(req.URL)
}
