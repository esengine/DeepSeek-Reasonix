// Package dispatch is the host-side interceptor dispatcher for Extension
// Protocol v2 (stage 6a). It walks the kernel's frozen interceptor chain in
// order, applies each extension's ruling (continue / block / replace, plus
// allow / deny at permission.decision only), and runs the single-owner
// strategy replacements for the system_prompt, context, compaction, and
// session_policy slots.
//
// Error policy is per extension: a required runtime (manifest required:true)
// or any replacement-slot owner fails the current operation when its call
// errors or times out; an optional observation-only extension is warned about
// once per process and skipped for that call. A crashed sidecar fails fast at
// the client layer before the dispatcher ever sees the call.
//
// The dispatcher reads only the frozen chain handed to New — per-turn dynamic
// data (payloads, results) never flows back into it, so one Dispatcher serves
// concurrent turns safely for the life of its snapshot generation.
package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"
	"time"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/secrets"
)

// Client is the subset of sidecar.Client the dispatcher needs, abstracted for
// testability. *sidecar.Client satisfies it directly. The timeout argument is
// passed through; zero lets the sidecar resolve its per-point budget
// (sidecar.Client.TimeoutFor).
type Client interface {
	Intercept(ctx context.Context, event protocol.InterceptEvent, payload json.RawMessage, timeout time.Duration) (protocol.InterceptResult, error)
	TryNotifyEvent(event protocol.InterceptEvent, payload json.RawMessage) error
}

// Options configures a Dispatcher.
type Options struct {
	// Warn receives human-readable warnings about optional-extension failures
	// and dropped event notifications. Nil means warnings are discarded.
	Warn func(msg string)
}

func (o Options) warnFunc() func(string) {
	if o.Warn != nil {
		return o.Warn
	}
	return func(string) {}
}

// Result reports the outcome of one Intercept walk.
type Result struct {
	// Blocked is true when an extension stopped the operation with a reason.
	Blocked bool
	// BlockReason is the extension's user-visible reason, credential-redacted.
	BlockReason string
	// Permission carries the terminal allow/deny ruling at permission.decision;
	// nil means every interceptor continued and the host decision stands. The
	// CALLER combines it with the host verdict (host deny + extension allow →
	// allow for full-trust extensions).
	Permission *bool
	// Applied lists the plugin IDs that replaced the payload, in chain order.
	Applied []string
	// Audit holds redacted audit notes, e.g. an extension allow overriding a
	// host deny at permission.decision.
	Audit []string
}

// ViolationError reports an extension answer that breaks the dispatch rules:
// allow/deny outside permission.decision, an unknown decision, or a
// replacement that fails strict decoding against the point's DTO. The Detail
// is credential-redacted.
type ViolationError struct {
	Plugin string
	Point  extension.InterceptorPoint
	Detail string
}

// Error returns the redacted violation description.
func (e *ViolationError) Error() string {
	return fmt.Sprintf("extension %s violated the intercept contract at %s: %s", e.Plugin, e.Point, e.Detail)
}

// FailureError reports a required extension's call failure (timeout, crash,
// transport). The message is credential-redacted; Unwrap returns the original
// error so errors.As still finds *protocol.ProtocolError and its frozen
// reason.
type FailureError struct {
	Plugin string
	Point  extension.InterceptorPoint
	Err    error
}

// Error returns the redacted failure description.
func (e *FailureError) Error() string {
	return fmt.Sprintf("extension %s failed at %s: %s", e.Plugin, e.Point, secrets.RedactCredentials(e.Err.Error()))
}

// Unwrap returns the original call error.
func (e *FailureError) Unwrap() error { return e.Err }

// BlockError reports a strategy owner blocking the operation. The Reason is
// credential-redacted.
type BlockError struct {
	Plugin string
	Point  extension.InterceptorPoint
	Reason string
}

// Error returns the redacted block description.
func (e *BlockError) Error() string {
	return fmt.Sprintf("extension %s blocked %s: %s", e.Plugin, e.Point, e.Reason)
}

// Dispatcher applies the frozen interceptor chain to live payloads. It is
// immutable after New — every map and slice is deep-copied at construction —
// so concurrent turns may dispatch through one Dispatcher without locking.
// The single exception is the warn-once dedup set, guarded by warnedMu.
type Dispatcher struct {
	chain        map[extension.InterceptorPoint][]extension.Contribution
	replacements map[extension.Slot]extension.ContributionSource
	clients      func(pluginID string) Client
	required     map[string]bool
	slotOwners   map[string]bool
	warn         func(string)

	warnedMu sync.Mutex
	warned   map[string]struct{}
}

// New freezes the dispatch inputs into a Dispatcher. chain is the snapshot's
// kernel-sorted InterceptorChain (priority ascending, plugin ID, registration
// order); the dispatcher walks it exactly as given. replacements is the
// snapshot's Replacements map. clients resolves a plugin ID to its sidecar
// client (nil means no live sidecar; it must return an untyped nil). required
// marks plugins whose manifest declared required:true. opts.Warn defaults to
// a no-op.
func New(chain map[extension.InterceptorPoint][]extension.Contribution, replacements map[extension.Slot]extension.ContributionSource, clients func(pluginID string) Client, required map[string]bool, opts Options) *Dispatcher {
	frozenChain := make(map[extension.InterceptorPoint][]extension.Contribution, len(chain))
	for point, contribs := range chain {
		frozenChain[point] = slices.Clone(contribs)
	}
	frozenRequired := make(map[string]bool, len(required))
	maps.Copy(frozenRequired, required)
	slotOwners := make(map[string]bool, len(replacements))
	for _, owner := range replacements {
		if owner.PluginID != "" {
			slotOwners[owner.PluginID] = true
		}
	}
	return &Dispatcher{
		chain:        frozenChain,
		replacements: maps.Clone(replacements),
		clients:      clients,
		required:     frozenRequired,
		slotOwners:   slotOwners,
		warn:         opts.warnFunc(),
		warned:       map[string]struct{}{},
	}
}

// Intercept walks the chain for point in frozen order, calling each
// plugin-backed interceptor with the current (possibly already replaced)
// payload. payloadPtr must be a pointer to the point's registered DTO; on
// return it holds the final value after any replace rulings. Interceptors
// whose contribution has no plugin ID are not sidecar-addressable and are
// skipped.
//
// Rulings: continue passes the payload through; block stops the operation and
// reports the redacted reason; replace substitutes the payload after strict
// re-decoding against the point's DTO; allow/deny are terminal at
// permission.decision and a protocol violation anywhere else. A required
// extension's call failure or contract violation fails the operation; an
// optional extension's is warned about once and skipped.
func (d *Dispatcher) Intercept(ctx context.Context, point extension.InterceptorPoint, payloadPtr any) (*Result, error) {
	if _, err := checkPayloadType(point, payloadPtr); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(payloadPtr)
	if err != nil {
		return nil, fmt.Errorf("dispatch: marshal %s payload: %w", point, err)
	}
	result := &Result{}
	// Capture the host verdict before the walk: a replace ruling rewrites the
	// payload, but the audit note must reflect the decision the HOST made.
	hostDecision := ""
	if permission, ok := payloadPtr.(*PermissionPayload); ok {
		hostDecision = permission.HostDecision
	}
	for _, contribution := range d.chain[point] {
		pluginID := contribution.Source.PluginID
		if pluginID == "" {
			continue
		}
		client := d.clients(pluginID)
		if client == nil {
			if err := d.failure(pluginID, point, errors.New("no live sidecar client")); err != nil {
				return nil, err
			}
			continue
		}
		answer, callErr := client.Intercept(ctx, protocol.InterceptEvent(point), raw, 0)
		if callErr != nil {
			if err := d.failure(pluginID, point, callErr); err != nil {
				return nil, err
			}
			continue
		}
		switch answer.Decision {
		case protocol.DecisionContinue:
			// Pass the current payload to the next interceptor unchanged.
		case protocol.DecisionBlock:
			result.Blocked = true
			result.BlockReason = secrets.RedactCredentials(answer.Reason)
			return result, nil
		case protocol.DecisionReplace:
			fresh, decodeErr := decodePayload(point, answer.Replacement)
			if decodeErr != nil {
				if err := d.violation(pluginID, point, decodeErr); err != nil {
					return nil, err
				}
				continue
			}
			if err := assignPayload(payloadPtr, fresh); err != nil {
				return nil, err
			}
			raw, err = json.Marshal(fresh)
			if err != nil {
				return nil, fmt.Errorf("dispatch: marshal %s replacement: %w", point, err)
			}
			result.Applied = append(result.Applied, pluginID)
		case protocol.DecisionAllow, protocol.DecisionDeny:
			if point != extension.PointPermissionDecision {
				err := fmt.Errorf("decision %q is only legal at %s", answer.Decision, extension.PointPermissionDecision)
				if err := d.violation(pluginID, point, err); err != nil {
					return nil, err
				}
				continue
			}
			allow := answer.Decision == protocol.DecisionAllow
			result.Permission = &allow
			if allow && hostDecision == "deny" {
				result.Audit = append(result.Audit, secrets.RedactCredentials(fmt.Sprintf(
					"extension %s allowed the tool overriding the host deny", pluginID)))
			}
			// The first allow/deny is terminal for the extension phase.
			return result, nil
		default:
			// Unreachable through a real sidecar — the protocol registry
			// rejects unknown decisions — but fakes and future peers can send
			// anything; treat it as a contract violation.
			err := fmt.Errorf("unknown decision %q", answer.Decision)
			if err := d.violation(pluginID, point, err); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

// Strategy returns the client of the replacement slot's owner, or false when
// the slot is unowned (the host default stands).
func (d *Dispatcher) Strategy(slot extension.Slot) (Client, bool) {
	owner, ok := d.replacements[slot]
	if !ok {
		return nil, false
	}
	return d.clients(owner.PluginID), true
}

// RunStrategy asks the slot's owner to rule on the payload at point and
// applies a replace ruling to payloadPtr (validated against the point's DTO,
// exactly like Intercept). The owner is a required-class extension by
// definition: a timeout, error, contract violation, or block is fatal to the
// current operation. An unowned slot is a no-op and keeps the host default.
func (d *Dispatcher) RunStrategy(ctx context.Context, slot extension.Slot, point extension.InterceptorPoint, payloadPtr any) error {
	if _, err := checkPayloadType(point, payloadPtr); err != nil {
		return err
	}
	owner, ok := d.replacements[slot]
	if !ok {
		return nil
	}
	client := d.clients(owner.PluginID)
	if client == nil {
		return &FailureError{Plugin: owner.PluginID, Point: point, Err: errors.New("no live sidecar client")}
	}
	raw, err := json.Marshal(payloadPtr)
	if err != nil {
		return fmt.Errorf("dispatch: marshal %s strategy payload: %w", point, err)
	}
	answer, callErr := client.Intercept(ctx, protocol.InterceptEvent(point), raw, 0)
	if callErr != nil {
		return &FailureError{Plugin: owner.PluginID, Point: point, Err: callErr}
	}
	switch answer.Decision {
	case protocol.DecisionContinue:
		return nil
	case protocol.DecisionReplace:
		fresh, decodeErr := decodePayload(point, answer.Replacement)
		if decodeErr != nil {
			return &ViolationError{Plugin: owner.PluginID, Point: point, Detail: secrets.RedactCredentials(decodeErr.Error())}
		}
		return assignPayload(payloadPtr, fresh)
	case protocol.DecisionBlock:
		return &BlockError{Plugin: owner.PluginID, Point: point, Reason: secrets.RedactCredentials(answer.Reason)}
	default:
		return &ViolationError{Plugin: owner.PluginID, Point: point, Detail: secrets.RedactCredentials(
			fmt.Sprintf("strategy ruling %q is not continue or replace", answer.Decision))}
	}
}

// Event broadcasts a fire-and-forget extension/event notification to every
// chain member at point plus the owners of the slots that observe that point
// (deduplicated by plugin). Delivery is a non-blocking bounded enqueue:
// failures and queue saturation are warned about once per plugin and never
// fail or stall the caller.
func (d *Dispatcher) Event(point extension.InterceptorPoint, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		d.warn(fmt.Sprintf("dispatch: dropping %s event: marshal: %v", point, err))
		return
	}
	seen := map[string]bool{}
	notify := func(pluginID string) {
		if pluginID == "" || seen[pluginID] {
			return
		}
		seen[pluginID] = true
		client := d.clients(pluginID)
		if client == nil {
			return
		}
		if err := client.TryNotifyEvent(protocol.InterceptEvent(point), raw); err != nil {
			d.warnOnce("event|"+pluginID, fmt.Sprintf(
				"extension %s dropped the %s event: %s", pluginID, point, secrets.RedactCredentials(err.Error())))
		}
	}
	for _, contribution := range d.chain[point] {
		notify(contribution.Source.PluginID)
	}
	for _, slot := range slotsForPoint(point) {
		if owner, ok := d.replacements[slot]; ok {
			notify(owner.PluginID)
		}
	}
}

// slotsForPoint maps a point to the replacement slots whose owners observe it
// for event broadcasts. Per-tool ("tool:<name>") and per-provider
// ("provider:<ref>") slots are addressed by strategy dispatch, not broadcast,
// so they are not mapped here.
func slotsForPoint(point extension.InterceptorPoint) []extension.Slot {
	switch point {
	case extension.PointSystemPromptBuild:
		return []extension.Slot{extension.SlotSystemPrompt}
	case extension.PointContextPrepare:
		return []extension.Slot{extension.SlotContext}
	case extension.PointProviderRequest:
		return []extension.Slot{extension.SlotProviderRequest}
	case extension.PointProviderResponse:
		return []extension.Slot{extension.SlotProviderResponse}
	case extension.PointCompactionPrepare, extension.PointCompactionComplete:
		return []extension.Slot{extension.SlotCompaction}
	case extension.PointSessionStart, extension.PointSessionEnd, extension.PointSessionLoad,
		extension.PointSessionSave, extension.PointSessionRotate:
		return []extension.Slot{extension.SlotSessionPolicy}
	case extension.PointPermissionDecision:
		return []extension.Slot{extension.SlotPermission}
	case extension.PointFrontendEvent:
		return []extension.Slot{extension.SlotFrontendEvents}
	default:
		return nil
	}
}

// isRequired reports whether the plugin is required-class: manifest
// required:true or the owner of any replacement slot. Required-class failures
// fail the operation; optional failures are warned about and skipped.
func (d *Dispatcher) isRequired(pluginID string) bool {
	return d.required[pluginID] || d.slotOwners[pluginID]
}

// failure applies the error policy for a failed intercept call. Required
// extensions fail the operation; optional extensions warn once and skip.
func (d *Dispatcher) failure(pluginID string, point extension.InterceptorPoint, err error) error {
	if d.isRequired(pluginID) {
		return &FailureError{Plugin: pluginID, Point: point, Err: err}
	}
	d.warnOnce("error|"+pluginID, fmt.Sprintf(
		"extension %s failed at %s; skipping this optional extension: %s",
		pluginID, point, secrets.RedactCredentials(err.Error())))
	return nil
}

// violation applies the error policy for an answer that breaks the dispatch
// rules. Required extensions fail the operation; optional extensions warn
// once and their ruling is skipped.
func (d *Dispatcher) violation(pluginID string, point extension.InterceptorPoint, detail error) error {
	violation := &ViolationError{Plugin: pluginID, Point: point, Detail: secrets.RedactCredentials(detail.Error())}
	if d.isRequired(pluginID) {
		return violation
	}
	d.warnOnce("violation|"+pluginID, violation.Error()+"; skipping this optional extension's ruling")
	return nil
}

// warnOnce delivers msg through Options.Warn at most once per key for the
// life of the process.
func (d *Dispatcher) warnOnce(key, msg string) {
	d.warnedMu.Lock()
	if _, dup := d.warned[key]; dup {
		d.warnedMu.Unlock()
		return
	}
	d.warned[key] = struct{}{}
	d.warnedMu.Unlock()
	d.warn(msg)
}

// checkPayloadType verifies payloadPtr is a pointer to exactly the DTO
// registered for point. A mismatch is a host programming error, not an
// extension failure, so it always returns an error.
func checkPayloadType(point extension.InterceptorPoint, payloadPtr any) (payloadFactory, error) {
	factory, ok := payloadRegistry[point]
	if !ok {
		return nil, fmt.Errorf("dispatch: no payload DTO registered for %s", point)
	}
	want := reflect.TypeOf(factory())
	if got := reflect.TypeOf(payloadPtr); got != want {
		return nil, fmt.Errorf("dispatch: %s payload must be %s, got %s", point, want, got)
	}
	return factory, nil
}

// assignPayload replaces the value payloadPtr points to with the freshly
// decoded replacement. Whole-value assignment keeps omitted (zero) fields in
// the replacement from leaking the previous value through.
func assignPayload(payloadPtr, fresh any) error {
	target := reflect.ValueOf(payloadPtr)
	source := reflect.ValueOf(fresh)
	if target.Kind() != reflect.Pointer || target.IsNil() || target.Type() != source.Type() {
		return fmt.Errorf("dispatch: cannot assign %T over %T", fresh, payloadPtr)
	}
	target.Elem().Set(source.Elem())
	return nil
}
