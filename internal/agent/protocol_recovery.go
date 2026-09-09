package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

var ErrProtocolRecoveryUnavailable = errors.New("protocol recovery is unavailable or stale")

type protocolRecoveryKey struct{}
type protocolRecoveryRequest struct {
	id              string
	record          provider.ProtocolRecoveryRecord
	ready, consumed bool
}

func WithProtocolRecovery(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, protocolRecoveryKey{}, &protocolRecoveryRequest{id: id})
}

func protocolHistory(messages []provider.Message) []provider.Message {
	// Host continuation text is not a new user task. Keep original human
	// boundaries when validating a recovery after a pre-request admission error.
	out := make([]provider.Message, 0, len(messages))
	for _, m := range messages {
		if m.Role == provider.RoleUser && m.Origin == provider.MessageOriginHost {
			continue
		}
		out = append(out, m)
	}
	out = append([]provider.Message(nil), provider.ModelMessages(out)...)
	for i := range out {
		out[i].CreatedAt = 0
		out[i].WorkDurationMs = 0
	}
	return out
}

// Evidence changes invalidate a pending action without renewing a consumed
// protocol budget or altering any provider-visible message bytes.
func protocolEvidenceDigest(messages []provider.Message) string {
	type receipt struct {
		ID, Name string
		State    provider.ToolRunState
	}
	var evidence []receipt
	for _, m := range messages {
		if !m.LocalOnly && m.Role == provider.RoleTool {
			evidence = append(evidence, receipt{m.ToolCallID, m.Name, provider.ToolResultRunState(m)})
		}
	}
	return protocolDigest(evidence)
}

func protocolDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (a *Agent) protocolRecoveryScope() string {
	return protocolDigest([]string{a.SessionPath(), provider.MissingToolCallReasoningWarningFingerprint(a.svc.prov)})
}

func (a *Agent) latestProtocolRecord() (provider.ProtocolRecoveryRecord, bool) {
	if a == nil || a.Session() == nil {
		return provider.ProtocolRecoveryRecord{}, false
	}
	for _, m := range slices.Backward(a.Session().Snapshot()) {
		if len(m.ProtocolRecovery) > 0 {
			return provider.DecodeProtocolRecovery(m.ProtocolRecovery)
		}
	}
	return provider.ProtocolRecoveryRecord{}, false
}

// PendingProtocolRecovery is read-only and safe for idle UI/history queries.
func (a *Agent) PendingProtocolRecovery() *provider.ProtocolRecoveryAction {
	r, ok := a.latestProtocolRecord()
	if !ok || r.State != "pending" || r.Scope != a.protocolRecoveryScope() {
		return nil
	}
	snapshot := a.Session().Snapshot()
	if protocolDigest(protocolHistory(snapshot)) != r.Fingerprint || (r.Evidence != "" && protocolEvidenceDigest(snapshot) != r.Evidence) {
		return nil
	}
	return &provider.ProtocolRecoveryAction{ID: r.ID}
}

func (a *Agent) prepareProtocolRecovery(ctx context.Context) error {
	request, _ := ctx.Value(protocolRecoveryKey{}).(*protocolRecoveryRequest)
	if request == nil {
		return nil
	}
	action := a.PendingProtocolRecovery()
	if action == nil || request.id != action.ID || ctx.Err() != nil {
		return ErrProtocolRecoveryUnavailable
	}
	r, _ := a.latestProtocolRecord()
	request.record, request.ready = r, true
	return nil
}

func (a *Agent) restoreProtocolProjection() {
	r, ok := a.latestProtocolRecord()
	if !ok || !r.Projected || r.State != "consumed" || r.Scope != a.protocolRecoveryScope() {
		return
	}
	messages := protocolHistory(a.Session().Snapshot())
	if r.Count <= 0 || r.Count > len(messages) || protocolDigest(messages[:r.Count]) != r.Fingerprint {
		return
	}
	a.sess.reasoningReplayStrongProjection = r.Prefix
	a.sess.reasoningReplayStrongProjectionAnchor = r.Anchor
}

func (a *Agent) protocolRecoverySpent() bool {
	for _, m := range slices.Backward(a.Session().Snapshot()) {
		if len(m.ProtocolRecovery) == 0 {
			continue
		}
		var version struct {
			Version int `json:"version"`
		}
		if json.Unmarshal(m.ProtocolRecovery, &version) != nil || version.Version != 1 {
			return true
		}
		break
	}
	r, ok := a.latestProtocolRecord()
	if !ok || r.State != "consumed" || r.Scope != a.protocolRecoveryScope() {
		return false
	}
	messages := protocolHistory(a.Session().Snapshot())
	if r.Count <= 0 || r.Count > len(messages) || protocolDigest(messages[:r.Count]) != r.Fingerprint {
		return false
	}
	// A fresh assistant/tool round can introduce a genuinely different failure.
	for _, m := range messages[r.Count:] {
		if m.Role == provider.RoleAssistant && (len(m.ToolCalls) > 0 || len(m.ThinkingBlocks) > 0 || len(m.ResponsesItems) > 0 || m.ReasoningContent != "") {
			return false
		}
	}
	return true
}

func (a *Agent) saveProtocolRecord(r provider.ProtocolRecoveryRecord) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	a.Session().storeProtocolRecord(r.ID, raw)
	return event.EmitChecked(a.svc.sink, event.Event{Kind: event.Notice, RecoveryCheckpoint: true})
}

func (a *Agent) protocolRecord(frozen samplingRequest, state string) provider.ProtocolRecoveryRecord {
	canonical := protocolHistory(a.Session().Snapshot())
	prefix := len(frozen.req.Messages)
	anchor := ""
	if prefix > 0 {
		anchor = reasoningReplayMessageFingerprint(frozen.req.Messages[prefix-1])
	}
	return provider.ProtocolRecoveryRecord{Evidence: protocolEvidenceDigest(a.Session().Snapshot()), Version: 1, ID: rand.Text(), State: state, Scope: a.protocolRecoveryScope(), Fingerprint: protocolDigest(canonical), Count: len(canonical), Prefix: prefix, Anchor: anchor, Run: a.recovery.runSeq.Load()}
}

func (a *Agent) offerProtocolRecovery(frozen samplingRequest, err error) error {
	if !provider.IsOpaqueBadRequest(err) || a.protocolRecoverySpent() {
		return nil
	}
	if _, changed := provider.ProjectReasoningStrippedMessages(a.svc.prov, frozen.req.Messages); !changed {
		return nil
	}
	if len(frozen.req.Messages) == 0 {
		return nil
	}
	return a.saveProtocolRecord(a.protocolRecord(frozen, "pending"))
}

// consumeManualProtocolRecovery runs after request preparation and immediately
// before any provider invocation. Failure to checkpoint prevents the request.
func (a *Agent) consumeManualProtocolRecovery(ctx context.Context, s *samplingRecoveryState) error {
	r, _ := ctx.Value(protocolRecoveryKey{}).(*protocolRecoveryRequest)
	if r == nil || r.consumed {
		return nil
	}
	if !r.ready || ctx.Err() != nil {
		return ErrProtocolRecoveryUnavailable
	}
	prefix := resolveReasoningReplayPrefix(s.frozen.req.Messages, r.record.Prefix, r.record.Anchor)
	if prefix <= 0 {
		return ErrProtocolRecoveryUnavailable
	}
	projected, changed := provider.ProjectReasoningStrippedMessagesPrefix(a.svc.prov, s.frozen.req.Messages, prefix)
	if !changed {
		return ErrProtocolRecoveryUnavailable
	}
	previous := s.frozen.req.Messages
	s.frozen.req.Messages = a.replayRecoveryFacts(previous[:prefix], projected)
	if err := a.applyAdmissionToRequest(&s.frozen.req); err != nil {
		return err
	}
	r.record.State = "consumed"
	r.record.Projected = true
	if err := a.saveProtocolRecord(r.record); err != nil {
		return fmt.Errorf("checkpoint protocol recovery: %w", err)
	}
	r.consumed = true
	s.protocol = true
	s.replay = reasoningReplayRecoveryBudget{retries: 1, cutoff: prefix, anchor: r.record.Anchor, persisted: true}
	// Keep the repaired view for subsequent requests even if generation fails.
	a.sess.reasoningReplayStrongProjection = prefix
	a.sess.reasoningReplayStrongProjectionAnchor = r.record.Anchor
	return nil
}

func (s *Session) storeProtocolRecord(id string, raw json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range slices.Backward(s.Messages) {
		if record, ok := provider.DecodeProtocolRecovery(s.Messages[i].ProtocolRecovery); ok && record.ID == id {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(s.Messages[i].ProtocolRecovery, &fields)
			var changes map[string]json.RawMessage
			_ = json.Unmarshal(raw, &changes)
			maps.Copy(fields, changes)
			merged, _ := json.Marshal(fields)
			s.Messages[i].ProtocolRecovery = merged
			s.version++
			s.rewriteVersion++
			return
		}
	}
	s.Messages = append(s.Messages, provider.Message{Role: provider.RoleTool, Name: provider.LocalOnlyToolName, ToolCallID: provider.LocalOnlyToolID, LocalOnly: true, ProtocolRecovery: append(json.RawMessage(nil), raw...)})
	s.version++
}

// New authored input invalidates a pending button. Consumed repair accounting
// remains intact, so sending "continue" cannot renew the same incident budget.
func (s *Session) expireProtocolRecoveryLocked(added []provider.Message) {
	if !slices.ContainsFunc(added, IsUserAuthoredTurnMessage) {
		return
	}
	for i := range slices.Backward(s.Messages) {
		raw := s.Messages[i].ProtocolRecovery
		if len(raw) == 0 {
			continue
		}
		r, ok := provider.DecodeProtocolRecovery(raw)
		if ok && r.State == "pending" {
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) == nil {
				fields["state"] = json.RawMessage(`"expired"`)
				s.Messages[i].ProtocolRecovery, _ = json.Marshal(fields)
				s.rewriteVersion++
			}
		}
		return
	}
}
