package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type recoverySink struct{ fail bool }

func (*recoverySink) Emit(event.Event) {}
func (s *recoverySink) EmitChecked(e event.Event) error {
	if s.fail && e.RecoveryCheckpoint {
		return errors.New("injected storage failure")
	}
	return nil
}

type idempotentRecoveryTool struct {
	readOnly   bool
	inspection tool.EffectInspection
	keys       []string
	effects    map[string]bool
}

func (*idempotentRecoveryTool) Name() string            { return "recovery_probe" }
func (*idempotentRecoveryTool) RecoveryScope() string   { return "fixture-sink" }
func (*idempotentRecoveryTool) Description() string     { return "fixture" }
func (*idempotentRecoveryTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t *idempotentRecoveryTool) ReadOnly() bool        { return t.readOnly }
func (t *idempotentRecoveryTool) InspectEffect(context.Context, string, json.RawMessage) (tool.EffectInspection, error) {
	return t.inspection, nil
}
func (t *idempotentRecoveryTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	key := tool.RecoveryIdempotencyKey(ctx)
	t.keys = append(t.keys, key)
	t.effects[key] = true
	return "observed", nil
}

func recoveryActionFixture(t *testing.T) (*Agent, *idempotentRecoveryTool, *recoverySink) {
	t.Helper()
	sink := &recoverySink{}
	probe := &idempotentRecoveryTool{effects: map[string]bool{}}
	reg := tool.NewRegistry()
	reg.Add(probe)
	r := provider.ToolCallRecord{Identity: provider.ActionIdentity{CallID: "old", AttemptID: "original", CanonicalTool: probe.Name(), ArgumentDigest: recoveryDigest([]byte(`{}`)), ResourceScope: probe.RecoveryScope()}, IdempotencyKey: "stable-effect-key", State: provider.ToolRunUnknown, Arguments: json.RawMessage(`{}`)}
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "write"})
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "old", Name: probe.Name(), Arguments: `{}`, Recovery: &r}}})
	return New(nil, reg, s, Options{}, sink), probe, sink
}

func TestToolRecoveryConfirmationRollsBackOnStorageFailure(t *testing.T) {
	a, _, sink := recoveryActionFixture(t)
	r, err := a.InspectToolRecovery(context.Background(), "original")
	if err != nil {
		t.Fatal(err)
	}
	sink.fail = true
	if err := a.ResolveToolRecovery("original", r.InspectionID, "confirm"); err == nil {
		t.Fatal("storage failure ignored")
	}
	if len(a.PendingToolRecovery()) != 1 {
		t.Fatal("unpersisted confirmation removed effect barrier")
	}
	sink.fail = false
	if err := a.ResolveToolRecovery("original", r.InspectionID, "confirm"); err != nil {
		t.Fatal(err)
	}
	if len(a.PendingToolRecovery()) != 0 {
		t.Fatal("confirmation not applied")
	}
	if err := a.ResolveToolRecovery("original", r.InspectionID, "confirm"); err == nil {
		t.Fatal("duplicate confirmation accepted")
	}
}

func TestToolRecoveryRetryRequiresFencedAbsenceAndKeepsIdempotency(t *testing.T) {
	a, probe, _ := recoveryActionFixture(t)
	r, err := a.InspectToolRecovery(context.Background(), "original")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RetryToolRecovery(context.Background(), "original", r.InspectionID); err == nil {
		t.Fatal("unknown external effect was retried")
	}
	probe.inspection = tool.EffectInspection{State: "absent", Fenced: false}
	if err := a.RetryToolRecovery(context.Background(), "original", r.InspectionID); err == nil {
		t.Fatal("unfenced absence was retried")
	}
	probe.inspection.Fenced = true
	if err := a.RetryToolRecovery(context.Background(), "original", r.InspectionID); err != nil {
		t.Fatal(err)
	}
	if len(probe.keys) != 1 || probe.keys[0] != "stable-effect-key" || len(probe.effects) != 1 {
		t.Fatalf("sink keys=%v effects=%v", probe.keys, probe.effects)
	}
	if len(a.PendingToolRecovery()) != 0 {
		t.Fatal("retry left unresolved evidence")
	}
	if err := a.RetryToolRecovery(context.Background(), "original", r.InspectionID); err == nil {
		t.Fatal("old retry could be replayed")
	}
}

func TestToolRecoverySurvivesHistoryRewrite(t *testing.T) {
	a, _, _ := recoveryActionFixture(t)
	a.Session().Rewrite([]provider.Message{{Role: provider.RoleSystem, Content: "sys"}, {Role: provider.RoleUser, Content: "summary"}}, "compact")
	if len(a.PendingToolRecovery()) != 1 {
		t.Fatal("compaction erased unresolved external effect")
	}
	if len(provider.ModelMessages(a.Session().Snapshot())) != 2 {
		t.Fatal("receipt polluted model view")
	}
}

func TestToolRecoveryConfirmationSupersedesPromptUnknownWithoutRewritingHistory(t *testing.T) {
	a, _, _ := recoveryActionFixture(t)
	r := &provider.InterruptedTurnRecovery{Pending: true, UnknownTools: []provider.InterruptedToolSummary{{ID: "old", Name: "recovery_probe"}}}
	a.Session().Add(provider.Message{Role: provider.RoleTool, LocalOnly: true, InterruptedTurn: r})
	proof, err := a.InspectToolRecovery(context.Background(), "original")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ResolveToolRecovery("original", proof.InspectionID, "confirm"); err != nil {
		t.Fatal(err)
	}
	view := a.pendingInterruptedRecovery()
	if len(view.UnknownTools) != 0 || len(view.UserConfirmedTools) != 1 {
		t.Fatalf("contradictory recovery facts: %+v", view)
	}
	if len(r.UnknownTools) != 1 {
		t.Fatal("prompt projection rewrote durable history")
	}
}
