package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

var ErrToolRecoveryRequired = errors.New("recovery_required: an external tool effect has not been confirmed")

func recoveryDigest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

func (s *Session) toolRecoveryRecord(callID string) *provider.ToolCallRecord {
	for _, m := range slices.Backward(s.Snapshot()) {
		for _, c := range m.ToolCalls {
			if c.ID == callID && c.Recovery != nil {
				r := *c.Recovery
				r.Arguments = append(json.RawMessage(nil), r.Arguments...)
				return &r
			}
		}
	}
	return nil
}

// Metadata updates detach the call slice so concurrent snapshots remain immutable.
func (s *Session) setToolRecoveryRecord(id string, r provider.ToolCallRecord) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range slices.Backward(s.Messages) {
		for j, call := range s.Messages[i].ToolCalls {
			if call.ID != id {
				continue
			}
			if call.Recovery != nil && call.Recovery.Identity.AttemptID != r.Identity.AttemptID {
				return false
			}
			calls := append([]provider.ToolCall(nil), s.Messages[i].ToolCalls...)
			r.Arguments = append(json.RawMessage(nil), r.Arguments...)
			calls[j].Recovery = &r
			s.Messages[i].ToolCalls = calls
			s.version++
			s.recoveryMetadataVersion = s.version
			return true
		}
	}
	return false
}

func (a *Agent) beginToolRecovery(ctx context.Context, p *toolCallPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// An unresolved external effect survives subsequent user turns. Read-only
	// diagnosis remains available; new call IDs cannot bypass this barrier.
	prior, _ := ctx.Value(recoveryRetryKey{}).(*provider.ToolCallRecord)
	if !p.readOnly && slices.ContainsFunc(a.PendingToolRecovery(), func(r provider.ToolCallRecord) bool {
		return !r.ReadOnly && (prior == nil || prior.Identity.AttemptID != r.Identity.AttemptID)
	}) {
		return fmt.Errorf("recovery_required: inspect and resolve the previous uncertain tool effect before another write")
	}
	var params any
	decoder := json.NewDecoder(bytes.NewReader(p.permArgs))
	decoder.UseNumber()
	if err := decoder.Decode(&params); err != nil {
		return err
	}
	canonical, err := json.Marshal(params)
	if err != nil {
		return err
	}
	attempt := make([]byte, 16)
	if _, err := rand.Read(attempt); err != nil {
		return err
	}
	msgs := a.sess.conversation.Snapshot()
	identity := provider.ActionIdentity{CallID: p.call.ID, CanonicalTool: p.permName, ArgumentDigest: recoveryDigest(canonical), AttemptID: hex.EncodeToString(attempt)}
	if len(msgs) > 0 {
		identity.SessionID = msgs[0].ID
	}
	for _, m := range slices.Backward(msgs) {
		if IsUserAuthoredTurnMessage(m) {
			identity.TurnID = m.ID
			break
		}
	}
	if open, ok := a.sess.conversation.OpenTurn(); ok {
		identity.TurnID = open.TurnID
	}
	// Unknown resource scopes intentionally cover the session. No guessed path
	// or model-provided scope may weaken the unresolved-effect barrier.
	identity.ResourceScope = "session:" + identity.SessionID
	if verifier, ok := p.runTool.(tool.EffectVerifier); ok {
		identity.ResourceScope = verifier.RecoveryScope()
		if identity.ResourceScope == "" {
			return fmt.Errorf("tool recovery sink identity unavailable")
		}
	}
	keyInput := identity
	if !p.readOnly && confirmedRecoveryEffect(msgs, identity) {
		return fmt.Errorf("the user confirmed this exact effect already happened; no write was repeated")
	}
	keyInput.AttemptID = ""
	keyJSON, _ := json.Marshal(keyInput)
	r := provider.ToolCallRecord{Identity: identity, State: provider.ToolRunStarted, ReadOnly: p.readOnly, Arguments: append(json.RawMessage(nil), p.permArgs...), IdempotencyKey: recoveryDigest(keyJSON), StartedAt: time.Now().UnixMilli()}
	if prior != nil {
		if prior.Identity.CanonicalTool != identity.CanonicalTool || prior.Identity.ArgumentDigest != identity.ArgumentDigest || prior.Identity.ResourceScope != identity.ResourceScope {
			return fmt.Errorf("retry target changed during policy resolution")
		}
		r.IdempotencyKey = prior.IdempotencyKey
	}
	p.cctx = tool.WithRecoveryIdempotencyKey(p.cctx, r.IdempotencyKey)
	p.call.Recovery = &r
	if a.sess.conversation.setToolRecoveryRecord(p.call.ID, r) {
		if err := event.EmitChecked(a.svc.sink, event.Event{Kind: event.Notice, RecoveryCheckpoint: true}); err != nil {
			r.State = provider.ToolRunNotStarted
			a.sess.conversation.setToolRecoveryRecord(p.call.ID, r)
			return err
		}
	}
	if err := a.emitToolStarted(p.call); err != nil {
		r.State = provider.ToolRunNotStarted
		a.sess.conversation.setToolRecoveryRecord(p.call.ID, r)
		return err
	}
	if prior != nil {
		old := *prior
		old.SupersededBy = r.Identity.AttemptID
		if !a.Session().setToolRecoveryRecord(old.Identity.CallID, old) {
			return fmt.Errorf("original retry attempt changed")
		}
		if err := event.EmitChecked(a.svc.sink, event.Event{Kind: event.Notice, RecoveryCheckpoint: true}); err != nil {
			a.Session().setToolRecoveryRecord(prior.Identity.CallID, *prior)
			return err
		}
	}
	return nil
}

func (a *Agent) finishToolRecovery(call provider.ToolCall, out toolOutcome) {
	r := a.sess.conversation.toolRecoveryRecord(call.ID)
	if r == nil {
		return
	}
	r.State = outcomeRunState(out)
	if out.executed && out.errMsg != "" && r.State == provider.ToolRunCompleted {
		r.State = provider.ToolRunFailed
	}
	// An explicit tool error proves failure, not absence of partial effects.
	r.FinishedAt = time.Now().UnixMilli()
	r.ResultDigest = recoveryDigest([]byte(out.output))
	a.sess.conversation.setToolRecoveryRecord(call.ID, *r)
}

func unresolvedToolRecord(r provider.ToolCallRecord) bool {
	if r.SupersededBy != "" {
		return false
	}
	return r.State == provider.ToolRunStarted || r.State == provider.ToolRunRunning || r.State == provider.ToolRunUnknown || (r.State == provider.ToolRunFailed && !r.ReadOnly && r.EffectSummary == "effect_unknown")
}

// Rewriting model history cannot erase evidence of an unresolved external
// effect. A local-only receipt survives compaction/rewind on the same session.
func retainUnresolvedToolRecords(previous, next []provider.Message) []provider.Message {
	seen := map[string]bool{}
	for _, m := range next {
		for _, c := range m.ToolCalls {
			if c.Recovery != nil {
				seen[c.Recovery.Identity.AttemptID] = true
			}
		}
	}
	for _, m := range previous {
		for _, c := range m.ToolCalls {
			if c.Recovery == nil || c.Recovery.ReadOnly || (!unresolvedToolRecord(*c.Recovery) && c.Recovery.State != provider.ToolRunUserConfirmed) || seen[c.Recovery.Identity.AttemptID] {
				continue
			}
			seen[c.Recovery.Identity.AttemptID] = true
			next = append(append([]provider.Message(nil), next...), provider.Message{Role: provider.RoleTool, LocalOnly: true, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName, ToolCalls: []provider.ToolCall{c}})
		}
	}
	return next
}

// PendingToolRecovery reads durable execution facts, independent of prompt-tail
// consumption. Records are detached before leaving the session boundary.
func (a *Agent) PendingToolRecovery() []provider.ToolCallRecord {
	result := []provider.ToolCallRecord{}
	if a == nil || a.sess.conversation == nil {
		return result
	}
	seen := map[string]bool{}
	for _, m := range slices.Backward(a.sess.conversation.Snapshot()) {
		for _, call := range m.ToolCalls {
			if call.Recovery == nil {
				continue
			}
			r := *call.Recovery
			id := r.Identity.AttemptID
			if seen[id] {
				continue
			}
			seen[id] = true
			if !unresolvedToolRecord(r) {
				continue
			}
			r.Arguments = append(json.RawMessage(nil), r.Arguments...)
			result = append(result, r)
		}
	}
	return result
}
