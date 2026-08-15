package acp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/agentpreset"
	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type statusFactory struct {
	*configurableFactory
}

func TestUsageAccumulatorTotalsMoreThanAuditLimit(t *testing.T) {
	var accumulator usageAccumulator
	usage := &provider.Usage{PromptTokens: 1_000_000}
	pricing := &provider.Pricing{Input: 1, Currency: "USD"}
	for range 65 {
		quote := billing.BuildQuote(billing.QuoteInput{
			Usage:           billing.UsageTokens{PromptTokens: usage.PromptTokens},
			Rates:           billing.RateCard{Input: pricing.Input, Currency: pricing.Currency},
			DisplayCurrency: "USD",
		})
		accumulator.addQuoted(usage, pricing, &quote, event.UsageSourceExecutor)
	}
	wire := accumulator.wire()
	if wire.EstimatedCost == nil || *wire.EstimatedCost != 65 || wire.Currency == nil || *wire.Currency != "USD" {
		t.Fatalf("65-event ACP total was truncated: %+v", wire)
	}
	if wire.CostQuote == nil || wire.CostQuote.Selected == nil || wire.CostQuote.Selected.Amount != "65" {
		t.Fatalf("65-event ACP aggregate quote = %+v", wire.CostQuote)
	}
	if wire.CostComplete == nil || !*wire.CostComplete {
		t.Fatalf("65-event ACP quote incomplete: %+v", wire)
	}
}

func TestUsageAccumulatorExposesAuthoritativeTotalWithoutCacheDoubleCount(t *testing.T) {
	var accumulator usageAccumulator
	accumulator.addQuoted(&provider.Usage{
		PromptTokens: 1_000, CompletionTokens: 500, ReasoningTokens: 300,
		CacheHitTokens: 800, CacheMissTokens: 200,
	}, nil, nil, event.UsageSourceExecutor)

	wire := accumulator.wire()
	if wire.TotalTokens != 1_500 {
		t.Fatalf("total tokens = %d, want 1500: %+v", wire.TotalTokens, wire)
	}
	if wire.PromptTokens != wire.CacheHitTokens+wire.CacheMissTokens {
		t.Fatalf("cache split no longer partitions prompt tokens: %+v", wire)
	}
}

func TestRestoreUsageReconstructsTotalTokensFromLegacySnapshot(t *testing.T) {
	wire := restoreUsage(persistedUsageAccumulator{
		PromptTokens: 1_000, CompletionTokens: 500,
		CacheHitTokens: 800, CacheMissTokens: 200,
	}).wire()

	if wire.TotalTokens != 1_500 {
		t.Fatalf("restored total tokens = %d, want 1500: %+v", wire.TotalTokens, wire)
	}
}

func TestRestoredUsageKeepsFullScalarTotalAfterNewQuote(t *testing.T) {
	complete := true
	accumulator := restoreUsage(persistedUsageAccumulator{
		PromptTokens: 1_000_000, Events: 1, PricedEvents: 1,
		EstimatedCost: 2, Currency: "USD", CostComplete: &complete,
	})
	usage := &provider.Usage{PromptTokens: 1_000_000}
	pricing := &provider.Pricing{Input: 1, Currency: "USD"}
	quote := billing.BuildQuote(billing.QuoteInput{
		Usage:           billing.UsageTokens{PromptTokens: usage.PromptTokens},
		Rates:           billing.RateCard{Input: pricing.Input, Currency: pricing.Currency},
		DisplayCurrency: "USD",
	})
	accumulator.addQuoted(usage, pricing, &quote, event.UsageSourceExecutor)
	wire := accumulator.wire()
	if wire.EstimatedCost == nil || *wire.EstimatedCost != 3 {
		t.Fatalf("restored scalar history was replaced by the new ledger fragment: %+v", wire)
	}
	if wire.CostComplete == nil || !*wire.CostComplete {
		t.Fatalf("restored complete state was lost: %+v", wire)
	}
}

type runtimeTrackingFactory struct {
	*configurableFactory
}

func (f *statusFactory) SessionRuntimeState(_ context.Context, p SessionRuntimeStateParams) (SessionRuntimeState, error) {
	return SessionRuntimeState{
		PlannerMode: "off",
		Sandbox: SessionSandboxState{
			Mode: "enforce", Engine: "bubblewrap", Available: true, WorkspaceRoot: p.Cwd,
			WriteRoots: []string{p.Cwd}, NetworkEnabled: false,
		},
	}, nil
}

func (f *runtimeTrackingFactory) SessionRuntimeState(_ context.Context, p SessionRuntimeStateParams) (SessionRuntimeState, error) {
	return SessionRuntimeState{
		PlannerMode: "on",
		Sandbox: SessionSandboxState{
			Mode: "enforce", Engine: "bubblewrap", Available: true, WorkspaceRoot: p.Cwd,
			WriteRoots: []string{p.Cwd},
		},
	}, nil
}

func openStatusSession(t *testing.T, client *rpcClient, cwd string) string {
	t.Helper()
	resp := client.call(t, "session/new", SessionNewParams{Cwd: cwd})
	if resp.Error != nil {
		t.Fatalf("session/new: %+v", resp.Error)
	}
	var opened SessionNewResult
	if err := json.Unmarshal(resp.Result, &opened); err != nil {
		t.Fatalf("session/new result: %v", err)
	}
	return opened.SessionID
}

func getStatus(t *testing.T, client *rpcClient, sessionID string) ReasonixSessionStatus {
	t.Helper()
	resp := client.call(t, sessionStatusMethod, SessionStatusParams{SessionID: sessionID})
	if resp.Error != nil {
		t.Fatalf("session/status: %+v", resp.Error)
	}
	var status ReasonixSessionStatus
	if err := json.Unmarshal(resp.Result, &status); err != nil {
		t.Fatalf("session/status result: %v", err)
	}
	return status
}

func TestStatusExtensionTracksMultipleSessionsAndUsage(t *testing.T) {
	factory := &statusFactory{configurableFactory: &configurableFactory{
		behavior: func(_ context.Context, sink event.Sink, input string, _ SessionParams) error {
			sink.Emit(event.Event{Kind: event.Phase, Source: event.UsageSourceExecutor, Text: "executor · implementing"})
			sink.Emit(event.Event{Kind: event.Usage, Usage: &provider.Usage{
				PromptTokens: 10, CompletionTokens: 4, ReasoningTokens: 2,
				CacheHitTokens: 7, CacheMissTokens: 3, Estimated: true,
			}, Pricing: &provider.Pricing{CacheHit: 0.1, Input: 1, Output: 2, Currency: "USD"}, UsageSource: event.UsageSourceExecutor})
			sink.Emit(event.Event{Kind: event.Usage, Usage: &provider.Usage{
				PromptTokens: 5, CompletionTokens: 1, CacheMissTokens: 5,
			}, Pricing: &provider.Pricing{CacheHit: 0.1, Input: 1, Output: 2, Currency: "USD"}, UsageSource: event.UsageSourceCompaction})
			sink.Emit(event.Event{Kind: event.Text, Text: input})
			return nil
		},
	}}
	client, stop := startServer(t, factory)
	defer stop()
	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	first := openStatusSession(t, client, t.TempDir())
	second := openStatusSession(t, client, t.TempDir())

	initialSecond := getStatus(t, client, second)
	prompt := client.callAsync("session/prompt", SessionPromptParams{SessionID: first, Prompt: []ContentBlock{{Type: "text", Text: "ship"}}})
	notifications, response := drainPrompt(t, client, prompt)
	if response.Error != nil {
		t.Fatalf("session/prompt: %+v", response.Error)
	}

	firstStatus := getStatus(t, client, first)
	if firstStatus.Sequence == 0 || firstStatus.State != "idle" || firstStatus.TurnOutcome.Kind != "completed" {
		t.Fatalf("first status = %+v", firstStatus)
	}
	if firstStatus.PlannerMode != "off" || firstStatus.Sandbox.WorkspaceRoot == "" || len(firstStatus.Sandbox.WriteRoots) != 1 {
		t.Fatalf("effective runtime status = %+v", firstStatus)
	}
	usage := firstStatus.Usage.Cumulative
	if usage.TotalTokens != 20 || usage.PromptTokens != 15 || usage.CompletionTokens != 5 || usage.ReasoningTokens != 2 || usage.CacheHitTokens != 7 || usage.CacheMissTokens != 8 {
		t.Fatalf("cumulative usage = %+v", usage)
	}
	if usage.UsageSource != "mixed" || usage.CacheHitRatio == nil || usage.EstimatedCost == nil || usage.Currency == nil || *usage.Currency != "USD" {
		t.Fatalf("usage metadata = %+v", usage)
	}
	if !usage.Estimated {
		t.Fatalf("cumulative usage lost estimated marker: %+v", usage)
	}
	secondStatus := getStatus(t, client, second)
	if secondStatus.Sequence != initialSecond.Sequence || secondStatus.Usage.Cumulative.PromptTokens != 0 {
		t.Fatalf("second session telemetry leaked: before=%+v after=%+v", initialSecond, secondStatus)
	}

	var sawPhase, sawUsage, sawCompletion bool
	for _, notification := range notifications {
		if notification.Method != sessionStatusUpdateMethod {
			continue
		}
		var update ReasonixStatusUpdate
		if err := json.Unmarshal(notification.Params, &update); err != nil {
			t.Fatalf("status update: %v", err)
		}
		if update.Sequence != update.Status.Sequence || update.SessionID != first {
			t.Fatalf("status update correlation = %+v", update)
		}
		switch update.Event {
		case "phase":
			sawPhase = true
		case "usage":
			sawUsage = true
		case "completion":
			sawCompletion = true
		}
	}
	if !sawPhase || !sawUsage || !sawCompletion {
		t.Fatalf("status events phase=%v usage=%v completion=%v", sawPhase, sawUsage, sawCompletion)
	}
}

func TestStatusNormalizesPhaseAndRedactsPublicText(t *testing.T) {
	telemetry := newStatusTelemetry()
	telemetry.beginTurn()
	telemetry.onEvent(event.Event{Kind: event.Phase, Source: event.UsageSourcePlanner, Text: "planner · private stage label"})
	if got := telemetry.snapshot().phase; got != "planning" {
		t.Fatalf("planner phase = %q, want planning", got)
	}
	telemetry.onEvent(event.Event{Kind: event.Phase, Text: "provider-specific handoff"})
	if got := telemetry.snapshot().phase; got != "working" {
		t.Fatalf("unknown phase = %q, want working", got)
	}
	telemetry.finishTurn(&agent.FinalReadinessError{
		Attempts: 1,
		Reason:   "token=secret-reason",
		Missing:  []string{"api_key=secret-risk"},
	}, false, "running", "authorization: bearer secret-summary")
	snapshot := telemetry.snapshot()
	encoded, err := json.Marshal(snapshot.finalReadiness)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-") || !strings.Contains(string(encoded), "[redacted]") {
		t.Fatalf("status text was not redacted: %s", encoded)
	}
	if strings.Contains(snapshot.turnOutcome.Reason, "secret-") {
		t.Fatalf("turn outcome was not redacted: %q", snapshot.turnOutcome.Reason)
	}

	empty, err := json.Marshal(newStatusTelemetry().snapshot().finalReadiness)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(empty), `"risks":[]`) {
		t.Fatalf("empty risks must encode as [], got %s", empty)
	}
}

func TestRestoreStatusNormalizesLegacyPresentationPhase(t *testing.T) {
	restored := restoreStatusTelemetry(&persistedStatusTelemetry{
		Phase:          "executor · implementing local patch",
		FinalReadiness: ReasonixFinalReadiness{},
	})
	if got := restored.snapshot().phase; got != "implementing" {
		t.Fatalf("restored phase = %q, want implementing", got)
	}
}

func TestRestoreStatusMarksInterruptedTurnPaused(t *testing.T) {
	restored := restoreStatusTelemetry(&persistedStatusTelemetry{
		Sequence:    7,
		State:       "running",
		Phase:       "implementing",
		TurnOutcome: ReasonixTurnOutcome{Kind: "none"},
		FinalReadiness: ReasonixFinalReadiness{
			ReadyForReview: true,
			Risks:          []string{},
		},
		TurnUsage:  persistedUsageAccumulator{PromptTokens: 3, Estimated: true, Events: 1},
		Cumulative: persistedUsageAccumulator{PromptTokens: 11, Estimated: true, Events: 2},
	})
	snapshot := restored.snapshot()
	if snapshot.state != "idle" || snapshot.phase != "recovery_paused" {
		t.Fatalf("restored interrupted state = state:%q phase:%q, want idle/recovery_paused", snapshot.state, snapshot.phase)
	}
	if snapshot.sequence != 8 || snapshot.turnOutcome.Kind != "paused" || snapshot.turnOutcome.Reason != "previous turn interrupted" {
		t.Fatalf("restored interrupted outcome = sequence:%d outcome:%+v", snapshot.sequence, snapshot.turnOutcome)
	}
	if snapshot.finalReadiness.ReadyForReview {
		t.Fatal("interrupted turn remained ready for review")
	}
	if snapshot.turnUsage.PromptTokens != 3 || snapshot.cumulative.PromptTokens != 11 {
		t.Fatalf("interrupted usage was lost: turn=%+v cumulative=%+v", snapshot.turnUsage, snapshot.cumulative)
	}
	if !snapshot.turnUsage.Estimated || !snapshot.cumulative.Estimated {
		t.Fatalf("interrupted estimated marker was lost: turn=%+v cumulative=%+v", snapshot.turnUsage, snapshot.cumulative)
	}
}

func TestStatusWorkModeSetConfigOptionIsDeprecatedNoop(t *testing.T) {
	factory := &runtimeTrackingFactory{configurableFactory: &configurableFactory{}}
	client, stop := startServer(t, factory)
	defer stop()
	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	sessionID := openStatusSession(t, client, t.TempDir())
	if status := getStatus(t, client, sessionID); status.WorkMode != "balanced" || status.PlannerMode != "on" {
		t.Fatalf("initial runtime status = %+v", status)
	}
	buildsBefore := factory.buildCount()

	for _, value := range []string{"economy", "delivery", "light"} {
		resp := client.call(t, "session/set_config_option", SetSessionConfigOptionParams{
			SessionID: sessionID,
			ConfigID:  "work_mode",
			Value:     value,
		})
		if resp.Error != nil {
			t.Fatalf("set work mode %q: %+v", value, resp.Error)
		}
		var set SetSessionConfigOptionResult
		if err := json.Unmarshal(resp.Result, &set); err != nil {
			t.Fatalf("set work mode %q result: %v", value, err)
		}
		if set.DeprecatedNotice != agentpreset.DeprecatedNotice {
			t.Fatalf("set work mode %q deprecatedNotice = %q", value, set.DeprecatedNotice)
		}
		status := getStatus(t, client, sessionID)
		if status.WorkMode != "balanced" || status.PlannerMode != "on" {
			t.Fatalf("runtime status after deprecated work_mode %q = %+v", value, status)
		}
	}
	if got := factory.buildCount(); got != buildsBefore {
		t.Fatalf("work_mode rebuilt controller: builds=%d, want %d", got, buildsBefore)
	}
}

func TestStatusClassifiesPauseAndError(t *testing.T) {
	telemetry := newStatusTelemetry()
	telemetry.beginTurn()
	pauseEvent := telemetry.finishTurn(&agent.FinalReadinessError{Attempts: 3, Reason: "missing verification", Missing: []string{"verify"}}, false, "running", "partial")
	paused := telemetry.snapshot()
	if pauseEvent != "pause" || paused.turnOutcome.Kind != "paused" || len(paused.finalReadiness.Risks) != 1 {
		t.Fatalf("pause classification = event %q snapshot %+v", pauseEvent, paused)
	}

	telemetry.beginTurn()
	errorEvent := telemetry.finishTurn(errors.New("provider failed"), false, "running", "")
	failed := telemetry.snapshot()
	if errorEvent != "error" || failed.turnOutcome.Kind != "error" || failed.goalOverride != "failed" {
		t.Fatalf("error classification = event %q snapshot %+v", errorEvent, failed)
	}
}

func TestStatusSnapshotSurvivesSessionResume(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sessionID := "status-reconnect"
	telemetry := newStatusTelemetry()
	telemetry.beginTurn()
	telemetry.onEvent(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		PromptTokens: 8, CompletionTokens: 2, CacheHitTokens: 6, CacheMissTokens: 2,
	}, UsageSource: event.UsageSourceExecutor})
	telemetry.finishTurn(nil, false, "", "persisted summary")
	path := filepath.Join(dir, sessionID+".jsonl")
	if err := agent.NewSession("system").Save(path); err != nil {
		t.Fatalf("save transcript: %v", err)
	}
	if err := saveACPMeta(path, acpSessionMeta{
		SessionID: sessionID, Cwd: cwd, Model: "fast", RuntimeProfile: "delivery",
		Status: telemetry.persisted(),
	}); err != nil {
		t.Fatalf("save ACP metadata: %v", err)
	}
	factory := &statusFactory{configurableFactory: &configurableFactory{
		dir: dir,
	}}
	reconnected, stopReconnected := startServer(t, factory)
	defer stopReconnected()
	reconnected.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	resume := reconnected.call(t, "session/resume", SessionResumeParams{SessionID: sessionID, Cwd: cwd})
	if resume.Error != nil {
		t.Fatalf("session/resume: %+v", resume.Error)
	}
	after := getStatus(t, reconnected, sessionID)
	if after.Sequence != telemetry.snapshot().sequence || after.Usage.Cumulative.PromptTokens != 8 || after.State != "idle" || after.FinalReadiness.Summary != "persisted summary" {
		t.Fatalf("recovered status = %+v", after)
	}
}

func TestStatusInterruptedSnapshotResumesPaused(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sessionID := "status-interrupted"
	telemetry := newStatusTelemetry()
	telemetry.beginTurn()
	telemetry.onEvent(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		PromptTokens: 5, CompletionTokens: 1,
	}, UsageSource: event.UsageSourceExecutor})
	path := filepath.Join(dir, sessionID+".jsonl")
	if err := agent.NewSession("system").Save(path); err != nil {
		t.Fatalf("save transcript: %v", err)
	}
	if err := saveACPMeta(path, acpSessionMeta{
		SessionID: sessionID, Cwd: cwd, Model: "fast", RuntimeProfile: "balanced",
		Status: telemetry.persisted(),
	}); err != nil {
		t.Fatalf("save ACP metadata: %v", err)
	}

	factory := &statusFactory{configurableFactory: &configurableFactory{dir: dir}}
	client, stop := startServer(t, factory)
	defer stop()
	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	resume := client.call(t, "session/resume", SessionResumeParams{SessionID: sessionID, Cwd: cwd})
	if resume.Error != nil {
		t.Fatalf("session/resume: %+v", resume.Error)
	}
	after := getStatus(t, client, sessionID)
	if after.State != "idle" || after.Phase != "recovery_paused" || after.TurnOutcome.Kind != "paused" {
		t.Fatalf("resumed interrupted status = %+v", after)
	}
	if after.Sequence != telemetry.snapshot().sequence+1 || after.Usage.Turn.PromptTokens != 5 || after.Usage.Cumulative.PromptTokens != 5 {
		t.Fatalf("resumed interrupted sequence/usage = %+v", after)
	}
}
