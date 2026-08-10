package acp

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"runtime"
	"slices"
	"strings"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/secrets"
)

const (
	reasonixStatusSchemaVersion = 1
	sessionStatusMethod         = "_reasonix.io/session/status"
	sessionStatusUpdateMethod   = "_reasonix.io/session/status_update"
)

// ReasonixSchemaCapability advertises one versioned vendor extension in
// agentCapabilities._meta. The method name is the map key so clients can fail
// closed before opening a session.
type ReasonixSchemaCapability struct {
	SchemaVersion int `json:"schemaVersion"`
}

// SessionStatusParams addresses one live ACP session.
type SessionStatusParams struct {
	SessionID string `json:"sessionId"`
}

// SessionSandboxState is the effective sandbox, after all CLI hard overrides
// have been applied. It deliberately reports no credentials or environment.
type SessionSandboxState struct {
	Mode           string   `json:"mode"`
	Engine         string   `json:"engine"`
	Available      bool     `json:"available"`
	WorkspaceRoot  string   `json:"workspaceRoot"`
	WriteRoots     []string `json:"writeRoots"`
	NetworkEnabled bool     `json:"networkEnabled"`
}

// SessionRuntimeState is supplied by the composition root because only it can
// truthfully report config-derived sandbox and planner state.
type SessionRuntimeState struct {
	PlannerMode string              `json:"plannerMode"`
	Sandbox     SessionSandboxState `json:"sandbox"`
}

// SessionRuntimeStateParams identifies the exact controller configuration whose
// effective policy is being reported. Model/profile switches rebuild the
// controller, so status must be recomputed from the same resolved inputs.
type SessionRuntimeStateParams struct {
	Cwd            string
	Model          string
	RuntimeProfile string
}

// SessionRuntimeStateProvider exposes effective process/session policy without
// coupling the ACP adapter to Reasonix configuration internals.
type SessionRuntimeStateProvider interface {
	SessionRuntimeState(ctx context.Context, p SessionRuntimeStateParams) (SessionRuntimeState, error)
}

type ReasonixStatusGoal struct {
	Status    string `json:"status"`
	Objective string `json:"objective,omitempty"`
	// Runtime is the optional Goal budget/runtime summary; absent for old
	// hosts or when no goal is active.
	Runtime *ReasonixGoalRuntime `json:"runtime,omitempty"`
}

type ReasonixGoalRuntime struct {
	TurnsUsed        int    `json:"turnsUsed"`
	TurnsLimit       int    `json:"turnsLimit"`
	TokensUsed       int    `json:"tokensUsed"`
	TokensLimit      int    `json:"tokensLimit"` // Deprecated: always 0; retained for protocol compatibility.
	NoProgressTurns  int    `json:"noProgressTurns"`
	NoProgressLimit  int    `json:"noProgressLimit"`
	LastReason       string `json:"lastReason,omitempty"`
	StopCause        string `json:"stopCause,omitempty"`
	BudgetExtensions int    `json:"budgetExtensions"`
}

type ReasonixTurnOutcome struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason,omitempty"`
}

type ReasonixFinalReadiness struct {
	ReadyForReview bool     `json:"readyForReview"`
	Summary        string   `json:"summary"`
	Risks          []string `json:"risks"`
}

type ReasonixUsage struct {
	PromptTokens     int      `json:"promptTokens"`
	CompletionTokens int      `json:"completionTokens"`
	ReasoningTokens  int      `json:"reasoningTokens"`
	CacheHitTokens   int      `json:"cacheHitTokens"`
	CacheMissTokens  int      `json:"cacheMissTokens"`
	Estimated        bool     `json:"estimated,omitempty"`
	CacheHitRatio    *float64 `json:"cacheHitRatio"`
	EstimatedCost    *float64 `json:"estimatedCost"`
	Currency         *string  `json:"currency"`
	UsageSource      string   `json:"usageSource"`
}

type ReasonixStatusUsage struct {
	Turn       ReasonixUsage `json:"turn"`
	Cumulative ReasonixUsage `json:"cumulative"`
}

// ReasonixSessionStatus is the stable schemaVersion=1 recovery snapshot.
// Reasoning text and unbounded terminal output are intentionally absent.
type ReasonixSessionStatus struct {
	SchemaVersion  int                    `json:"schemaVersion"`
	Sequence       uint64                 `json:"sequence"`
	SessionID      string                 `json:"sessionId"`
	State          string                 `json:"state"`
	Model          string                 `json:"model"`
	Effort         string                 `json:"effort"`
	Mode           string                 `json:"mode"`
	WorkMode       string                 `json:"workMode"`
	PlannerMode    string                 `json:"plannerMode"`
	Goal           ReasonixStatusGoal     `json:"goal"`
	Phase          string                 `json:"phase"`
	TurnOutcome    ReasonixTurnOutcome    `json:"turnOutcome"`
	FinalReadiness ReasonixFinalReadiness `json:"finalReadiness"`
	Sandbox        SessionSandboxState    `json:"sandbox"`
	Usage          ReasonixStatusUsage    `json:"usage"`
}

type ReasonixStatusUpdate struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Sequence      uint64                `json:"sequence"`
	SessionID     string                `json:"sessionId"`
	Event         string                `json:"event"`
	Status        ReasonixSessionStatus `json:"status"`
}

type usageAccumulator struct {
	promptTokens     int
	completionTokens int
	reasoningTokens  int
	cacheHitTokens   int
	cacheMissTokens  int
	estimated        bool
	events           int
	pricedEvents     int
	estimatedCost    float64
	currency         string
	source           string
}

func (a *usageAccumulator) add(u *provider.Usage, pricing *provider.Pricing, source string) {
	if u == nil {
		return
	}
	a.promptTokens += u.PromptTokens
	a.completionTokens += u.CompletionTokens
	a.reasoningTokens += u.ReasoningTokens
	a.cacheHitTokens += u.CacheHitTokens
	a.cacheMissTokens += u.CacheMissTokens
	a.estimated = a.estimated || u.Estimated
	a.events++
	source = strings.TrimSpace(source)
	if source == "" {
		source = event.UsageSourceExecutor
	}
	if a.source == "" {
		a.source = source
	} else if a.source != source {
		a.source = "mixed"
	}
	if pricing != nil {
		currency := strings.TrimSpace(pricing.Currency)
		if currency == "" {
			currency = pricing.Symbol()
		}
		if a.pricedEvents == 0 {
			a.currency = currency
		} else if a.currency != currency {
			a.currency = ""
		}
		a.estimatedCost += pricing.Cost(u)
		a.pricedEvents++
	}
}

func (a usageAccumulator) wire() ReasonixUsage {
	usage := ReasonixUsage{
		PromptTokens:     a.promptTokens,
		CompletionTokens: a.completionTokens,
		ReasoningTokens:  a.reasoningTokens,
		CacheHitTokens:   a.cacheHitTokens,
		CacheMissTokens:  a.cacheMissTokens,
		Estimated:        a.estimated,
		UsageSource:      a.source,
	}
	if usage.UsageSource == "" {
		usage.UsageSource = event.UsageSourceExecutor
	}
	if total := a.cacheHitTokens + a.cacheMissTokens; total > 0 {
		ratio := float64(a.cacheHitTokens) / float64(total)
		usage.CacheHitRatio = &ratio
	}
	if a.events > 0 && a.pricedEvents == a.events && a.currency != "" && !math.IsNaN(a.estimatedCost) && !math.IsInf(a.estimatedCost, 0) {
		cost := a.estimatedCost
		currency := a.currency
		usage.EstimatedCost = &cost
		usage.Currency = &currency
	}
	return usage
}

type statusTelemetry struct {
	mu             sync.Mutex
	sequence       uint64
	state          string
	phase          string
	turnOutcome    ReasonixTurnOutcome
	finalReadiness ReasonixFinalReadiness
	turnUsage      usageAccumulator
	cumulative     usageAccumulator
	goalOverride   string
}

func newStatusTelemetry() *statusTelemetry {
	return &statusTelemetry{
		state:       "idle",
		phase:       "idle",
		turnOutcome: ReasonixTurnOutcome{Kind: "none"},
		finalReadiness: ReasonixFinalReadiness{
			Risks: []string{},
		},
	}
}

func (t *statusTelemetry) mutate(fn func(*statusTelemetry)) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	fn(t)
	t.sequence++
	return t.sequence
}

func (t *statusTelemetry) beginTurn() {
	t.mutate(func(t *statusTelemetry) {
		t.state = "running"
		t.phase = "starting"
		t.turnOutcome = ReasonixTurnOutcome{Kind: "none"}
		t.finalReadiness = ReasonixFinalReadiness{Risks: []string{}}
		t.turnUsage = usageAccumulator{}
		t.goalOverride = ""
	})
}

func (t *statusTelemetry) onEvent(e event.Event) (string, bool) {
	switch e.Kind {
	case event.Phase:
		t.mutate(func(t *statusTelemetry) {
			t.phase = normalizeStatusPhase(e)
		})
		return "phase", true
	case event.Usage:
		t.mutate(func(t *statusTelemetry) {
			t.turnUsage.add(e.Usage, e.Pricing, e.UsageSource)
			t.cumulative.add(e.Usage, e.Pricing, e.UsageSource)
		})
		return "usage", true
	case event.ApprovalRequest:
		t.mutate(func(t *statusTelemetry) { t.phase = "waiting_permission" })
		return "phase", true
	case event.AskRequest:
		t.mutate(func(t *statusTelemetry) { t.phase = "waiting_input" })
		return "phase", true
	case event.ToolDispatch:
		t.mutate(func(t *statusTelemetry) { t.phase = "implementing" })
	case event.Notice:
		if e.Code == event.NoticeCodeFinalReadiness {
			t.mutate(func(t *statusTelemetry) { t.phase = "checking_readiness" })
			return "phase", true
		}
	}
	return "", false
}

func (t *statusTelemetry) finishTurn(runErr error, cancelled bool, goalStatus, summary string) string {
	eventName := "completion"
	t.mutate(func(t *statusTelemetry) {
		t.state = "idle"
		t.finalReadiness.Summary = clipStatusText(summary, 16_384)
		t.finalReadiness.Risks = []string{}
		t.goalOverride = ""
		switch {
		case cancelled:
			t.phase = "cancelled"
			t.turnOutcome = ReasonixTurnOutcome{Kind: "cancelled"}
			t.goalOverride = "cancelled"
			eventName = "completion"
		case runErr == nil && goalStatus == control.GoalStatusComplete:
			t.phase = "review_ready"
			t.turnOutcome = ReasonixTurnOutcome{Kind: "completed"}
			t.finalReadiness.ReadyForReview = true
		case runErr == nil && (goalStatus == "" || goalStatus == control.GoalStatusStopped):
			t.phase = "completed"
			t.turnOutcome = ReasonixTurnOutcome{Kind: "completed"}
			t.finalReadiness.ReadyForReview = true
		case goalStatus == control.GoalStatusBlocked:
			t.phase = "paused"
			t.turnOutcome = ReasonixTurnOutcome{Kind: "paused", Reason: "goal blocked"}
			eventName = "pause"
		default:
			var readinessErr *agent.FinalReadinessError
			var recoveryPause *agent.RecoveryPauseError
			switch {
			case errors.As(runErr, &readinessErr):
				t.phase = "readiness_paused"
				t.turnOutcome = ReasonixTurnOutcome{Kind: "paused", Reason: clipStatusText(readinessErr.Error(), 2_048)}
				t.finalReadiness.Risks = redactStatusTexts(readinessErr.Missing, 2_048)
				eventName = "pause"
			case errors.As(runErr, &recoveryPause):
				t.phase = "recovery_paused"
				t.turnOutcome = ReasonixTurnOutcome{Kind: "paused", Reason: clipStatusText(recoveryPause.Error(), 2_048)}
				eventName = "pause"
			case runErr != nil:
				t.phase = "error"
				t.turnOutcome = ReasonixTurnOutcome{Kind: "error", Reason: clipStatusText(runErr.Error(), 2_048)}
				t.goalOverride = "failed"
				eventName = "error"
			default:
				t.phase = "paused"
				t.turnOutcome = ReasonixTurnOutcome{Kind: "paused", Reason: "goal is not complete"}
				eventName = "pause"
			}
		}
	})
	return eventName
}

type statusTelemetrySnapshot struct {
	sequence       uint64
	state          string
	phase          string
	turnOutcome    ReasonixTurnOutcome
	finalReadiness ReasonixFinalReadiness
	turnUsage      ReasonixUsage
	cumulative     ReasonixUsage
	goalOverride   string
}

type persistedUsageAccumulator struct {
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	ReasoningTokens  int     `json:"reasoningTokens"`
	CacheHitTokens   int     `json:"cacheHitTokens"`
	CacheMissTokens  int     `json:"cacheMissTokens"`
	Estimated        bool    `json:"estimated,omitempty"`
	Events           int     `json:"events"`
	PricedEvents     int     `json:"pricedEvents"`
	EstimatedCost    float64 `json:"estimatedCost"`
	Currency         string  `json:"currency,omitempty"`
	Source           string  `json:"source,omitempty"`
}

type persistedStatusTelemetry struct {
	Sequence       uint64                    `json:"sequence"`
	State          string                    `json:"state"`
	Phase          string                    `json:"phase"`
	TurnOutcome    ReasonixTurnOutcome       `json:"turnOutcome"`
	FinalReadiness ReasonixFinalReadiness    `json:"finalReadiness"`
	TurnUsage      persistedUsageAccumulator `json:"turnUsage"`
	Cumulative     persistedUsageAccumulator `json:"cumulative"`
	GoalOverride   string                    `json:"goalOverride,omitempty"`
}

func persistUsage(a usageAccumulator) persistedUsageAccumulator {
	return persistedUsageAccumulator{
		PromptTokens: a.promptTokens, CompletionTokens: a.completionTokens,
		ReasoningTokens: a.reasoningTokens, CacheHitTokens: a.cacheHitTokens,
		CacheMissTokens: a.cacheMissTokens, Estimated: a.estimated, Events: a.events,
		PricedEvents: a.pricedEvents, EstimatedCost: a.estimatedCost,
		Currency: a.currency, Source: a.source,
	}
}

func restoreUsage(a persistedUsageAccumulator) usageAccumulator {
	return usageAccumulator{
		promptTokens: a.PromptTokens, completionTokens: a.CompletionTokens,
		reasoningTokens: a.ReasoningTokens, cacheHitTokens: a.CacheHitTokens,
		cacheMissTokens: a.CacheMissTokens, estimated: a.Estimated, events: a.Events,
		pricedEvents: a.PricedEvents, estimatedCost: a.EstimatedCost,
		currency: a.Currency, source: a.Source,
	}
}

func (t *statusTelemetry) persisted() *persistedStatusTelemetry {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return &persistedStatusTelemetry{
		Sequence: t.sequence, State: t.state, Phase: t.phase,
		TurnOutcome: t.turnOutcome,
		FinalReadiness: ReasonixFinalReadiness{
			ReadyForReview: t.finalReadiness.ReadyForReview,
			Summary:        clipStatusText(t.finalReadiness.Summary, 16_384),
			Risks:          redactStatusTexts(t.finalReadiness.Risks, 2_048),
		},
		TurnUsage: persistUsage(t.turnUsage), Cumulative: persistUsage(t.cumulative),
		GoalOverride: t.goalOverride,
	}
}

func restoreStatusTelemetry(saved *persistedStatusTelemetry) *statusTelemetry {
	t := newStatusTelemetry()
	if saved == nil {
		return t
	}
	interrupted := saved.State == "running"
	t.sequence = saved.Sequence
	// A restored process never owns the turn that wrote a running snapshot.
	// Publish a new, terminal recovery state instead of leaving supervisors
	// waiting on work that no longer exists in this runtime.
	t.state = "idle"
	t.phase = normalizePersistedStatusPhase(saved.Phase)
	t.turnOutcome = saved.TurnOutcome
	if t.turnOutcome.Kind == "" {
		t.turnOutcome.Kind = "none"
	}
	t.finalReadiness = ReasonixFinalReadiness{
		ReadyForReview: saved.FinalReadiness.ReadyForReview,
		Summary:        clipStatusText(saved.FinalReadiness.Summary, 16_384),
		Risks:          redactStatusTexts(saved.FinalReadiness.Risks, 2_048),
	}
	t.turnUsage = restoreUsage(saved.TurnUsage)
	t.cumulative = restoreUsage(saved.Cumulative)
	t.goalOverride = saved.GoalOverride
	if interrupted {
		t.sequence++
		t.phase = "recovery_paused"
		t.turnOutcome = ReasonixTurnOutcome{Kind: "paused", Reason: "previous turn interrupted"}
		t.finalReadiness.ReadyForReview = false
	}
	return t
}

func (t *statusTelemetry) snapshot() statusTelemetrySnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return statusTelemetrySnapshot{
		sequence: t.sequence,
		state:    t.state,
		phase:    t.phase,
		turnOutcome: ReasonixTurnOutcome{
			Kind: t.turnOutcome.Kind, Reason: clipStatusText(t.turnOutcome.Reason, 2_048),
		},
		finalReadiness: ReasonixFinalReadiness{
			ReadyForReview: t.finalReadiness.ReadyForReview,
			Summary:        clipStatusText(t.finalReadiness.Summary, 16_384),
			Risks:          redactStatusTexts(t.finalReadiness.Risks, 2_048),
		},
		turnUsage:    t.turnUsage.wire(),
		cumulative:   t.cumulative.wire(),
		goalOverride: t.goalOverride,
	}
}

func defaultSessionRuntimeState(cwd string) SessionRuntimeState {
	engine := "bubblewrap"
	if runtime.GOOS == "darwin" {
		engine = "seatbelt"
	}
	return SessionRuntimeState{
		PlannerMode: "on",
		Sandbox: SessionSandboxState{
			Mode:          "enforce",
			Engine:        engine,
			Available:     true,
			WorkspaceRoot: cwd,
			WriteRoots:    []string{cwd},
		},
	}
}

func normalizeStatusEffort(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "auto"
	}
	return strings.TrimSpace(*value)
}

func normalizeGoalStatus(value string) string {
	switch value {
	case control.GoalStatusRunning, control.GoalStatusComplete, control.GoalStatusBlocked:
		return value
	default:
		return "none"
	}
}

func clipStatusText(value string, limit int) string {
	value = strings.TrimSpace(secrets.Redact(value))
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func redactStatusTexts(values []string, limit int) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, clipStatusText(value, limit))
	}
	return out
}

func normalizeStatusPhase(e event.Event) string {
	switch strings.TrimSpace(e.Source) {
	case event.UsageSourcePlanner:
		return "planning"
	case event.UsageSourceExecutor:
		return "implementing"
	}
	lower := strings.ToLower(strings.TrimSpace(e.Text))
	switch {
	case strings.Contains(lower, "planning"):
		return "planning"
	case strings.Contains(lower, "executing"), strings.Contains(lower, "implementing"):
		return "implementing"
	default:
		return "working"
	}
}

func normalizePersistedStatusPhase(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "idle", "starting", "planning", "working", "implementing",
		"waiting_permission", "waiting_input", "checking_readiness",
		"cancelled", "review_ready", "completed", "paused",
		"readiness_paused", "recovery_paused", "error":
		return value
	default:
		return normalizeStatusPhase(event.Event{Kind: event.Phase, Text: value})
	}
}

func (s *service) sessionRuntimeState(ctx context.Context, p SessionRuntimeStateParams) (SessionRuntimeState, error) {
	if provider, ok := s.factory.(SessionRuntimeStateProvider); ok {
		state, err := provider.SessionRuntimeState(ctx, p)
		if err != nil {
			return SessionRuntimeState{}, err
		}
		if strings.EqualFold(strings.TrimSpace(p.RuntimeProfile), "economy") {
			state.PlannerMode = "off"
		}
		if strings.TrimSpace(state.PlannerMode) == "" {
			state.PlannerMode = "on"
		}
		if state.Sandbox.WriteRoots == nil {
			state.Sandbox.WriteRoots = []string{}
		}
		return state, nil
	}
	state := defaultSessionRuntimeState(p.Cwd)
	if strings.EqualFold(strings.TrimSpace(p.RuntimeProfile), "economy") {
		state.PlannerMode = "off"
	}
	return state, nil
}

func (s *service) bindStatusEvents(sess *acpSession) {
	if sess == nil || sess.sink == nil {
		return
	}
	if sess.status == nil {
		sess.status = newStatusTelemetry()
	}
	sess.sink.bindStatus(func(e event.Event) {
		eventName, publish := sess.status.onEvent(e)
		if publish {
			s.publishStatus(sess, eventName)
		}
	})
}

func (s *service) sessionStatus(_ context.Context, raw json.RawMessage) (any, error) {
	var p SessionStatusParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionStatusMethod + ": " + err.Error()}
	}
	sess := s.session(p.SessionID)
	if sess == nil {
		return nil, &RPCError{Code: ErrInvalidParams, Message: sessionStatusMethod + ": unknown session " + p.SessionID}
	}
	return sess.statusSnapshot(), nil
}

func (s *service) publishStatus(sess *acpSession, eventName string) {
	if sess == nil {
		return
	}
	status := sess.statusSnapshot()
	_ = s.conn.Notify(sessionStatusUpdateMethod, ReasonixStatusUpdate{
		SchemaVersion: reasonixStatusSchemaVersion,
		Sequence:      status.Sequence,
		SessionID:     status.SessionID,
		Event:         eventName,
		Status:        status,
	})
}

func (s *acpSession) statusSnapshot() ReasonixSessionStatus {
	s.mu.Lock()
	id := s.id
	ctrl := s.ctrl
	model := s.model
	effort := cloneStringPtr(s.effortOverride)
	workMode := s.runtimeProfile
	mode := s.modeID
	runtimeState := s.runtimeState
	telemetry := s.status
	s.mu.Unlock()

	if telemetry == nil {
		telemetry = newStatusTelemetry()
	}
	t := telemetry.snapshot()
	goalStatus := "none"
	goalObjective := ""
	var goalRuntime *ReasonixGoalRuntime
	if ctrl != nil {
		goalStatus = normalizeGoalStatus(ctrl.GoalStatus())
		goalObjective = clipStatusText(ctrl.Goal(), 16_384)
		if strings.TrimSpace(goalObjective) != "" {
			rt := ctrl.GoalRuntime()
			goalRuntime = &ReasonixGoalRuntime{
				TurnsUsed:        rt.TurnsUsed,
				TurnsLimit:       rt.TurnsLimit,
				TokensUsed:       rt.TokensUsed,
				TokensLimit:      rt.TokensLimit,
				NoProgressTurns:  rt.NoProgressTurns,
				NoProgressLimit:  rt.NoProgressLimit,
				LastReason:       rt.LastReason,
				StopCause:        rt.StopCause,
				BudgetExtensions: rt.BudgetExtensions,
			}
		}
	}
	if t.goalOverride != "" {
		goalStatus = t.goalOverride
	}
	mode = normalizeACPCollaborationMode(mode)
	workMode = strings.ToLower(strings.TrimSpace(workMode))
	switch workMode {
	case "economy", "delivery":
	default:
		workMode = "balanced"
	}
	if runtimeState.PlannerMode != "off" {
		runtimeState.PlannerMode = "on"
	}
	if runtimeState.Sandbox.WriteRoots == nil {
		runtimeState.Sandbox.WriteRoots = []string{}
	}
	phase := strings.TrimSpace(t.phase)
	if phase == "" {
		phase = "idle"
	}
	state := t.state
	if state != "running" {
		state = "idle"
	}
	return ReasonixSessionStatus{
		SchemaVersion: reasonixStatusSchemaVersion,
		Sequence:      t.sequence,
		SessionID:     id,
		State:         state,
		Model:         strings.TrimSpace(model),
		Effort:        normalizeStatusEffort(effort),
		Mode:          mode,
		WorkMode:      workMode,
		PlannerMode:   runtimeState.PlannerMode,
		Goal: ReasonixStatusGoal{
			Status:    goalStatus,
			Objective: goalObjective,
			Runtime:   goalRuntime,
		},
		Phase:          phase,
		TurnOutcome:    t.turnOutcome,
		FinalReadiness: t.finalReadiness,
		Sandbox:        runtimeState.Sandbox,
		Usage: ReasonixStatusUsage{
			Turn:       t.turnUsage,
			Cumulative: t.cumulative,
		},
	}
}

func finalAssistantSummary(ctrl acpController) string {
	if ctrl == nil {
		return ""
	}
	history := ctrl.History()
	for _, v := range slices.Backward(history) {
		if v.Role == provider.RoleAssistant && strings.TrimSpace(v.Content) != "" {
			return v.Content
		}
	}
	return ""
}
