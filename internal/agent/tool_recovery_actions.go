package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func (a *Agent) recoveryCall(attempt string) (provider.ToolCall, error) {
	for _, m := range a.Session().Snapshot() {
		for _, call := range m.ToolCalls {
			if call.Recovery != nil && call.Recovery.Identity.AttemptID == attempt {
				return call, nil
			}
		}
	}
	return provider.ToolCall{}, fmt.Errorf("recovery action is stale or unavailable")
}

func (a *Agent) InspectToolRecovery(ctx context.Context, attempt string) (provider.ToolCallRecord, error) {
	call, err := a.recoveryCall(attempt)
	if err != nil {
		return provider.ToolCallRecord{}, err
	}
	r := *call.Recovery
	if !unresolvedToolRecord(r) {
		return r, fmt.Errorf("tool recovery is already resolved")
	}
	t := a.recoveryInspectionTarget(ctx, call)
	state := "unknown"
	if t != nil {
		if verifier, ok := t.(tool.EffectVerifier); ok {
			if verifier.RecoveryScope() == "" || verifier.RecoveryScope() != r.Identity.ResourceScope {
				return r, fmt.Errorf("tool recovery sink identity changed")
			}
			inspection, err := verifier.InspectEffect(ctx, r.IdempotencyKey, r.Arguments)
			if err != nil {
				return r, err
			}
			if inspection.State == "present" {
				state = "present"
			} else if inspection.State == "absent" && inspection.Fenced {
				state = "absent_fenced"
			}
		} else if _, satisfied := checkRecordedWrite(ctx, t, call); satisfied {
			state = "postcondition_satisfied"
		}
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return r, err
	}
	r.InspectionID = hex.EncodeToString(id[:])
	r.InspectionState = state
	if !a.Session().setToolRecoveryRecord(call.ID, r) {
		return r, fmt.Errorf("recovery attempt changed")
	}
	if err := event.EmitChecked(a.svc.sink, event.Event{Kind: event.Notice, RecoveryCheckpoint: true}); err != nil {
		a.Session().setToolRecoveryRecord(call.ID, *call.Recovery)
		return r, err
	}
	return r, nil
}

func (a *Agent) ResolveToolRecovery(attempt, inspection, action string) error {
	call, err := a.recoveryCall(attempt)
	if err != nil {
		return err
	}
	r := *call.Recovery
	if !unresolvedToolRecord(r) {
		return fmt.Errorf("tool recovery is already resolved")
	}
	if inspection == "" || r.InspectionID != inspection {
		return fmt.Errorf("inspect this exact attempt before resolving it")
	}
	switch action {
	case "confirm":
		r.State = provider.ToolRunUserConfirmed
	case "reject": // Rejecting a retry does not prove the external effect absent.
	default:
		return fmt.Errorf("unsupported recovery action")
	}
	r.Resolution = action
	r.ResolvedAt = time.Now().UnixMilli()
	r.ResolutionSource = "user"
	if !a.Session().setToolRecoveryRecord(call.ID, r) {
		return fmt.Errorf("recovery attempt changed")
	}
	if err := event.EmitChecked(a.svc.sink, event.Event{Kind: event.Notice, RecoveryCheckpoint: true}); err != nil {
		a.Session().setToolRecoveryRecord(call.ID, *call.Recovery)
		return err
	}
	return nil
}

type recoveryRetryKey struct{}

// RetryToolRecovery executes only stored arguments through the ordinary
// parse/policy/lease/hook pipeline, with a fresh call and attempt identity.
func (a *Agent) RetryToolRecovery(ctx context.Context, attempt, inspection string) error {
	call, err := a.recoveryCall(attempt)
	if err != nil {
		return err
	}
	r := *call.Recovery
	if !unresolvedToolRecord(r) || inspection == "" || inspection != r.InspectionID {
		return fmt.Errorf("inspect the current unresolved attempt before retry")
	}
	if a.stragglers.live.Load() != 0 {
		return fmt.Errorf("the previous executor is still running")
	}
	t := a.recoveryInspectionTarget(ctx, call)
	if t == nil {
		return fmt.Errorf("tool target unavailable")
	}
	if !r.ReadOnly || !t.ReadOnly() {
		verifier, ok := t.(tool.EffectVerifier)
		if !ok {
			return fmt.Errorf("tool cannot prove a fenced absent effect; retry refused")
		}
		if verifier.RecoveryScope() == "" || verifier.RecoveryScope() != r.Identity.ResourceScope {
			return fmt.Errorf("tool recovery sink identity changed")
		}
		state, err := verifier.InspectEffect(ctx, r.IdempotencyKey, r.Arguments)
		if err != nil {
			return err
		}
		if state.State != "absent" || !state.Fenced {
			return fmt.Errorf("previous effect is not proven absent and fenced")
		}
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	call.ID = "retry_" + hex.EncodeToString(id[:])
	call.Recovery = nil
	a.Session().Add(provider.Message{Role: provider.RoleAssistant, LocalOnly: true, ToolCalls: []provider.ToolCall{call}})
	ctx = context.WithValue(ctx, recoveryRetryKey{}, &r)
	out := a.executeOne(ctx, &a.turn, call)
	a.finishToolRecovery(call, out)
	a.Session().Add(provider.Message{Role: provider.RoleTool, LocalOnly: true, ToolCallID: call.ID, Name: call.Name, Content: out.output, ToolRunState: outcomeRunState(out)})
	if err := event.EmitChecked(a.svc.sink, event.Event{Kind: event.Notice, RecoveryCheckpoint: true}); err != nil {
		return err
	}
	if out.errMsg != "" {
		return fmt.Errorf("retry did not complete: %s", out.errMsg)
	}
	return nil
}

// Resolve a proxy without executing its commit callback. A renamed or
// unavailable target cannot supply evidence for the previous capability.
func (a *Agent) recoveryInspectionTarget(ctx context.Context, call provider.ToolCall) tool.Tool {
	t, _, ambiguous := a.svc.tools.ResolveCall(call.Name)
	if t == nil || len(ambiguous) != 0 {
		return nil
	}
	if resolver, ok := t.(tool.CallResolver); ok {
		r, err := resolver.ResolveCall(ctx, json.RawMessage(call.Arguments))
		if err != nil || r.Unavailable || r.Target == nil || call.Recovery == nil || r.TargetName != call.Recovery.Identity.CanonicalTool {
			return nil
		}
		t = r.Target
	}
	return t
}
