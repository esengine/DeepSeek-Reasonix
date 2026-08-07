package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// UI DTOs: extension-contributed actions, host-rendered surfaces, and
// blocking prompts.
//
// The extension UI surface is structured-only: payloads are the typed
// documents below and nothing else. HTML, CSS, JavaScript, and URLs are
// never accepted or rendered — hosts map these structures onto their own
// native widgets (TUI, desktop, ACP client, or headless sink).

// UIActionDecl declares one invocable action in the handshake.
type UIActionDecl struct {
	ActionID string `json:"actionId" validate:"nonempty"`
	Label    string `json:"label,omitempty"`
}

// UIActionParams invokes one declared action. Generation pins the session
// generation the action applies to; a stale generation is rejected host-side.
type UIActionParams struct {
	ActionID   string            `json:"actionId" validate:"nonempty"`
	SessionID  string            `json:"sessionId" validate:"nonempty"`
	Generation uint64            `json:"generation"`
	Args       map[string]string `json:"args,omitempty"`
}

// UIActionResult reports whether the extension accepted the invocation.
type UIActionResult struct {
	Accepted bool   `json:"accepted"`
	Message  string `json:"message,omitempty"`
}

// UISubmitParams delivers the values of a previously published form surface
// back to the extension.
type UISubmitParams struct {
	SurfaceID  string         `json:"surfaceId" validate:"nonempty"`
	SessionID  string         `json:"sessionId" validate:"nonempty"`
	Generation uint64         `json:"generation"`
	Values     map[string]any `json:"values"`
}

// UISubmitResult acknowledges the submission.
type UISubmitResult struct {
	Accepted bool `json:"accepted"`
}

// UIPublishParams publishes or replaces one structured surface on the host.
// Payload must decode to the UISurfaceKind's payload type below.
type UIPublishParams struct {
	SurfaceID  string          `json:"surfaceId" validate:"nonempty"`
	SessionID  string          `json:"sessionId" validate:"nonempty"`
	Generation uint64          `json:"generation"`
	Kind       UISurfaceKind   `json:"kind"`
	Payload    json.RawMessage `json:"payload"`
}

// UIPublishResult acknowledges the publish.
type UIPublishResult struct {
	Accepted bool `json:"accepted"`
}

// UIRequestParams asks the host to block on one structured prompt. Payload
// must decode to the UIRequestKind's payload shape (a UIFormPayload-shaped
// document for input/select/multiselect).
type UIRequestParams struct {
	SurfaceID  string          `json:"surfaceId" validate:"nonempty"`
	SessionID  string          `json:"sessionId" validate:"nonempty"`
	Generation uint64          `json:"generation"`
	Kind       UIRequestKind   `json:"kind"`
	Payload    json.RawMessage `json:"payload"`
}

// UIRequestResult carries the user's answer; Cancelled distinguishes
// dismissal from an empty value set.
type UIRequestResult struct {
	Cancelled bool           `json:"cancelled"`
	Values    map[string]any `json:"values,omitempty"`
}

// UIStatusPayload is a one-line status contribution.
type UIStatusPayload struct {
	Label    string     `json:"label" validate:"nonempty"`
	Detail   string     `json:"detail,omitempty"`
	Severity UISeverity `json:"severity,omitempty"`
	Progress *float64   `json:"progress,omitempty"`
}

// UIKeyValue is one labelled value row in a card.
type UIKeyValue struct {
	Key   string `json:"key" validate:"nonempty"`
	Value string `json:"value"`
}

// UIActionRef renders a button that invokes a declared action.
type UIActionRef struct {
	ActionID string `json:"actionId" validate:"nonempty"`
	Label    string `json:"label" validate:"nonempty"`
}

// UICardPayload is a rich read-only surface: Markdown body, key/value rows,
// optional progress, and action buttons.
type UICardPayload struct {
	Title    string        `json:"title,omitempty"`
	Markdown string        `json:"markdown,omitempty"`
	Text     string        `json:"text,omitempty"`
	Fields   []UIKeyValue  `json:"fields,omitempty"`
	Progress *float64      `json:"progress,omitempty"`
	Actions  []UIActionRef `json:"actions,omitempty"`
}

// UIFormField is one input row of a form surface.
type UIFormField struct {
	Key      string      `json:"key" validate:"nonempty"`
	Label    string      `json:"label,omitempty"`
	Kind     UIFieldKind `json:"kind"`
	Options  []string    `json:"options,omitempty"`
	Default  any         `json:"default,omitempty"`
	Required bool        `json:"required,omitempty"`
}

// UIFormPayload is an editable surface; submissions return through
// extension/ui/submit.
type UIFormPayload struct {
	Title   string        `json:"title,omitempty"`
	Message string        `json:"message,omitempty"`
	Fields  []UIFormField `json:"fields"`
}

// UINotificationPayload is a transient toast-style message.
type UINotificationPayload struct {
	Title    string     `json:"title" validate:"nonempty"`
	Body     string     `json:"body,omitempty"`
	Severity UISeverity `json:"severity,omitempty"`
}

// DecodeUIPublishPayload strict-decodes the payload document of one
// host/ui/publish call into the kind's payload struct (UIStatusPayload,
// UICardPayload, UIFormPayload, or UINotificationPayload). It applies the
// same strictness as the method DTO decoders: unknown fields, enum values,
// and required-field violations are rejected.
func DecodeUIPublishPayload(kind UISurfaceKind, raw json.RawMessage) (any, error) {
	var typ reflect.Type
	switch kind {
	case UISurfaceStatus:
		typ = reflect.TypeFor[UIStatusPayload]()
	case UISurfaceCard:
		typ = reflect.TypeFor[UICardPayload]()
	case UISurfaceForm:
		typ = reflect.TypeFor[UIFormPayload]()
	case UISurfaceNotification:
		typ = reflect.TypeFor[UINotificationPayload]()
	default:
		return nil, fmt.Errorf("protocol: unknown UI surface kind %q", kind)
	}
	return decodeAndValidate(raw, typ)
}

// DecodeUIRequestPayload strict-decodes the payload document of one
// host/ui/request call. Every request kind carries a UIFormPayload-shaped
// document: a confirm is a form with one confirm field (or a bare message),
// input/select/multiselect compose the matching field kinds.
func DecodeUIRequestPayload(kind UIRequestKind, raw json.RawMessage) (any, error) {
	switch kind {
	case UIRequestConfirm, UIRequestInput, UIRequestSelect, UIRequestMultiselect:
		return decodeAndValidate(raw, reflect.TypeFor[UIFormPayload]())
	default:
		return nil, fmt.Errorf("protocol: unknown UI request kind %q", kind)
	}
}
