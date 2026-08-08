// Package uihub implements the host side of the Extension Protocol v2
// structured UI surface (stage 8a). One Hub serves host/ui/publish and
// host/ui/request for every sidecar client of a runtime generation:
// publications are strict-decoded, credential-redacted, and emitted as
// frontend events; blocking prompts are translated onto the host's Ask
// machinery; and handshake-declared actions are registered for
// /<plugin>:<action> invocation and form submission routing.
//
// Stability contract: a publication or request whose generation or session
// does not match the hub's current binding is dropped with a debug log (late
// results after a reload must never overwrite the new generation's state);
// calls from unknown or crashed clients are rejected. All sidecar-sourced
// user-visible text passes secrets.RedactCredentials before surfacing.
package uihub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/sidecar"
	"reasonix/internal/secrets"
)

// UIHandler is the sidecar package's Extension → Host UI call surface,
// re-exported so hub bindings and the sidecar manager's UIBinder contract
// share ONE interface type (Go interface satisfaction is signature-exact —
// a same-shaped local interface would silently fail the manager's
// per-plugin binding path).
type UIHandler = sidecar.UIHandler

// ActionClient is the subset of a sidecar client the hub needs for
// host-initiated UI calls. *sidecar.Client satisfies it.
type ActionClient interface {
	UIAction(ctx context.Context, p protocol.UIActionParams) (protocol.UIActionResult, error)
	UISubmit(ctx context.Context, p protocol.UISubmitParams) (protocol.UISubmitResult, error)
}

// ClientResolver resolves a plugin ID to its live sidecar client, or nil when
// the plugin has no running sidecar in this generation.
type ClientResolver func(pluginID string) ActionClient

// HubRequest is one blocking prompt translated from a host/ui/request call,
// ready for the host's Ask machinery. Every user-visible string is already
// credential-redacted.
type HubRequest struct {
	PluginID  string
	SurfaceID string
	SessionID string
	Kind      protocol.UIRequestKind
	Title     string
	Message   string
	Fields    []protocol.UIFormField
}

// RequestFunc answers one blocking prompt. The returned values are keyed by
// form field key; cancelled reports a dismissal (distinct from an empty value
// set); err reports a channel failure.
type RequestFunc func(ctx context.Context, req HubRequest) (values map[string]any, cancelled bool, err error)

// Options configures a Hub. Emit and Request are the frontend seams: Emit
// receives the extension surface/status events, Request answers blocking
// prompts through the host's Ask machinery. Resolve is optional at
// construction because the sidecar manager only exists after StartPackages —
// bind it with SetResolver once clients are live.
type Options struct {
	SessionID  string
	Generation uint64
	Owner      *extension.RuntimeOwner
	Emit       func(event.Event)
	Request    RequestFunc
	Warn       func(string)
	Resolve    ClientResolver
}

// ActionView is one registered extension action for frontend enumeration.
// Slash is the public invocation name, "/<plugin>:<action>".
type ActionView struct {
	PluginID string
	ActionID string
	Label    string
	Slash    string
}

// Hub is the per-generation host UI hub. It is constructed at build time,
// bound to the build's session ID and snapshot generation, and retired with
// its controller; BindGeneration re-binds it across a reload. The mutex makes
// every method safe for concurrent sidecar traffic.
type Hub struct {
	mu         sync.Mutex
	sessionID  string
	generation uint64
	owner      *extension.RuntimeOwner
	emit       func(event.Event)
	requestFn  RequestFunc
	warn       func(string)
	resolve    ClientResolver
	known      map[string]bool
	crashed    map[string]bool
	actions    map[string]map[string]protocol.UIActionDecl
}

// New builds a Hub bound to one session ID and generation.
func New(opts Options) *Hub {
	owner := opts.Owner
	if owner == nil {
		owner = extension.RuntimeOwnerOrDefault(nil)
	}
	return &Hub{
		sessionID:  strings.TrimSpace(opts.SessionID),
		generation: opts.Generation,
		owner:      owner,
		emit:       opts.Emit,
		requestFn:  opts.Request,
		warn:       opts.Warn,
		resolve:    opts.Resolve,
		known:      map[string]bool{},
		crashed:    map[string]bool{},
		actions:    map[string]map[string]protocol.UIActionDecl{},
	}
}

// SessionID returns the session the hub is bound to.
func (h *Hub) SessionID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessionID
}

// Generation returns the generation the hub is bound to.
func (h *Hub) Generation() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.generation
}

// BindGeneration re-binds session/generation after reload. Narrow rebuild
// stage reuses the hub without this call, so next-gen host/ui/* during
// handshake/ready is dropped until commit; do not rely on UI before publish.
func (h *Hub) BindGeneration(sessionID string, gen uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessionID = strings.TrimSpace(sessionID)
	h.generation = gen
}

// SetResolver installs the client resolver once the sidecar manager exists.
func (h *Hub) SetResolver(r ClientResolver) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resolve = r
}

// HandlerFor returns the UIHandler binding the sidecar manager installs on
// one client's connection. Binding marks the plugin known and live: a publish
// or request is attributed to the plugin through the returned value.
func (h *Hub) HandlerFor(pluginID string) UIHandler {
	pluginID = strings.TrimSpace(pluginID)
	h.mu.Lock()
	h.known[pluginID] = true
	delete(h.crashed, pluginID)
	h.mu.Unlock()
	return binding{pluginID: pluginID, hub: h}
}

// ClientCrashed marks a bound plugin's sidecar dead. Its later UI calls are
// rejected until a fresh HandlerFor binding marks the replacement live.
func (h *Hub) ClientCrashed(pluginID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.known[pluginID] {
		h.crashed[pluginID] = true
	}
}

// Publish implements the bare UIHandler. Without a per-plugin binding the hub
// cannot attribute the publication, so the unbound method rejects.
func (h *Hub) Publish(context.Context, protocol.UIPublishParams) (protocol.UIPublishResult, error) {
	return protocol.UIPublishResult{}, unknownClientError("")
}

// Request implements the bare UIHandler; like Publish it requires a binding.
func (h *Hub) Request(context.Context, protocol.UIRequestParams) (protocol.UIRequestResult, error) {
	return protocol.UIRequestResult{}, unknownClientError("")
}

// binding is the per-plugin UIHandler the sidecar manager installs.
type binding struct {
	pluginID string
	hub      *Hub
}

func (b binding) Publish(ctx context.Context, p protocol.UIPublishParams) (protocol.UIPublishResult, error) {
	return b.hub.publish(b.pluginID, ctx, p)
}

func (b binding) Request(ctx context.Context, p protocol.UIRequestParams) (protocol.UIRequestResult, error) {
	return b.hub.request(b.pluginID, ctx, p)
}

// gate enforces the binding and staleness rules: unknown or crashed clients
// are rejected; a generation or session mismatch is a silent drop (stale) —
// late results after a reload must never overwrite the new generation's
// state, and a wedged old sidecar must not fail either.
func (h *Hub) gate(pluginID, sessionID string, generation uint64) (stale bool, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.known[pluginID] {
		return false, unknownClientError(pluginID)
	}
	if h.crashed[pluginID] {
		return false, &protocol.ProtocolError{
			Reason:  protocol.ErrProviderInterrupted,
			Message: "extension sidecar " + pluginID + " crashed",
		}
	}
	if generation != h.generation {
		slog.Debug("uihub: dropping stale-generation UI call", "plugin", pluginID, "got", generation, "current", h.generation)
		return true, nil
	}
	// Also drop generations that have been superseded on the process gate
	// (older than the published generation after a successful rebuild).
	if h.owner.Gate.IsStale(generation) {
		slog.Debug("uihub: dropping publish-gate-stale UI call", "plugin", pluginID, "got", generation)
		return true, nil
	}
	if sessionID != h.sessionID {
		slog.Debug("uihub: dropping UI call for an unbound session", "plugin", pluginID, "session", sessionID, "bound", h.sessionID)
		return true, nil
	}
	return false, nil
}

// publish serves one host/ui/publish call: gate, strict-decode by kind,
// redact, emit the matching extension event, acknowledge.
func (h *Hub) publish(pluginID string, _ context.Context, p protocol.UIPublishParams) (protocol.UIPublishResult, error) {
	stale, err := h.gate(pluginID, p.SessionID, p.Generation)
	if err != nil {
		return protocol.UIPublishResult{}, err
	}
	if stale {
		return protocol.UIPublishResult{Accepted: false}, nil
	}
	payload, err := decodePublishEvent(pluginID, p)
	if err != nil {
		return protocol.UIPublishResult{}, err
	}
	kind := event.ExtensionSurface
	if p.Kind == protocol.UISurfaceStatus {
		kind = event.ExtensionStatus
	}
	h.emitEvent(event.Event{Kind: kind, Extension: payload})
	return protocol.UIPublishResult{Accepted: true}, nil
}

// request serves one host/ui/request call: gate, strict-decode the form,
// redact, block on the host's Ask channel, and map the answers back to the
// protocol result. A stale request is answered cancelled immediately so the
// old sidecar never wedges on a prompt nobody will see.
func (h *Hub) request(pluginID string, ctx context.Context, p protocol.UIRequestParams) (protocol.UIRequestResult, error) {
	stale, err := h.gate(pluginID, p.SessionID, p.Generation)
	if err != nil {
		return protocol.UIRequestResult{}, err
	}
	if stale {
		return protocol.UIRequestResult{Cancelled: true}, nil
	}
	decoded, err := protocol.DecodeUIRequestPayload(p.Kind, p.Payload)
	if err != nil {
		return protocol.UIRequestResult{}, &protocol.ProtocolError{
			Reason:  protocol.ErrInvalidParams,
			Message: "invalid " + string(p.Kind) + " request payload: " + err.Error(),
		}
	}
	form := decoded.(protocol.UIFormPayload)
	h.mu.Lock()
	request := h.requestFn
	h.mu.Unlock()
	if request == nil {
		return protocol.UIRequestResult{}, &protocol.ProtocolError{
			Reason:  protocol.ErrUnknownMethod,
			Message: "extension UI is not available on this host",
		}
	}
	values, cancelled, err := request(ctx, HubRequest{
		PluginID:  pluginID,
		SurfaceID: p.SurfaceID,
		SessionID: p.SessionID,
		Kind:      p.Kind,
		Title:     secrets.RedactCredentials(form.Title),
		Message:   secrets.RedactCredentials(form.Message),
		Fields:    redactFormFields(form.Fields),
	})
	if err != nil {
		return protocol.UIRequestResult{}, err
	}
	return protocol.UIRequestResult{Cancelled: cancelled, Values: values}, nil
}

// RegisterActions records the UI actions one plugin declared in its
// handshake, replacing any previously registered set for that plugin. An
// invalid action ID rejects the whole batch: the registry is the allow-list
// for later invocations, so a malformed declaration must not slip through.
func (h *Hub) RegisterActions(pluginID string, actions []protocol.UIActionDecl) error {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return errors.New("uihub: plugin id is required")
	}
	for _, action := range actions {
		if !ValidActionID(action.ActionID) {
			return fmt.Errorf("uihub: extension %s declared invalid action id %q", pluginID, action.ActionID)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	decls := make(map[string]protocol.UIActionDecl, len(actions))
	for _, action := range actions {
		if _, dup := decls[action.ActionID]; dup {
			h.warnLocked("extension " + pluginID + " re-declared action " + action.ActionID + "; keeping the first declaration")
			continue
		}
		decls[action.ActionID] = protocol.UIActionDecl{
			ActionID: action.ActionID,
			Label:    secrets.RedactCredentials(action.Label),
		}
	}
	h.actions[pluginID] = decls
	h.known[pluginID] = true
	return nil
}

// Actions returns every registered action ordered by its public slash name.
func (h *Hub) Actions() []ActionView {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []ActionView
	for pluginID, decls := range h.actions {
		for actionID, decl := range decls {
			out = append(out, ActionView{
				PluginID: pluginID,
				ActionID: actionID,
				Label:    decl.Label,
				Slash:    SlashName(pluginID, actionID),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slash < out[j].Slash })
	return out
}

// InvokeAction routes one /<plugin>:<action> invocation to the owning sidecar
// as extension/ui/action. The action must be handshake-declared, the plugin's
// sidecar live, and the session the hub's current one; the result message is
// credential-redacted before it travels back to the frontend.
func (h *Hub) InvokeAction(ctx context.Context, pluginID, actionID, sessionID string, args map[string]string) (protocol.UIActionResult, error) {
	if !ValidActionID(actionID) {
		return protocol.UIActionResult{}, &protocol.ProtocolError{
			Reason:  protocol.ErrInvalidParams,
			Message: fmt.Sprintf("invalid action id %q", actionID),
		}
	}
	client, generation, err := h.actionTarget(pluginID, sessionID)
	if err != nil {
		return protocol.UIActionResult{}, err
	}
	h.mu.Lock()
	_, declared := h.actions[pluginID][actionID]
	h.mu.Unlock()
	if !declared {
		return protocol.UIActionResult{}, &protocol.ProtocolError{
			Reason:  protocol.ErrInvalidParams,
			Message: "extension " + pluginID + " declared no action " + actionID,
		}
	}
	result, err := client.UIAction(ctx, protocol.UIActionParams{
		ActionID:   actionID,
		SessionID:  sessionID,
		Generation: generation,
		Args:       args,
	})
	if err != nil {
		return protocol.UIActionResult{}, err
	}
	result.Message = secrets.RedactCredentials(result.Message)
	return result, nil
}

// Submit routes one form surface's values back to the owning sidecar as
// extension/ui/submit.
func (h *Hub) Submit(ctx context.Context, pluginID, surfaceID, sessionID string, values map[string]any) (protocol.UISubmitResult, error) {
	if strings.TrimSpace(surfaceID) == "" {
		return protocol.UISubmitResult{}, &protocol.ProtocolError{
			Reason:  protocol.ErrInvalidParams,
			Message: "surface id is required",
		}
	}
	client, generation, err := h.actionTarget(pluginID, sessionID)
	if err != nil {
		return protocol.UISubmitResult{}, err
	}
	return client.UISubmit(ctx, protocol.UISubmitParams{
		SurfaceID:  surfaceID,
		SessionID:  sessionID,
		Generation: generation,
		Values:     values,
	})
}

// actionTarget resolves the live client for one plugin after enforcing the
// binding and session rules shared by InvokeAction and Submit.
func (h *Hub) actionTarget(pluginID, sessionID string) (ActionClient, uint64, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.known[pluginID] {
		return nil, 0, unknownClientError(pluginID)
	}
	if h.crashed[pluginID] {
		return nil, 0, &protocol.ProtocolError{
			Reason:  protocol.ErrProviderInterrupted,
			Message: "extension sidecar " + pluginID + " crashed",
		}
	}
	if sessionID != h.sessionID {
		return nil, 0, &protocol.ProtocolError{
			Reason:  protocol.ErrInvalidParams,
			Message: "extension UI call for a stale session",
		}
	}
	if h.resolve == nil {
		return nil, 0, &protocol.ProtocolError{
			Reason:  protocol.ErrUnknownMethod,
			Message: "extension UI is not available on this host",
		}
	}
	client := h.resolve(pluginID)
	if client == nil {
		return nil, 0, &protocol.ProtocolError{
			Reason:  protocol.ErrProviderInterrupted,
			Message: "extension " + pluginID + " has no live sidecar",
		}
	}
	return client, h.generation, nil
}

// emitEvent fans one extension event out to the frontend sink.
func (h *Hub) emitEvent(ev event.Event) {
	h.mu.Lock()
	emit := h.emit
	h.mu.Unlock()
	if emit == nil {
		slog.Debug("uihub: dropping extension event (no emit sink)", "kind", ev.Kind)
		return
	}
	emit(ev)
}

// warnLocked reports a non-fatal anomaly; the caller holds h.mu.
func (h *Hub) warnLocked(msg string) {
	if h.warn != nil {
		h.warn(msg)
		return
	}
	slog.Warn("uihub: " + msg)
}

// decodePublishEvent strict-decodes one publish payload by kind, redacts
// every user-visible string, and builds the event payload.
func decodePublishEvent(pluginID string, p protocol.UIPublishParams) (*event.ExtensionSurfacePayload, error) {
	decoded, err := protocol.DecodeUIPublishPayload(p.Kind, p.Payload)
	if err != nil {
		return nil, &protocol.ProtocolError{
			Reason:  protocol.ErrInvalidParams,
			Message: "invalid " + string(p.Kind) + " surface payload: " + err.Error(),
		}
	}
	out := &event.ExtensionSurfacePayload{
		PluginID:   pluginID,
		SurfaceID:  p.SurfaceID,
		SessionID:  p.SessionID,
		Generation: p.Generation,
		Kind:       string(p.Kind),
	}
	switch payload := decoded.(type) {
	case protocol.UIStatusPayload:
		out.Status = &event.ExtensionStatusView{
			Label:    secrets.RedactCredentials(payload.Label),
			Detail:   secrets.RedactCredentials(payload.Detail),
			Severity: string(payload.Severity),
			Progress: payload.Progress,
		}
	case protocol.UICardPayload:
		card := &event.ExtensionCardView{
			Title:    secrets.RedactCredentials(payload.Title),
			Markdown: secrets.RedactCredentials(payload.Markdown),
			Text:     secrets.RedactCredentials(payload.Text),
			Progress: payload.Progress,
		}
		for _, field := range payload.Fields {
			card.Fields = append(card.Fields, event.ExtensionKeyValue{
				Key:   secrets.RedactCredentials(field.Key),
				Value: secrets.RedactCredentials(field.Value),
			})
		}
		for _, action := range payload.Actions {
			card.Actions = append(card.Actions, event.ExtensionActionRef{
				ActionID: action.ActionID,
				Label:    secrets.RedactCredentials(action.Label),
			})
		}
		out.Card = card
	case protocol.UIFormPayload:
		out.Form = &event.ExtensionFormView{
			Title:   secrets.RedactCredentials(payload.Title),
			Message: secrets.RedactCredentials(payload.Message),
			Fields:  redactEventFormFields(payload.Fields),
		}
	case protocol.UINotificationPayload:
		out.Notification = &event.ExtensionNotificationView{
			Title:    secrets.RedactCredentials(payload.Title),
			Body:     secrets.RedactCredentials(payload.Body),
			Severity: string(payload.Severity),
		}
	}
	return out, nil
}

// redactFormFields redacts the user-visible strings of protocol form fields
// (labels and options; keys stay intact — they correlate answers back).
func redactFormFields(fields []protocol.UIFormField) []protocol.UIFormField {
	out := make([]protocol.UIFormField, len(fields))
	for i, field := range fields {
		out[i] = protocol.UIFormField{
			Key:      field.Key,
			Label:    secrets.RedactCredentials(field.Label),
			Kind:     field.Kind,
			Options:  redactStrings(field.Options),
			Default:  redactDefault(field.Default),
			Required: field.Required,
		}
	}
	return out
}

// redactEventFormFields redacts protocol form fields into their event view.
func redactEventFormFields(fields []protocol.UIFormField) []event.ExtensionFormField {
	out := make([]event.ExtensionFormField, 0, len(fields))
	for _, field := range fields {
		out = append(out, event.ExtensionFormField{
			Key:      field.Key,
			Label:    secrets.RedactCredentials(field.Label),
			Kind:     string(field.Kind),
			Options:  redactStrings(field.Options),
			Default:  redactDefault(field.Default),
			Required: field.Required,
		})
	}
	return out
}

func redactStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = secrets.RedactCredentials(s)
	}
	return out
}

// redactDefault redacts a string default value; non-string defaults carry no
// user-visible text.
func redactDefault(v any) any {
	if s, ok := v.(string); ok {
		return secrets.RedactCredentials(s)
	}
	return v
}

func unknownClientError(pluginID string) error {
	return &protocol.ProtocolError{
		Reason:  protocol.ErrInternal,
		Message: "host/ui call from an unknown extension client " + fmt.Sprintf("%q", pluginID),
	}
}

// actionIDPattern freezes the public action ID contract: lowercase, digits,
// and dashes only, so actions compose cleanly into /<plugin>:<action> names.
var actionIDPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// ValidActionID reports whether id is a well-formed extension action ID.
func ValidActionID(id string) bool {
	return actionIDPattern.MatchString(id)
}

// SlashName renders the public invocation name of one action,
// "/<plugin>:<action>".
func SlashName(pluginID, actionID string) string {
	return "/" + pluginID + ":" + actionID
}

// ParseSlashName splits a public invocation name back into plugin and action
// IDs. ok is false when the name is not a well-formed "/<plugin>:<action>".
func ParseSlashName(name string) (pluginID, actionID string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(name), "/")
	if !found {
		return "", "", false
	}
	plugin, action, found := strings.Cut(rest, ":")
	if !found || strings.TrimSpace(plugin) == "" || !ValidActionID(action) {
		return "", "", false
	}
	return plugin, action, true
}

// Confirmation option labels. They are display strings; the boolean answer
// maps back from the picked label.
const (
	confirmYes = "Yes"
	confirmNo  = "No"
)

// AskRequestFunc adapts the host's Ask channel (agent.Asker-shaped, e.g.
// control.Controller.Ask) to the hub's RequestFunc. Form fields translate
// one-to-one into AskQuestions; answers map back to values keyed by field
// key — a string for input/select/confirm-free text, a bool for confirm, a
// string slice for multiselect. A prompt dismissed with no selections at all
// reports cancelled, mirroring the controller's own skip semantics.
func AskRequestFunc(ask func(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error)) RequestFunc {
	return func(ctx context.Context, req HubRequest) (map[string]any, bool, error) {
		if ask == nil {
			return nil, false, &protocol.ProtocolError{
				Reason:  protocol.ErrUnknownMethod,
				Message: "extension UI is not available on this host",
			}
		}
		fields := req.Fields
		if len(fields) == 0 {
			// A field-less request (the common confirm shape) asks one
			// question of the request kind's matching field kind.
			fields = []protocol.UIFormField{{
				Key:   "value",
				Label: req.Message,
				Kind:  protocol.UIFieldKind(req.Kind),
			}}
		}
		questions := make([]event.AskQuestion, 0, len(fields))
		for _, field := range fields {
			question := event.AskQuestion{ID: field.Key, Header: field.Label, Prompt: field.Label}
			if question.Prompt == "" {
				question.Prompt = req.Message
			}
			if question.Header == "" {
				question.Header = req.Title
			}
			switch field.Kind {
			case protocol.UIFieldConfirm:
				question.Options = []event.AskOption{{Label: confirmYes}, {Label: confirmNo}}
			case protocol.UIFieldSelect:
				question.Options = askOptions(field.Options)
			case protocol.UIFieldMultiselect:
				question.Options = askOptions(field.Options)
				question.Multi = true
			default:
				// input (and anything unrecognised) is a free-text question.
			}
			questions = append(questions, question)
		}
		answers, err := ask(ctx, questions)
		if err != nil {
			return nil, false, err
		}
		byID := make(map[string]event.AskAnswer, len(answers))
		for _, answer := range answers {
			byID[answer.QuestionID] = answer
		}
		values := map[string]any{}
		selections := 0
		for _, field := range fields {
			answer, ok := byID[field.Key]
			if !ok || len(answer.Selected) == 0 {
				continue
			}
			selections += len(answer.Selected)
			switch field.Kind {
			case protocol.UIFieldConfirm:
				values[field.Key] = strings.EqualFold(answer.Selected[0], confirmYes)
			case protocol.UIFieldMultiselect:
				values[field.Key] = append([]string(nil), answer.Selected...)
			default:
				values[field.Key] = answer.Selected[0]
			}
		}
		if selections == 0 {
			return nil, true, nil
		}
		return values, false, nil
	}
}

func askOptions(options []string) []event.AskOption {
	out := make([]event.AskOption, len(options))
	for i, option := range options {
		out[i] = event.AskOption{Label: option}
	}
	return out
}
