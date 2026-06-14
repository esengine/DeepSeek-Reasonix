package daemon

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

func (d *Daemon) enqueueIntent(intent RunIntent) {
	if intent.CreatedAt.IsZero() {
		intent.CreatedAt = time.Now().UTC()
	}
	if intent.Source == "" {
		intent.Source = "daemon"
	}
	if intent.Priority == 0 {
		intent.Priority = defaultIntentPriority(intent)
	}
	d.appendTimeline(intent.SessionPath, agent.RuntimeTimelineEvent{
		Type:    "intent_queued",
		Source:  intent.Source,
		Reason:  intent.Reason,
		EventID: intent.EventID,
		Step:    "deterministic",
	})
	select {
	case d.intentCh <- intent:
	default:
		d.logger.Warn("daemon intent queue full", "session", intent.SessionID, "source", intent.Source)
	}
}

type intentDone struct {
	sessionID string
}

func (d *Daemon) runIntentWorker(ctx context.Context) {
	var pending []RunIntent
	busy := make(map[string]struct{})
	running := 0
	doneCh := make(chan intentDone, d.maxConcurrent)

	dispatch := func() {
		for running < d.maxConcurrent {
			idx := d.nextRunnableIntentIndex(pending, busy)
			if idx < 0 {
				return
			}
			intent := pending[idx]
			pending = append(pending[:idx], pending[idx+1:]...)
			busy[intent.SessionID] = struct{}{}
			running++
			go func(intent RunIntent) {
				defer func() {
					doneCh <- intentDone{sessionID: intent.SessionID}
				}()
				d.executeIntent(ctx, intent)
			}(intent)
		}
	}

	for {
		dispatch()
		select {
		case <-ctx.Done():
			return
		case intent := <-d.intentCh:
			pending = append(pending, intent)
			pending = drainIntentQueue(d.intentCh, pending)
		case done := <-doneCh:
			delete(busy, done.sessionID)
			if running > 0 {
				running--
			}
		}
	}
}

func drainIntentQueue(ch <-chan RunIntent, pending []RunIntent) []RunIntent {
	for {
		select {
		case intent := <-ch:
			pending = append(pending, intent)
		default:
			return pending
		}
	}
}

func (d *Daemon) nextRunnableIntentIndex(pending []RunIntent, busy map[string]struct{}) int {
	best := -1
	for i, intent := range pending {
		if _, ok := busy[intent.SessionID]; ok {
			continue
		}
		if d.isSessionActive(intent.SessionID) {
			continue
		}
		if best < 0 ||
			intent.Priority > pending[best].Priority ||
			(intent.Priority == pending[best].Priority && intent.CreatedAt.Before(pending[best].CreatedAt)) {
			best = i
		}
	}
	return best
}

func (d *Daemon) isSessionActive(sessionID string) bool {
	d.mu.RLock()
	_, ok := d.activeRuns[sessionID]
	d.mu.RUnlock()
	return ok
}

func defaultIntentPriority(intent RunIntent) int {
	switch strings.TrimSpace(intent.Source) {
	case "api", "bot", "user":
		return 100
	case "webhook", "file_watch", "time":
		return 50
	case "cron":
		return 10
	default:
		return 20
	}
}

func waitRunStatus(kind string) string {
	switch kind {
	case "approval":
		return agent.RunStatusWaitingApproval
	case "ask":
		return agent.RunStatusWaitingAsk
	case "event":
		return agent.RunStatusWaitingEvent
	case "time":
		return agent.RunStatusWaitingTime
	case "file":
		return agent.RunStatusWaitingFile
	default:
		return "waiting_" + kind
	}
}

func (d *Daemon) executeIntent(parent context.Context, intent RunIntent) {
	d.mu.Lock()
	entry, ok := d.registry[intent.SessionID]
	if !ok {
		d.mu.Unlock()
		return
	}
	if _, exists := d.activeRuns[intent.SessionID]; exists {
		d.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	if ok, reason := checkModelBudget(&entry.Runtime, firstNonEmpty(intent.Source, intent.Reason, "daemon"), now); !ok {
		entry.Runtime.Run.Status = agent.RunStatusIdle
		entry.Runtime.Run.LastError = reason
		entry.Runtime.Scheduler.LastWakeupReason = "budget_blocked:model"
		runtime := entry.Runtime
		path := entry.Path
		d.mu.Unlock()
		if err := saveRuntimeMeta(path, runtime); err != nil {
			d.logger.Warn("daemon: persist model budget block", "session", intent.SessionID, "err", err)
		}
		d.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       "model_budget_blocked",
			Source:     firstNonEmpty(intent.Source, "daemon"),
			Reason:     reason,
			EventID:    intent.EventID,
			Step:       "deterministic",
			RunStatus:  runtime.Run.Status,
			GoalStatus: runtime.Goal.Status,
			Message:    reason,
		})
		return
	}
	if ok, reason := d.checkScopeModelBudgetLocked(entry, firstNonEmpty(intent.Source, intent.Reason, "daemon"), now); !ok {
		entry.Runtime.Run.Status = agent.RunStatusIdle
		entry.Runtime.Run.LastError = reason
		entry.Runtime.Scheduler.LastWakeupReason = "budget_blocked:model"
		runtime := entry.Runtime
		path := entry.Path
		d.mu.Unlock()
		if err := saveRuntimeMeta(path, runtime); err != nil {
			d.logger.Warn("daemon: persist scope model budget block", "session", intent.SessionID, "err", err)
		}
		d.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       "model_budget_blocked",
			Source:     firstNonEmpty(intent.Source, "daemon"),
			Reason:     reason,
			EventID:    intent.EventID,
			Step:       "deterministic",
			RunStatus:  runtime.Run.Status,
			GoalStatus: runtime.Goal.Status,
			Message:    reason,
		})
		return
	}
	entryCopy := *entry
	ctx, cancel := context.WithCancel(parent)
	active := &ActiveRun{
		Intent:    intent,
		StartedAt: time.Now().UTC(),
		Cancel:    cancel,
		Approvals: make(map[string]event.Approval),
		Asks:      make(map[string]event.Ask),
	}
	d.activeRuns[intent.SessionID] = active
	runPath := entry.Path
	d.mu.Unlock()
	d.appendTimeline(runPath, agent.RuntimeTimelineEvent{
		Type:    "run_started",
		Source:  intent.Source,
		Reason:  intent.Reason,
		EventID: intent.EventID,
		Step:    "deterministic",
	})

	sink := event.Sync(event.FuncSink(func(e event.Event) {
		d.handleRunEvent(intent.SessionID, e)
	}))
	ctrl, err := d.buildController(ctx, d, &entryCopy, sink)
	if err != nil {
		cancel()
		d.finishIntent(intent.SessionID, agent.RunStatusFailed, err)
		return
	}
	applyModelBudgetTurnLimit(ctrl, entryCopy.Runtime)
	d.mu.Lock()
	active.Control = ctrl
	d.mu.Unlock()
	defer ctrl.Close()
	defer cancel()

	err = ctrl.ContinueGoalWithContext(ctx, firstNonEmpty(intent.Reason, intent.Source), intent.Context)
	if err != nil && ctx.Err() == nil {
		d.finishIntent(intent.SessionID, agent.RunStatusFailed, err)
		return
	}
	d.finishIntent(intent.SessionID, "", err)
}

func defaultControllerFactory(ctx context.Context, d *Daemon, entry *SessionEntry, sink event.Sink) (*control.Controller, error) {
	sess, err := agent.LoadSession(entry.Path)
	if err != nil {
		return nil, err
	}
	workspaceRoot := strings.TrimSpace(entry.Runtime.WorkspaceRoot)
	if workspaceRoot == "" {
		if meta, ok, err := agent.LoadBranchMeta(entry.Path); err == nil && ok {
			workspaceRoot = meta.WorkspaceRoot
		}
	}
	ctrl, err := boot.Build(ctx, boot.Options{
		Model:         entry.Runtime.Model,
		RequireKey:    true,
		Sink:          sink,
		WorkspaceRoot: workspaceRoot,
		SessionDir:    d.sessionDir,
	})
	if err != nil {
		return nil, err
	}
	ctrl.EnableInteractiveApproval()
	ctrl.Resume(sess, entry.Path)
	return ctrl, nil
}

func (d *Daemon) handleRunEvent(sessionID string, e event.Event) {
	switch e.Kind {
	case event.Usage:
		d.recordModelUsage(sessionID, e)
	case event.ApprovalRequest:
		d.recordWait(sessionID, agent.RuntimeWaitMeta{
			Kind:       "approval",
			Reason:     "approval required",
			ApprovalID: e.Approval.ID,
			Tool:       e.Approval.Tool,
			Subject:    e.Approval.Subject,
			Since:      time.Now().UTC(),
		}, e)
	case event.AskRequest:
		d.recordWait(sessionID, agent.RuntimeWaitMeta{
			Kind:   "ask",
			Reason: "user answer required",
			AskID:  e.Ask.ID,
			Since:  time.Now().UTC(),
		}, e)
	case event.TurnDone:
		if e.Err != nil {
			d.logger.Warn("daemon run turn finished with error", "session", sessionID, "err", e.Err)
		}
	}
}

func (d *Daemon) recordModelUsage(sessionID string, e event.Event) {
	if e.Usage == nil {
		return
	}
	d.mu.RLock()
	entry, ok := d.registry[sessionID]
	active := d.activeRuns[sessionID]
	var path string
	var runtime agent.RuntimeMeta
	var intent RunIntent
	if ok {
		path = entry.Path
		runtime = entry.Runtime
	}
	if active != nil {
		intent = active.Intent
	}
	d.mu.RUnlock()
	if !ok {
		return
	}
	if loaded, found, err := agent.LoadRuntimeMeta(path); err == nil && found {
		runtime = loaded
	}
	usage := e.Usage
	var cost float64
	var currency string
	if e.Pricing != nil {
		cost = e.Pricing.Cost(usage)
		currency = e.Pricing.Symbol()
	}
	recordModelBudgetUsage(&runtime, cost, currency, time.Now().UTC())
	if err := saveRuntimeMeta(path, runtime); err != nil {
		d.logger.Warn("daemon: persist model budget usage", "session", sessionID, "err", err)
	}
	d.mu.Lock()
	if entry := d.registry[sessionID]; entry != nil {
		entry.Runtime.Budget = runtime.Budget
	}
	d.mu.Unlock()
	timeline := agent.RuntimeTimelineEvent{
		Type:       "model_usage",
		Source:     firstNonEmpty(intent.Source, "model"),
		Reason:     intent.Reason,
		EventID:    intent.EventID,
		Step:       "model",
		Model:      runtime.Model,
		RunStatus:  runtime.Run.Status,
		GoalStatus: runtime.Goal.Status,
		Prompt:     usage.PromptTokens,
		Completion: usage.CompletionTokens,
		Total:      usage.TotalTokens,
		CacheHit:   usage.CacheHitTokens,
		CacheMiss:  usage.CacheMissTokens,
		Reasoning:  usage.ReasoningTokens,
		Finish:     usage.FinishReason,
	}
	timeline.Cost = cost
	timeline.Currency = currency
	d.appendTimeline(path, timeline)
}

func applyModelBudgetTurnLimit(ctrl *control.Controller, runtime agent.RuntimeMeta) {
	if ctrl == nil || runtime.Budget.DailyModelCallLimit <= 0 {
		return
	}
	budget := runtime.Budget
	resetBudgetWindowIfNeeded(&budget, time.Now().UTC())
	remaining := budget.DailyModelCallLimit - budget.DailyModelCalls
	if remaining <= 0 {
		return
	}
	cap := runtime.Goal.Turns + remaining
	if runtime.Budget.MaxGoalAutoTurns > 0 && runtime.Budget.MaxGoalAutoTurns < cap {
		return
	}
	if runtime.Budget.MaxGoalAutoTurns == 0 && cap >= control.DefaultMaxGoalAutoTurns {
		return
	}
	ctrl.SetGoalAutoTurnLimit(cap)
}

func (d *Daemon) recordWait(sessionID string, wait agent.RuntimeWaitMeta, e event.Event) {
	d.mu.Lock()
	entry, ok := d.registry[sessionID]
	active := d.activeRuns[sessionID]
	if ok {
		entry.Runtime.Wait = wait
		entry.Runtime.Run.Status = waitRunStatus(wait.Kind)
	}
	if active != nil {
		if e.Kind == event.ApprovalRequest {
			active.Approvals[e.Approval.ID] = e.Approval
		}
		if e.Kind == event.AskRequest {
			active.Asks[e.Ask.ID] = e.Ask
		}
	}
	var runtime agent.RuntimeMeta
	var path string
	if ok {
		runtime = entry.Runtime
		path = entry.Path
	}
	d.mu.Unlock()
	if ok {
		if err := saveRuntimeMeta(path, runtime); err != nil {
			d.logger.Warn("daemon: persist wait state", "session", sessionID, "err", err)
		}
		d.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       "wait_started",
			Step:       "deterministic",
			RunStatus:  runtime.Run.Status,
			GoalStatus: runtime.Goal.Status,
			WaitKind:   wait.Kind,
			WaitID:     firstNonEmpty(wait.ApprovalID, wait.AskID, wait.EventID),
			Tool:       wait.Tool,
			Subject:    wait.Subject,
			Reason:     wait.Reason,
			EventID:    wait.EventID,
		})
	}
}

func (d *Daemon) finishIntent(sessionID, fallbackStatus string, runErr error) {
	d.mu.Lock()
	entry, ok := d.registry[sessionID]
	delete(d.activeRuns, sessionID)
	var path string
	if ok {
		path = entry.Path
	}
	d.mu.Unlock()

	if !ok {
		return
	}
	if loaded, found, err := agent.LoadRuntimeMeta(path); err == nil && found {
		d.mu.Lock()
		if entry := d.registry[sessionID]; entry != nil {
			entry.Runtime = loaded
		}
		d.mu.Unlock()
		d.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       "run_finished",
			Step:       "deterministic",
			RunStatus:  loaded.Run.Status,
			GoalStatus: loaded.Goal.Status,
			Error:      errorString(runErr),
		})
		return
	}
	if fallbackStatus == "" {
		return
	}
	d.mu.Lock()
	entry = d.registry[sessionID]
	if entry != nil {
		entry.Runtime.Run.Status = fallbackStatus
		if runErr != nil {
			entry.Runtime.Run.LastError = runErr.Error()
		}
	}
	var runtime agent.RuntimeMeta
	if entry != nil {
		runtime = entry.Runtime
	}
	d.mu.Unlock()
	if entry != nil {
		if err := saveRuntimeMeta(path, runtime); err != nil {
			slog.Warn("daemon: persist finished intent", "err", err, "session", sessionID)
		}
		d.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       "run_finished",
			Step:       "deterministic",
			RunStatus:  runtime.Run.Status,
			GoalStatus: runtime.Goal.Status,
			Error:      errorString(runErr),
		})
	}
}

func (d *Daemon) appendTimeline(path string, e agent.RuntimeTimelineEvent) {
	if path == "" {
		return
	}
	if err := agent.AppendRuntimeTimeline(path, e); err != nil {
		d.logger.Warn("daemon: append runtime timeline", "err", err)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
