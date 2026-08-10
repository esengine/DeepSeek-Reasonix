package dispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
)

// Host payload DTOs: one struct per intercept point. These are the host-side
// shapes the dispatcher marshals into extension/intercept params and — more
// importantly — the shapes an extension's "replace" answer is strictly
// re-decoded against before it may substitute the live value. JSON field
// names are camelCase, matching the protocol package's DTO convention.

// InputPayload is the input.receive payload: one user input line.
type InputPayload struct {
	Text string `json:"text,omitempty"`
}

// Point returns the intercept point this payload serves.
func (InputPayload) Point() extension.InterceptorPoint { return extension.PointInputReceive }

// Validate enforces the required fields: text must be non-empty (an
// extension emptying the input should block instead).
func (p *InputPayload) Validate() error {
	if p.Text == "" {
		return errors.New("text must be non-empty")
	}
	return nil
}

// AgentStartPayload is the agent.before_start payload.
type AgentStartPayload struct {
	Model     string `json:"model,omitempty"`
	ToolCount int    `json:"toolCount,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

// Point returns the intercept point this payload serves.
func (AgentStartPayload) Point() extension.InterceptorPoint { return extension.PointAgentBeforeStart }

// Validate enforces the required fields.
func (p *AgentStartPayload) Validate() error {
	if p.SessionID == "" {
		return errors.New("sessionId must be non-empty")
	}
	return nil
}

// SystemPromptPayload is the system_prompt.build payload.
type SystemPromptPayload struct {
	Prompt        string `json:"prompt,omitempty"`
	WorkspaceRoot string `json:"workspaceRoot,omitempty"`
}

// Point returns the intercept point this payload serves.
func (SystemPromptPayload) Point() extension.InterceptorPoint {
	return extension.PointSystemPromptBuild
}

// Validate enforces the required fields. The prompt itself may be empty: a
// strategy owner intentionally blanking the prompt is a policy question, not
// a shape violation.
func (p *SystemPromptPayload) Validate() error {
	if p.WorkspaceRoot == "" {
		return errors.New("workspaceRoot must be non-empty")
	}
	return nil
}

// ContextPayload is the context.prepare payload.
type ContextPayload struct {
	Messages []protocol.ProviderMessage `json:"messages,omitempty"`
}

// Point returns the intercept point this payload serves.
func (ContextPayload) Point() extension.InterceptorPoint { return extension.PointContextPrepare }

// Validate enforces the required fields: a replacement must carry the
// messages array explicitly, even when empty.
func (p *ContextPayload) Validate() error {
	if p.Messages == nil {
		return errors.New("messages must be an array")
	}
	return nil
}

// ProviderRequestPayload is the provider.request payload.
type ProviderRequestPayload struct {
	Request protocol.ProviderRequest `json:"request"`
}

// Point returns the intercept point this payload serves.
func (ProviderRequestPayload) Point() extension.InterceptorPoint {
	return extension.PointProviderRequest
}

// Validate enforces the request invariants, including the JSON-Schema shape
// of every tool's parameters (protocol.ProviderRequest.Validate).
func (p *ProviderRequestPayload) Validate() error {
	return p.Request.Validate()
}

// ProviderResponsePayload is the provider.response payload: the assembled
// terminal response of one provider stream.
type ProviderResponsePayload struct {
	Text      string                      `json:"text,omitempty"`
	Reasoning string                      `json:"reasoning,omitempty"`
	Signature string                      `json:"signature,omitempty"`
	Calls     []protocol.ProviderToolCall `json:"calls,omitempty"`
	Usage     *protocol.ProviderUsage     `json:"usage,omitempty"`
}

// Point returns the intercept point this payload serves.
func (ProviderResponsePayload) Point() extension.InterceptorPoint {
	return extension.PointProviderResponse
}

// Validate enforces the required fields: every tool call must carry its
// provider-visible identity.
func (p *ProviderResponsePayload) Validate() error {
	for i, call := range p.Calls {
		if call.ID == "" || call.Name == "" {
			return fmt.Errorf("calls[%d]: id and name must be non-empty", i)
		}
	}
	return nil
}

// ToolBeforePayload is the tool.before payload. Arguments is the tool's JSON
// argument object in text form.
type ToolBeforePayload struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// Point returns the intercept point this payload serves.
func (ToolBeforePayload) Point() extension.InterceptorPoint { return extension.PointToolBefore }

// Validate enforces the required fields plus the JSON shape of the tool
// arguments.
func (p *ToolBeforePayload) Validate() error {
	if p.Name == "" {
		return errors.New("name must be non-empty")
	}
	return validateArguments(p.Arguments)
}

// ToolAfterPayload is the tool.after payload.
type ToolAfterPayload struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
	IsError   bool   `json:"isError,omitempty"`
}

// Point returns the intercept point this payload serves.
func (ToolAfterPayload) Point() extension.InterceptorPoint { return extension.PointToolAfter }

// Validate enforces the required fields plus the JSON shape of the tool
// arguments.
func (p *ToolAfterPayload) Validate() error {
	if p.Name == "" {
		return errors.New("name must be non-empty")
	}
	return validateArguments(p.Arguments)
}

// PermissionPayload is the permission.decision payload. HostDecision is the
// verdict the host reached on its own ("allow" or "deny"); an extension's
// allow may override a host deny (the dispatcher records an audit note),
// never the reverse without the caller's combination rule.
type PermissionPayload struct {
	Name         string `json:"name,omitempty"`
	Arguments    string `json:"arguments,omitempty"`
	ReadOnly     bool   `json:"readOnly,omitempty"`
	HostDecision string `json:"hostDecision,omitempty"`
}

// Point returns the intercept point this payload serves.
func (PermissionPayload) Point() extension.InterceptorPoint { return extension.PointPermissionDecision }

// Validate enforces the required fields, the host-decision enum, and the
// JSON shape of the tool arguments.
func (p *PermissionPayload) Validate() error {
	if p.Name == "" {
		return errors.New("name must be non-empty")
	}
	if p.HostDecision != "allow" && p.HostDecision != "deny" {
		return fmt.Errorf("hostDecision must be %q or %q", "allow", "deny")
	}
	return validateArguments(p.Arguments)
}

// CompactionPreparePayload is the compaction.prepare payload.
type CompactionPreparePayload struct {
	Messages []protocol.ProviderMessage `json:"messages,omitempty"`
	Guidance string                     `json:"guidance,omitempty"`
}

// Point returns the intercept point this payload serves.
func (CompactionPreparePayload) Point() extension.InterceptorPoint {
	return extension.PointCompactionPrepare
}

// Validate enforces the required fields: a replacement must carry the
// messages array explicitly, even when empty.
func (p *CompactionPreparePayload) Validate() error {
	if p.Messages == nil {
		return errors.New("messages must be an array")
	}
	return nil
}

// CompactionCompletePayload is the compaction.complete payload.
type CompactionCompletePayload struct {
	Summary string `json:"summary,omitempty"`
}

// Point returns the intercept point this payload serves.
func (CompactionCompletePayload) Point() extension.InterceptorPoint {
	return extension.PointCompactionComplete
}

// Validate enforces the required fields.
func (p *CompactionCompletePayload) Validate() error {
	if p.Summary == "" {
		return errors.New("summary must be non-empty")
	}
	return nil
}

// Session phases: the SessionPayload.Phase values, one per session.* point.
const (
	PhaseStart  = "start"
	PhaseEnd    = "end"
	PhaseLoad   = "load"
	PhaseSave   = "save"
	PhaseRotate = "rotate"
)

// SessionPayload serves all five session.* points; Phase distinguishes them
// and must agree with the point being dispatched.
type SessionPayload struct {
	SessionPath string `json:"sessionPath,omitempty"`
	Phase       string `json:"phase,omitempty"`
}

// Point returns the family representative; the registry maps this payload to
// all five session.* points.
func (SessionPayload) Point() extension.InterceptorPoint { return extension.PointSessionStart }

// Validate enforces the required fields and the phase enum.
func (p *SessionPayload) Validate() error {
	switch p.Phase {
	case PhaseStart, PhaseEnd, PhaseLoad, PhaseSave, PhaseRotate:
		return nil
	default:
		return fmt.Errorf("phase must be one of %q, %q, %q, %q, %q",
			PhaseStart, PhaseEnd, PhaseLoad, PhaseSave, PhaseRotate)
	}
}

// FrontendEventPayload is the frontend.event payload.
type FrontendEventPayload struct {
	Kind   string `json:"kind,omitempty"`
	Text   string `json:"text,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Point returns the intercept point this payload serves.
func (FrontendEventPayload) Point() extension.InterceptorPoint { return extension.PointFrontendEvent }

// Validate enforces the required fields.
func (p *FrontendEventPayload) Validate() error {
	if p.Kind == "" {
		return errors.New("kind must be non-empty")
	}
	return nil
}

// validateArguments enforces the tool-arguments shape: empty (no arguments)
// or a valid JSON object.
func validateArguments(arguments string) error {
	if arguments == "" {
		return nil
	}
	trimmed := bytes.TrimSpace([]byte(arguments))
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return errors.New("arguments must be a JSON object")
	}
	return nil
}

// payloadFactory returns a fresh pointer to one point's payload struct.
type payloadFactory func() any

// payloadRegistry maps each of the 17 intercept points to the factory for
// its payload DTO, so replace answers decode strictly into a fresh value of
// the right type.
var payloadRegistry = map[extension.InterceptorPoint]payloadFactory{
	extension.PointInputReceive:       func() any { return &InputPayload{} },
	extension.PointAgentBeforeStart:   func() any { return &AgentStartPayload{} },
	extension.PointSystemPromptBuild:  func() any { return &SystemPromptPayload{} },
	extension.PointContextPrepare:     func() any { return &ContextPayload{} },
	extension.PointProviderRequest:    func() any { return &ProviderRequestPayload{} },
	extension.PointProviderResponse:   func() any { return &ProviderResponsePayload{} },
	extension.PointToolBefore:         func() any { return &ToolBeforePayload{} },
	extension.PointToolAfter:          func() any { return &ToolAfterPayload{} },
	extension.PointPermissionDecision: func() any { return &PermissionPayload{} },
	extension.PointCompactionPrepare:  func() any { return &CompactionPreparePayload{} },
	extension.PointCompactionComplete: func() any { return &CompactionCompletePayload{} },
	extension.PointSessionStart:       func() any { return &SessionPayload{} },
	extension.PointSessionEnd:         func() any { return &SessionPayload{} },
	extension.PointSessionLoad:        func() any { return &SessionPayload{} },
	extension.PointSessionSave:        func() any { return &SessionPayload{} },
	extension.PointSessionRotate:      func() any { return &SessionPayload{} },
	extension.PointFrontendEvent:      func() any { return &FrontendEventPayload{} },
}

// decodePayload strictly decodes a replacement payload for point: unknown
// fields are rejected, trailing JSON is rejected, and the DTO's Validate runs
// before the value may substitute the live payload. Session payloads must
// also agree with the point being dispatched (a "start" payload cannot
// replace session.save).
func decodePayload(point extension.InterceptorPoint, raw json.RawMessage) (any, error) {
	factory, ok := payloadRegistry[point]
	if !ok {
		return nil, fmt.Errorf("no payload DTO registered for %s", point)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("replacement is empty")
	}
	fresh := factory()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(fresh); err != nil {
		return nil, fmt.Errorf("replacement does not match the %s payload: %w", point, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("replacement contains trailing JSON")
	}
	validatable, ok := fresh.(interface{ Validate() error })
	if !ok {
		return nil, fmt.Errorf("payload DTO for %s has no Validate method", point)
	}
	if err := validatable.Validate(); err != nil {
		return nil, err
	}
	if session, ok := fresh.(*SessionPayload); ok {
		if want := extension.InterceptorPoint("session." + session.Phase); want != point {
			return nil, fmt.Errorf("phase %q does not match point %s", session.Phase, point)
		}
	}
	return fresh, nil
}
