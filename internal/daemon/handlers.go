package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

var startTime = time.Now()

// handleStatus returns daemon health info.
func (d *Daemon) handleStatus(w http.ResponseWriter, r *http.Request) {
	d.mu.RLock()
	sessions := len(d.registry)
	d.mu.RUnlock()

	resp := StatusResponse{
		Status:   "running",
		Addr:     d.addr,
		Sessions: sessions,
		Uptime:   time.Since(startTime).Round(time.Second).String(),
		PID:      os.Getpid(),
	}
	if d.fileWatcher != nil {
		stats := d.fileWatcher.Stats()
		resp.FileWatcher = &stats
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleSessions lists all tracked sessions with their runtime state.
func (d *Daemon) handleSessions(w http.ResponseWriter, r *http.Request) {
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	workspaceRoot := strings.TrimSpace(r.URL.Query().Get("workspace_root"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if !validSessionScopeFilter(scope) {
		http.Error(w, `{"error":"invalid scope"}`, http.StatusBadRequest)
		return
	}
	if !validSessionStatusFilter(status) {
		http.Error(w, `{"error":"invalid status"}`, http.StatusBadRequest)
		return
	}

	d.mu.RLock()
	views := make([]SessionView, 0, len(d.registry))
	for _, entry := range d.registry {
		_, active := d.activeRuns[entry.ID]
		view := sessionViewForEntry(entry, active)
		if sessionViewMatchesFilters(view, scope, workspaceRoot, status) {
			views = append(views, view)
		}
	}
	d.mu.RUnlock()
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SessionsResponse{Sessions: views})
}

func sessionViewForEntry(entry *SessionEntry, active bool) SessionView {
	meta, _, _ := agent.LoadBranchMeta(entry.Path)
	scope := meta.DefaultScope()
	workspaceRoot := firstNonEmpty(entry.Runtime.WorkspaceRoot, meta.WorkspaceRoot)
	if scope == "global" && strings.TrimSpace(workspaceRoot) != "" {
		scope = "project"
	}
	runStatus := agent.NormalizeRunStatus(entry.Runtime.Run.Status)
	nextWakeupAt := timePtr(entry.Runtime.Scheduler.NextWakeupAt)
	budget := entry.Runtime.Budget
	return SessionView{
		ID:                  entry.ID,
		Path:                entry.Path,
		GoalText:            entry.Runtime.Goal.Text,
		GoalStatus:          entry.Runtime.Goal.Status,
		RunStatus:           runStatus,
		WaitKind:            entry.Runtime.Wait.Kind,
		WaitReason:          entry.Runtime.Wait.Reason,
		WaitID:              firstNonEmpty(entry.Runtime.Wait.ApprovalID, entry.Runtime.Wait.AskID, entry.Runtime.Wait.EventID),
		WaitTool:            entry.Runtime.Wait.Tool,
		WaitSubject:         entry.Runtime.Wait.Subject,
		Active:              active,
		Scope:               scope,
		WorkspaceRoot:       workspaceRoot,
		TopicID:             meta.TopicID,
		TopicTitle:          meta.TopicTitle,
		NextWakeupAt:        nextWakeupAt,
		DailyWakeupLimit:    budget.DailyWakeupLimit,
		DailyWakeups:        budget.DailyWakeups,
		MaxGoalAutoTurns:    budget.MaxGoalAutoTurns,
		DailyModelCallLimit: budget.DailyModelCallLimit,
		DailyModelCalls:     budget.DailyModelCalls,
		DailyModelCostLimit: budget.DailyModelCostLimit,
		DailyModelCost:      budget.DailyModelCost,
		ModelCostCurrency:   budget.ModelCostCurrency,
		BudgetBlockedReason: budget.LastBlockedReason,
		Scheduled:           entry.Runtime.Scheduler.Enabled,
		Watched:             entry.Runtime.FileWatch.Enabled && len(entry.Runtime.FileWatch.Paths) > 0,
	}
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	tt := t
	return &tt
}

func validSessionScopeFilter(scope string) bool {
	switch scope {
	case "", "all", "global", "project":
		return true
	default:
		return false
	}
}

func validSessionStatusFilter(status string) bool {
	switch status {
	case "", "all", "active", "running", "waiting", "blocked", "scheduled", "watched":
		return true
	default:
		return agent.IsKnownRunStatus(status)
	}
}

func sessionViewMatchesFilters(view SessionView, scope, workspaceRoot, status string) bool {
	switch scope {
	case "global":
		if view.Scope != "global" {
			return false
		}
	case "project":
		if view.Scope != "project" {
			return false
		}
		if strings.TrimSpace(workspaceRoot) != "" && !sameWorkspaceRoot(view.WorkspaceRoot, workspaceRoot) {
			return false
		}
	}
	switch status {
	case "", "all":
		return true
	case "active":
		return view.Active
	case "running":
		return view.Active || view.RunStatus == agent.RunStatusRunning || view.RunStatus == agent.RunStatusQueued
	case "waiting":
		return view.WaitKind != "" || strings.HasPrefix(view.RunStatus, "waiting_")
	case "blocked":
		return view.GoalStatus == "blocked" || view.RunStatus == agent.RunStatusBlocked || view.BudgetBlockedReason != ""
	case "scheduled":
		return view.Scheduled
	case "watched":
		return view.Watched
	default:
		return view.RunStatus == agent.NormalizeRunStatus(status)
	}
}

func (d *Daemon) handleTimeline(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = r.URL.Query().Get("session")
	}
	if sessionID == "" {
		http.Error(w, `{"error":"session_id required"}`, http.StatusBadRequest)
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			http.Error(w, `{"error":"invalid limit"}`, http.StatusBadRequest)
			return
		}
		limit = n
	}
	d.mu.RLock()
	entry, ok := d.registry[sessionID]
	d.mu.RUnlock()
	if !ok {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}
	events, _, err := agent.LoadRuntimeTimeline(entry.Path, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"load timeline failed: %s"}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TimelineResponse{SessionID: sessionID, Events: events})
}

func (d *Daemon) handleApprovals(w http.ResponseWriter, r *http.Request) {
	items := d.approvalDeskItems()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApprovalDeskResponse{Items: items})
}

func (d *Daemon) handleBudgets(w http.ResponseWriter, r *http.Request) {
	views, err := d.budgetAggregates(time.Now().UTC())
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"load budgets failed: %s"}`, err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(BudgetAggregatesResponse{Budgets: views})
}

func (d *Daemon) approvalDeskItems() []ApprovalDeskItem {
	d.mu.RLock()
	defer d.mu.RUnlock()

	items := make([]ApprovalDeskItem, 0)
	seen := make(map[string]bool)
	for _, entry := range d.registry {
		active := d.activeRuns[entry.ID]
		if active != nil {
			for _, approval := range active.Approvals {
				wait := matchingWait(entry.Runtime.Wait, "approval", approval.ID)
				addApprovalDeskItem(&items, seen, ApprovalDeskItem{
					SessionID:  entry.ID,
					Kind:       "approval",
					ID:         approval.ID,
					Tool:       firstNonEmpty(approval.Tool, wait.Tool),
					Subject:    firstNonEmpty(approval.Subject, wait.Subject),
					Reason:     wait.Reason,
					GoalText:   entry.Runtime.Goal.Text,
					GoalStatus: entry.Runtime.Goal.Status,
					RunStatus:  entry.Runtime.Run.Status,
					Active:     active.Control != nil,
					Since:      wait.Since,
				})
			}
			for _, ask := range active.Asks {
				wait := matchingWait(entry.Runtime.Wait, "ask", ask.ID)
				addApprovalDeskItem(&items, seen, ApprovalDeskItem{
					SessionID:  entry.ID,
					Kind:       "ask",
					ID:         ask.ID,
					Subject:    wait.Subject,
					Reason:     wait.Reason,
					GoalText:   entry.Runtime.Goal.Text,
					GoalStatus: entry.Runtime.Goal.Status,
					RunStatus:  entry.Runtime.Run.Status,
					Active:     active.Control != nil,
					Since:      wait.Since,
					Questions:  approvalDeskQuestions(ask.Questions),
				})
			}
		}

		wait := entry.Runtime.Wait
		switch wait.Kind {
		case "approval":
			addApprovalDeskItem(&items, seen, ApprovalDeskItem{
				SessionID:  entry.ID,
				Kind:       "approval",
				ID:         wait.ApprovalID,
				Tool:       wait.Tool,
				Subject:    wait.Subject,
				Reason:     wait.Reason,
				GoalText:   entry.Runtime.Goal.Text,
				GoalStatus: entry.Runtime.Goal.Status,
				RunStatus:  entry.Runtime.Run.Status,
				Since:      wait.Since,
			})
		case "ask":
			addApprovalDeskItem(&items, seen, ApprovalDeskItem{
				SessionID:  entry.ID,
				Kind:       "ask",
				ID:         wait.AskID,
				Subject:    wait.Subject,
				Reason:     wait.Reason,
				GoalText:   entry.Runtime.Goal.Text,
				GoalStatus: entry.Runtime.Goal.Status,
				RunStatus:  entry.Runtime.Run.Status,
				Since:      wait.Since,
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Active != items[j].Active {
			return items[i].Active
		}
		if items[i].SessionID != items[j].SessionID {
			return items[i].SessionID < items[j].SessionID
		}
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].ID < items[j].ID
	})
	return items
}

func addApprovalDeskItem(items *[]ApprovalDeskItem, seen map[string]bool, item ApprovalDeskItem) {
	key := item.SessionID + "\x00" + item.Kind + "\x00" + item.ID
	if seen[key] {
		return
	}
	seen[key] = true
	*items = append(*items, item)
}

func matchingWait(wait agent.RuntimeWaitMeta, kind, id string) agent.RuntimeWaitMeta {
	if wait.Kind != kind {
		return agent.RuntimeWaitMeta{}
	}
	if id == "" || firstNonEmpty(wait.ApprovalID, wait.AskID, wait.EventID) == id {
		return wait
	}
	return agent.RuntimeWaitMeta{}
}

func approvalDeskQuestions(questions []event.AskQuestion) []ApprovalDeskQuestion {
	if len(questions) == 0 {
		return nil
	}
	out := make([]ApprovalDeskQuestion, 0, len(questions))
	for _, q := range questions {
		options := make([]ApprovalDeskOption, 0, len(q.Options))
		for _, opt := range q.Options {
			options = append(options, ApprovalDeskOption{Label: opt.Label, Description: opt.Description})
		}
		out = append(out, ApprovalDeskQuestion{
			ID:      q.ID,
			Header:  q.Header,
			Prompt:  q.Prompt,
			Options: options,
			Multi:   q.Multi,
		})
	}
	return out
}

// handleContinueGoal triggers goal continuation for a session.
// Body: {"session_id": "...", "reason": "bot|user|cron"}
func (d *Daemon) handleContinueGoal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, `{"error":"session_id required"}`, http.StatusBadRequest)
		return
	}
	if req.Reason == "" {
		req.Reason = "api"
	}

	d.mu.RLock()
	entry, ok := d.registry[req.SessionID]
	d.mu.RUnlock()
	if !ok {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	d.mu.Lock()
	if _, running := d.activeRuns[req.SessionID]; running {
		d.mu.Unlock()
		http.Error(w, `{"error":"session already running"}`, http.StatusConflict)
		return
	}
	entry.Runtime.Run.Status = agent.RunStatusQueued
	entry.Runtime.Run.LastWakeupReason = req.Reason
	entry.Runtime.Run.ResumeCount++
	entry.Runtime.Wait = agent.RuntimeWaitMeta{}
	runtime := entry.Runtime
	path := entry.Path
	d.mu.Unlock()

	if err := saveRuntimeMeta(path, runtime); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, err), http.StatusInternalServerError)
		return
	}
	d.enqueueIntent(RunIntent{
		SessionID:   req.SessionID,
		SessionPath: path,
		Source:      "api",
		Reason:      req.Reason,
	})

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ok":true,"session_id":%q,"status":"queued"}`, req.SessionID)
}

// handleStop stops a running session's goal.
// Body: {"session_id": "..."}
func (d *Daemon) handleStop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, `{"error":"session_id required"}`, http.StatusBadRequest)
		return
	}

	d.mu.Lock()
	entry, ok := d.registry[req.SessionID]
	active := d.activeRuns[req.SessionID]
	if ok {
		if active != nil && active.Cancel != nil {
			active.Cancel()
		}
		entry.Runtime.Run.Status = agent.RunStatusStopped
		entry.Runtime.Goal.Status = "stopped"
		entry.Runtime.Wait = agent.RuntimeWaitMeta{}
	}
	var runtime agent.RuntimeMeta
	var path string
	if ok {
		runtime = entry.Runtime
		path = entry.Path
	}
	d.mu.Unlock()
	if !ok {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	if err := saveRuntimeMeta(path, runtime); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, err), http.StatusInternalServerError)
		return
	}
	d.appendTimeline(path, agent.RuntimeTimelineEvent{
		Type:       "stopped",
		Source:     "api",
		Step:       "deterministic",
		RunStatus:  runtime.Run.Status,
		GoalStatus: runtime.Goal.Status,
	})

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ok":true,"session_id":%q,"status":"stopped"}`, req.SessionID)
}

// handleSchedule sets or clears a cron schedule for a session or scope.
// Body: {"session_id": "...", "daily_at": "09:00", "timezone":"Asia/Shanghai", "interval": "1h", "enabled": true}
// Body: {"scope":"project","workspace_root":"/repo", ...} or {"scope":"global", ...}
// All schedule fields are optional; omitted fields are left unchanged.
// Set enabled=false to disable without clearing the schedule.
func (d *Daemon) handleSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID     string  `json:"session_id"`
		Scope         string  `json:"scope"`          // global|project; optional alternative to session_id
		WorkspaceRoot string  `json:"workspace_root"` // required for project scope
		DailyAt       *string `json:"daily_at"`       // "HH:MM" or "" to clear
		Timezone      *string `json:"timezone"`       // IANA timezone or "" to clear
		Interval      *string `json:"interval"`       // duration string or "" to clear
		Enabled       *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Scope = strings.TrimSpace(req.Scope)
	req.WorkspaceRoot = strings.TrimSpace(req.WorkspaceRoot)
	if req.SessionID == "" && req.Scope == "" {
		http.Error(w, `{"error":"session_id or scope required"}`, http.StatusBadRequest)
		return
	}
	if req.SessionID != "" && req.Scope != "" {
		http.Error(w, `{"error":"session_id and scope are mutually exclusive"}`, http.StatusBadRequest)
		return
	}
	if req.Scope != "" && req.Scope != "global" && req.Scope != "project" {
		http.Error(w, `{"error":"scope must be global or project"}`, http.StatusBadRequest)
		return
	}
	if req.Scope == "project" && req.WorkspaceRoot == "" {
		http.Error(w, `{"error":"workspace_root required for project scope"}`, http.StatusBadRequest)
		return
	}
	if req.Timezone != nil && *req.Timezone != "" {
		if _, err := time.LoadLocation(*req.Timezone); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid timezone: %s"}`, err), http.StatusBadRequest)
			return
		}
	}
	var interval *time.Duration
	if req.Interval != nil && *req.Interval != "" {
		dur, err := time.ParseDuration(*req.Interval)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid interval: %s"}`, err), http.StatusBadRequest)
			return
		}
		if dur < time.Second {
			http.Error(w, `{"error":"interval must be at least 1s"}`, http.StatusBadRequest)
			return
		}
		interval = &dur
	}

	d.mu.Lock()
	targets := d.scheduleTargetsLocked(req.SessionID, req.Scope, req.WorkspaceRoot)
	if len(targets) == 0 {
		d.mu.Unlock()
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	now := time.Now()
	snapshots := make([]SessionEntry, 0, len(targets))
	for _, entry := range targets {
		applyScheduleRequest(&entry.Runtime.Scheduler, req.DailyAt, req.Timezone, req.Interval, interval, req.Enabled)
		if entry.Runtime.Scheduler.Enabled && d.scheduler != nil {
			entry.Runtime.Scheduler.NextWakeupAt = d.scheduler.computeNextWakeup(entry.Runtime.Scheduler, now)
		} else if !entry.Runtime.Scheduler.Enabled {
			entry.Runtime.Scheduler.NextWakeupAt = time.Time{}
		}
		snapshots = append(snapshots, *entry)
	}
	d.mu.Unlock()

	for _, entry := range snapshots {
		if err := agent.SaveRuntimeMeta(entry.Path, entry.Runtime); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	sessionIDs := make([]string, 0, len(snapshots))
	for _, entry := range snapshots {
		sessionIDs = append(sessionIDs, entry.ID)
	}
	first := snapshots[0]
	resp := map[string]interface{}{
		"ok":             true,
		"session_id":     first.ID,
		"session_ids":    sessionIDs,
		"scope":          req.Scope,
		"workspace_root": req.WorkspaceRoot,
		"updated":        len(snapshots),
		"enabled":        first.Runtime.Scheduler.Enabled,
		"daily_at":       first.Runtime.Scheduler.DailyAt,
		"timezone":       first.Runtime.Scheduler.Timezone,
		"interval":       first.Runtime.Scheduler.Interval.String(),
		"next_wakeup_at": first.Runtime.Scheduler.NextWakeupAt.Format(time.RFC3339),
	}
	json.NewEncoder(w).Encode(resp)
}

func applyScheduleRequest(sched *agent.RuntimeSchedMeta, dailyAt, timezone, intervalRaw *string, interval *time.Duration, enabled *bool) {
	if dailyAt != nil {
		sched.DailyAt = *dailyAt
	}
	if timezone != nil {
		sched.Timezone = *timezone
	}
	if intervalRaw != nil {
		if *intervalRaw == "" {
			sched.Interval = 0
		} else if interval != nil {
			sched.Interval = *interval
		}
	}
	if enabled != nil {
		sched.Enabled = *enabled
	}
}

func (d *Daemon) scheduleTargetsLocked(sessionID, scope, workspaceRoot string) []*SessionEntry {
	if sessionID != "" {
		if entry := d.registry[sessionID]; entry != nil {
			return []*SessionEntry{entry}
		}
		return nil
	}
	targets := make([]*SessionEntry, 0)
	for _, entry := range d.registry {
		if scheduleEntryMatchesScope(entry, scope, workspaceRoot) {
			targets = append(targets, entry)
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	return targets
}

func scheduleEntryMatchesScope(entry *SessionEntry, scope, workspaceRoot string) bool {
	meta, _, _ := agent.LoadBranchMeta(entry.Path)
	entryScope := meta.DefaultScope()
	root := firstNonEmpty(entry.Runtime.WorkspaceRoot, meta.WorkspaceRoot)
	if entryScope == "global" && strings.TrimSpace(root) != "" {
		entryScope = "project"
	}
	switch scope {
	case "global":
		return entryScope == "global"
	case "project":
		return entryScope == "project" && sameWorkspaceRoot(root, workspaceRoot)
	default:
		return false
	}
}

func sameWorkspaceRoot(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return a == b
	}
	if abs, err := filepath.Abs(a); err == nil {
		a = abs
	}
	if abs, err := filepath.Abs(b); err == nil {
		b = abs
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// handleBudget sets or resets the automatic wakeup budget for a session.
// Body: {"session_id":"...","daily_wakeup_limit":10,"max_goal_auto_turns":20,"reset":true}
func (d *Daemon) handleBudget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID           string   `json:"session_id"`
		Scope               string   `json:"scope"`
		WorkspaceRoot       string   `json:"workspace_root"`
		DailyWakeupLimit    *int     `json:"daily_wakeup_limit"`
		MaxGoalAutoTurns    *int     `json:"max_goal_auto_turns"`
		DailyModelCallLimit *int     `json:"daily_model_call_limit"`
		DailyModelCostLimit *float64 `json:"daily_model_cost_limit"`
		Reset               bool     `json:"reset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Scope = strings.TrimSpace(req.Scope)
	req.WorkspaceRoot = strings.TrimSpace(req.WorkspaceRoot)
	if req.SessionID == "" && req.Scope == "" {
		http.Error(w, `{"error":"session_id or scope required"}`, http.StatusBadRequest)
		return
	}
	if req.SessionID != "" && req.Scope != "" {
		http.Error(w, `{"error":"session_id and scope are mutually exclusive"}`, http.StatusBadRequest)
		return
	}
	if req.Scope != "" && req.Scope != "global" && req.Scope != "project" {
		http.Error(w, `{"error":"scope must be global or project"}`, http.StatusBadRequest)
		return
	}
	if req.Scope == "project" && req.WorkspaceRoot == "" {
		http.Error(w, `{"error":"workspace_root required for project scope"}`, http.StatusBadRequest)
		return
	}
	if req.DailyWakeupLimit != nil && *req.DailyWakeupLimit < 0 {
		http.Error(w, `{"error":"daily_wakeup_limit must be >= 0"}`, http.StatusBadRequest)
		return
	}
	if req.MaxGoalAutoTurns != nil && *req.MaxGoalAutoTurns < 0 {
		http.Error(w, `{"error":"max_goal_auto_turns must be >= 0"}`, http.StatusBadRequest)
		return
	}
	if req.DailyModelCallLimit != nil && *req.DailyModelCallLimit < 0 {
		http.Error(w, `{"error":"daily_model_call_limit must be >= 0"}`, http.StatusBadRequest)
		return
	}
	if req.DailyModelCostLimit != nil && *req.DailyModelCostLimit < 0 {
		http.Error(w, `{"error":"daily_model_cost_limit must be >= 0"}`, http.StatusBadRequest)
		return
	}
	if req.Scope != "" {
		d.handleScopeBudget(w, req.Scope, req.WorkspaceRoot, req.DailyWakeupLimit, req.MaxGoalAutoTurns, req.DailyModelCallLimit, req.DailyModelCostLimit, req.Reset)
		return
	}

	now := time.Now().UTC()
	d.mu.Lock()
	entry, ok := d.registry[req.SessionID]
	if !ok {
		d.mu.Unlock()
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}
	if req.DailyWakeupLimit != nil {
		entry.Runtime.Budget.DailyWakeupLimit = *req.DailyWakeupLimit
		if entry.Runtime.Budget.WindowStartedAt.IsZero() {
			entry.Runtime.Budget.WindowStartedAt = budgetWindowStart(now)
		}
	}
	if req.MaxGoalAutoTurns != nil {
		entry.Runtime.Budget.MaxGoalAutoTurns = *req.MaxGoalAutoTurns
		if active := d.activeRuns[req.SessionID]; active != nil && active.Control != nil {
			active.Control.SetGoalAutoTurnLimit(*req.MaxGoalAutoTurns)
		}
	}
	if req.DailyModelCallLimit != nil {
		entry.Runtime.Budget.DailyModelCallLimit = *req.DailyModelCallLimit
		if entry.Runtime.Budget.WindowStartedAt.IsZero() {
			entry.Runtime.Budget.WindowStartedAt = budgetWindowStart(now)
		}
	}
	if req.DailyModelCostLimit != nil {
		entry.Runtime.Budget.DailyModelCostLimit = *req.DailyModelCostLimit
		if entry.Runtime.Budget.WindowStartedAt.IsZero() {
			entry.Runtime.Budget.WindowStartedAt = budgetWindowStart(now)
		}
	}
	if req.Reset {
		entry.Runtime.Budget.DailyWakeups = 0
		entry.Runtime.Budget.DailyModelCalls = 0
		entry.Runtime.Budget.DailyModelCost = 0
		entry.Runtime.Budget.ModelCostCurrency = ""
		entry.Runtime.Budget.WindowStartedAt = budgetWindowStart(now)
		entry.Runtime.Budget.LastBlockedAt = time.Time{}
		entry.Runtime.Budget.LastBlockedReason = ""
	}
	runtime := entry.Runtime
	path := entry.Path
	d.mu.Unlock()

	if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, err), http.StatusInternalServerError)
		return
	}
	d.appendTimeline(path, agent.RuntimeTimelineEvent{
		Type:       "budget_configured",
		Source:     "api",
		Step:       "deterministic",
		RunStatus:  runtime.Run.Status,
		GoalStatus: runtime.Goal.Status,
		Message:    fmt.Sprintf("daily_wakeup_limit=%d daily_wakeups=%d max_goal_auto_turns=%d daily_model_call_limit=%d daily_model_calls=%d daily_model_cost_limit=%.6f daily_model_cost=%.6f", runtime.Budget.DailyWakeupLimit, runtime.Budget.DailyWakeups, runtime.Budget.MaxGoalAutoTurns, runtime.Budget.DailyModelCallLimit, runtime.Budget.DailyModelCalls, runtime.Budget.DailyModelCostLimit, runtime.Budget.DailyModelCost),
	})

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"ok":                     true,
		"session_id":             req.SessionID,
		"daily_wakeup_limit":     runtime.Budget.DailyWakeupLimit,
		"max_goal_auto_turns":    runtime.Budget.MaxGoalAutoTurns,
		"daily_model_call_limit": runtime.Budget.DailyModelCallLimit,
		"daily_model_cost_limit": runtime.Budget.DailyModelCostLimit,
		"daily_wakeups":          runtime.Budget.DailyWakeups,
		"daily_model_calls":      runtime.Budget.DailyModelCalls,
		"daily_model_cost":       runtime.Budget.DailyModelCost,
		"model_cost_currency":    runtime.Budget.ModelCostCurrency,
		"window_started_at":      runtime.Budget.WindowStartedAt.Format(time.RFC3339),
	}
	json.NewEncoder(w).Encode(resp)
}

func (d *Daemon) handleScopeBudget(w http.ResponseWriter, scope, workspaceRoot string, dailyWakeupLimit, maxGoalAutoTurns, dailyModelCallLimit *int, dailyModelCostLimit *float64, reset bool) {
	if dailyWakeupLimit != nil || maxGoalAutoTurns != nil {
		http.Error(w, `{"error":"scope budgets support daily_model_call_limit and daily_model_cost_limit only"}`, http.StatusBadRequest)
		return
	}
	if dailyModelCallLimit == nil && dailyModelCostLimit == nil && !reset {
		http.Error(w, `{"error":"daily_model_call_limit, daily_model_cost_limit, or reset required"}`, http.StatusBadRequest)
		return
	}
	cfg, err := d.loadScopeBudgetConfig()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"load budgets failed: %s"}`, err), http.StatusInternalServerError)
		return
	}
	quota := ScopeBudgetQuota{Scope: scope, WorkspaceRoot: workspaceRoot}
	for _, existing := range cfg.Quotas {
		existing = normalizeScopeBudgetQuota(existing)
		if existing.Scope == scope && sameWorkspaceRoot(existing.WorkspaceRoot, workspaceRoot) {
			quota = existing
			break
		}
	}
	if dailyModelCallLimit != nil {
		quota.DailyModelCallLimit = *dailyModelCallLimit
	}
	if dailyModelCostLimit != nil {
		quota.DailyModelCostLimit = *dailyModelCostLimit
	}
	upsertScopeBudgetQuota(&cfg, quota)
	if err := d.saveScopeBudgetConfig(cfg); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"save budgets failed: %s"}`, err), http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	var snapshots []SessionEntry
	if reset {
		d.mu.Lock()
		for _, entry := range d.registry {
			if !entryMatchesAggregate(entry, scope, workspaceRoot) {
				continue
			}
			entry.Runtime.Budget.DailyModelCalls = 0
			entry.Runtime.Budget.DailyModelCost = 0
			entry.Runtime.Budget.ModelCostCurrency = ""
			entry.Runtime.Budget.WindowStartedAt = budgetWindowStart(now)
			entry.Runtime.Budget.LastBlockedAt = time.Time{}
			entry.Runtime.Budget.LastBlockedReason = ""
			snapshots = append(snapshots, *entry)
		}
		d.mu.Unlock()
		for _, entry := range snapshots {
			if err := agent.SaveRuntimeMeta(entry.Path, entry.Runtime); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, err), http.StatusInternalServerError)
				return
			}
		}
	}
	views, err := d.budgetAggregates(now)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"load budgets failed: %s"}`, err), http.StatusInternalServerError)
		return
	}
	var selected BudgetAggregateView
	for _, view := range views {
		if view.Scope == scope && sameWorkspaceRoot(view.WorkspaceRoot, workspaceRoot) {
			selected = view
			break
		}
	}
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"ok":             true,
		"scope":          selected.Scope,
		"workspace_root": selected.WorkspaceRoot,
		"session_count":  selected.SessionCount,
		"reset":          reset,
		"budget":         selected,
	}
	json.NewEncoder(w).Encode(resp)
}

// handleWaitEvent sets or clears an external event wait condition.
// Body: {"session_id":"...","event_source":"github.workflow_run","event_id":"...","event_status":"completed","event_conclusion":"success","reason":"waiting for CI","subject":"PR #42","clear":false}
func (d *Daemon) handleWaitEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID       string `json:"session_id"`
		EventSource     string `json:"event_source"`
		EventID         string `json:"event_id"`
		EventStatus     string `json:"event_status"`
		EventConclusion string `json:"event_conclusion"`
		Reason          string `json:"reason"`
		Subject         string `json:"subject"`
		Clear           bool   `json:"clear"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, `{"error":"session_id required"}`, http.StatusBadRequest)
		return
	}
	if !req.Clear && req.EventSource == "" && req.EventID == "" {
		http.Error(w, `{"error":"event_source or event_id required"}`, http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	d.mu.Lock()
	entry, ok := d.registry[req.SessionID]
	if !ok {
		d.mu.Unlock()
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}
	if _, active := d.activeRuns[req.SessionID]; active {
		d.mu.Unlock()
		http.Error(w, `{"error":"session already running"}`, http.StatusConflict)
		return
	}
	action := "wait_started"
	if req.Clear {
		entry.Runtime.Wait = agent.RuntimeWaitMeta{}
		if entry.Runtime.Run.Status == agent.RunStatusWaitingEvent {
			entry.Runtime.Run.Status = agent.RunStatusIdle
		}
		action = "wait_cleared"
	} else {
		reason := req.Reason
		if reason == "" {
			reason = "waiting for external event"
		}
		entry.Runtime.Wait = agent.RuntimeWaitMeta{
			Kind:            "event",
			Reason:          reason,
			EventSource:     strings.TrimSpace(req.EventSource),
			EventID:         strings.TrimSpace(req.EventID),
			EventStatus:     strings.TrimSpace(req.EventStatus),
			EventConclusion: strings.TrimSpace(req.EventConclusion),
			Subject:         strings.TrimSpace(req.Subject),
			Since:           now,
		}
		entry.Runtime.Run.Status = agent.RunStatusWaitingEvent
	}
	runtime := entry.Runtime
	path := entry.Path
	d.mu.Unlock()

	if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, err), http.StatusInternalServerError)
		return
	}
	d.appendTimeline(path, agent.RuntimeTimelineEvent{
		Type:       action,
		Source:     "api",
		Step:       "deterministic",
		RunStatus:  runtime.Run.Status,
		GoalStatus: runtime.Goal.Status,
		WaitKind:   runtime.Wait.Kind,
		WaitID:     runtime.Wait.EventID,
		Subject:    runtime.Wait.Subject,
		Reason:     runtime.Wait.Reason,
		EventID:    runtime.Wait.EventID,
	})

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"ok":               true,
		"session_id":       req.SessionID,
		"run_status":       runtime.Run.Status,
		"wait_kind":        runtime.Wait.Kind,
		"event_source":     runtime.Wait.EventSource,
		"event_id":         runtime.Wait.EventID,
		"event_status":     runtime.Wait.EventStatus,
		"event_conclusion": runtime.Wait.EventConclusion,
	}
	json.NewEncoder(w).Encode(resp)
}

// handleWaitTime sets or clears a time wait condition.
// Body: {"session_id":"...","until":"2026-06-13T10:00:00Z","after":"1h","reason":"waiting until CI window","subject":"release"}
func (d *Daemon) handleWaitTime(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		Until     string `json:"until"`
		After     string `json:"after"`
		Reason    string `json:"reason"`
		Subject   string `json:"subject"`
		Clear     bool   `json:"clear"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, `{"error":"session_id required"}`, http.StatusBadRequest)
		return
	}
	now := time.Now().UTC()
	var until time.Time
	if !req.Clear {
		var err error
		until, err = parseWaitUntil(req.Until, req.After, now)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusBadRequest)
			return
		}
	}

	d.mu.Lock()
	entry, ok := d.registry[req.SessionID]
	if !ok {
		d.mu.Unlock()
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}
	if _, active := d.activeRuns[req.SessionID]; active {
		d.mu.Unlock()
		http.Error(w, `{"error":"session already running"}`, http.StatusConflict)
		return
	}
	action := "wait_started"
	if req.Clear {
		entry.Runtime.Wait = agent.RuntimeWaitMeta{}
		if entry.Runtime.Run.Status == agent.RunStatusWaitingTime {
			entry.Runtime.Run.Status = agent.RunStatusIdle
		}
		action = "wait_cleared"
	} else {
		reason := strings.TrimSpace(req.Reason)
		if reason == "" {
			reason = "waiting until time"
		}
		subject := strings.TrimSpace(req.Subject)
		if subject == "" {
			subject = until.Format(time.RFC3339)
		}
		entry.Runtime.Wait = agent.RuntimeWaitMeta{
			Kind:    "time",
			Reason:  reason,
			Subject: subject,
			Until:   until,
			Since:   now,
		}
		entry.Runtime.Run.Status = agent.RunStatusWaitingTime
	}
	runtime := entry.Runtime
	path := entry.Path
	d.mu.Unlock()

	if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, err), http.StatusInternalServerError)
		return
	}
	d.appendTimeline(path, agent.RuntimeTimelineEvent{
		Type:       action,
		Source:     "api",
		Step:       "deterministic",
		RunStatus:  runtime.Run.Status,
		GoalStatus: runtime.Goal.Status,
		WaitKind:   runtime.Wait.Kind,
		Subject:    runtime.Wait.Subject,
		Reason:     runtime.Wait.Reason,
	})

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"ok":         true,
		"session_id": req.SessionID,
		"run_status": runtime.Run.Status,
		"wait_kind":  runtime.Wait.Kind,
		"until":      runtime.Wait.Until.Format(time.RFC3339),
	}
	json.NewEncoder(w).Encode(resp)
}

func parseWaitUntil(untilRaw, afterRaw string, now time.Time) (time.Time, error) {
	untilRaw = strings.TrimSpace(untilRaw)
	afterRaw = strings.TrimSpace(afterRaw)
	if untilRaw == "" && afterRaw == "" {
		return time.Time{}, fmt.Errorf("until or after required")
	}
	if untilRaw != "" && afterRaw != "" {
		return time.Time{}, fmt.Errorf("only one of until or after may be set")
	}
	if afterRaw != "" {
		dur, err := time.ParseDuration(afterRaw)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid after: %s", err)
		}
		if dur < time.Second {
			return time.Time{}, fmt.Errorf("after must be at least 1s")
		}
		return now.Add(dur).UTC(), nil
	}
	until, err := time.Parse(time.RFC3339, untilRaw)
	if err != nil {
		until, err = time.Parse(time.RFC3339Nano, untilRaw)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid until: %s", err)
	}
	return until.UTC(), nil
}

// handleWaitFile sets or clears a file-change wait condition and configures the
// underlying watcher needed to observe it.
// Body: {"session_id":"...","paths":["src/"],"ignore_patterns":["*.tmp"],"debounce":"3s","reason":"waiting for build output","subject":"dist/app.js"}
func (d *Daemon) handleWaitFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID      string   `json:"session_id"`
		Paths          []string `json:"paths"`
		IgnorePatterns []string `json:"ignore_patterns"`
		Debounce       string   `json:"debounce"`
		Reason         string   `json:"reason"`
		Subject        string   `json:"subject"`
		Clear          bool     `json:"clear"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, `{"error":"session_id required"}`, http.StatusBadRequest)
		return
	}
	if !req.Clear && len(req.Paths) == 0 {
		http.Error(w, `{"error":"paths required"}`, http.StatusBadRequest)
		return
	}
	debounce := 3 * time.Second
	if req.Debounce != "" {
		dur, err := time.ParseDuration(req.Debounce)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid debounce: %s"}`, err), http.StatusBadRequest)
			return
		}
		if dur < time.Second {
			http.Error(w, `{"error":"debounce must be at least 1s"}`, http.StatusBadRequest)
			return
		}
		debounce = dur
	}

	now := time.Now().UTC()
	d.mu.Lock()
	entry, ok := d.registry[req.SessionID]
	if !ok {
		d.mu.Unlock()
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}
	if _, active := d.activeRuns[req.SessionID]; active {
		d.mu.Unlock()
		http.Error(w, `{"error":"session already running"}`, http.StatusConflict)
		return
	}
	action := "wait_started"
	if req.Clear {
		entry.Runtime.Wait = agent.RuntimeWaitMeta{}
		entry.Runtime.FileWatch = agent.RuntimeWatchMeta{}
		if entry.Runtime.Run.Status == agent.RunStatusWaitingFile {
			entry.Runtime.Run.Status = agent.RunStatusIdle
		}
		action = "wait_cleared"
	} else {
		reason := strings.TrimSpace(req.Reason)
		if reason == "" {
			reason = "waiting for file change"
		}
		subject := strings.TrimSpace(req.Subject)
		if subject == "" {
			subject = strings.Join(req.Paths, ",")
		}
		paths := append([]string(nil), req.Paths...)
		watchPaths := fileWaitWatchPaths(paths, entry.Runtime.WorkspaceRoot)
		entry.Runtime.Wait = agent.RuntimeWaitMeta{
			Kind:      "file",
			Reason:    reason,
			Subject:   subject,
			FilePaths: paths,
			Since:     now,
		}
		entry.Runtime.Run.Status = agent.RunStatusWaitingFile
		entry.Runtime.FileWatch = agent.RuntimeWatchMeta{
			Paths:          watchPaths,
			IgnorePatterns: append([]string(nil), req.IgnorePatterns...),
			Debounce:       debounce,
			Enabled:        true,
		}
	}
	runtime := entry.Runtime
	path := entry.Path
	d.mu.Unlock()

	if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, err), http.StatusInternalServerError)
		return
	}
	d.mu.Lock()
	if entry := d.registry[req.SessionID]; entry != nil {
		entry.Runtime = runtime
	}
	d.mu.Unlock()
	if d.fileWatcher != nil {
		watchEntry := &SessionEntry{ID: req.SessionID, Path: path, Runtime: runtime}
		if runtime.FileWatch.Enabled {
			d.fileWatcher.Register(req.SessionID, fileWatchConfigForEntry(watchEntry))
		} else if req.Clear {
			d.fileWatcher.Unregister(req.SessionID)
		}
	}
	d.appendTimeline(path, agent.RuntimeTimelineEvent{
		Type:       action,
		Source:     "api",
		Step:       "deterministic",
		RunStatus:  runtime.Run.Status,
		GoalStatus: runtime.Goal.Status,
		WaitKind:   runtime.Wait.Kind,
		Subject:    runtime.Wait.Subject,
		Reason:     runtime.Wait.Reason,
		Message:    fmt.Sprintf("file wait paths=%d", len(runtime.Wait.FilePaths)),
	})

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"ok":         true,
		"session_id": req.SessionID,
		"run_status": runtime.Run.Status,
		"wait_kind":  runtime.Wait.Kind,
		"paths":      len(runtime.Wait.FilePaths),
	}
	json.NewEncoder(w).Encode(resp)
}

func fileWaitWatchPaths(paths []string, workspaceRoot string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{})
	for _, path := range paths {
		root := fileWaitWatchPath(path, workspaceRoot)
		if root == "" {
			continue
		}
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		out = append(out, root)
	}
	return out
}

func fileWaitWatchPath(path, workspaceRoot string) string {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return ""
	}
	clean := filepath.Clean(raw)
	if idx := strings.IndexAny(clean, "*?["); idx >= 0 {
		prefix := clean[:idx]
		if prefix == "" {
			return "."
		}
		dir := filepath.Dir(prefix)
		if dir == "." && strings.HasSuffix(prefix, string(filepath.Separator)) {
			return filepath.Clean(prefix)
		}
		return dir
	}
	if strings.HasSuffix(raw, string(filepath.Separator)) {
		return clean
	}
	resolved := clean
	if !filepath.IsAbs(clean) && workspaceRoot != "" {
		resolved = filepath.Join(workspaceRoot, clean)
	}
	if info, err := os.Stat(resolved); err == nil && info.IsDir() {
		return clean
	}
	dir := filepath.Dir(clean)
	if dir == "" {
		return "."
	}
	return dir
}

// handleWatch configures file watching for a session.
// Body: {"session_id": "...", "paths": ["src/"], "ignore_patterns": ["*.tmp"], "debounce": "3s", "enabled": true}
func (d *Daemon) handleWatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID      string   `json:"session_id"`
		Paths          []string `json:"paths"`
		IgnorePatterns []string `json:"ignore_patterns"`
		Debounce       string   `json:"debounce"`
		Enabled        *bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		http.Error(w, `{"error":"session_id required"}`, http.StatusBadRequest)
		return
	}

	d.mu.RLock()
	entry, ok := d.registry[req.SessionID]
	var runtime agent.RuntimeMeta
	var path string
	if ok {
		runtime = entry.Runtime
		path = entry.Path
	}
	d.mu.RUnlock()
	if !ok {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	debounce := runtime.FileWatch.Debounce
	if debounce == 0 {
		debounce = 3 * time.Second
	}
	if req.Debounce != "" {
		dur, err := time.ParseDuration(req.Debounce)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid debounce: %s"}`, err), http.StatusBadRequest)
			return
		}
		debounce = dur
	}

	paths := append([]string(nil), runtime.FileWatch.Paths...)
	if req.Paths != nil {
		paths = append([]string(nil), req.Paths...)
	}
	ignorePatterns := append([]string(nil), runtime.FileWatch.IgnorePatterns...)
	if req.IgnorePatterns != nil {
		ignorePatterns = append([]string(nil), req.IgnorePatterns...)
	}

	enabled := runtime.FileWatch.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	} else if req.Paths != nil {
		enabled = true
	}

	cfg := FileWatchConfig{
		Paths:          paths,
		IgnorePatterns: ignorePatterns,
		Debounce:       debounce,
		Enabled:        enabled,
	}
	runtime.FileWatch = agent.RuntimeWatchMeta{
		Paths:          append([]string(nil), cfg.Paths...),
		IgnorePatterns: append([]string(nil), cfg.IgnorePatterns...),
		Debounce:       cfg.Debounce,
		Enabled:        cfg.Enabled && len(cfg.Paths) > 0,
	}
	if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"save failed: %s"}`, err), http.StatusInternalServerError)
		return
	}

	d.mu.Lock()
	if entry := d.registry[req.SessionID]; entry != nil {
		entry.Runtime = runtime
	}
	d.mu.Unlock()

	if d.fileWatcher != nil {
		watchEntry := &SessionEntry{ID: req.SessionID, Path: path, Runtime: runtime}
		if runtime.FileWatch.Enabled {
			d.fileWatcher.Register(req.SessionID, fileWatchConfigForEntry(watchEntry))
		} else {
			d.fileWatcher.Unregister(req.SessionID)
		}
	}
	d.appendTimeline(path, agent.RuntimeTimelineEvent{
		Type:       "watch_configured",
		Source:     "api",
		Step:       "deterministic",
		RunStatus:  runtime.Run.Status,
		GoalStatus: runtime.Goal.Status,
		Message:    fmt.Sprintf("file watch enabled=%t paths=%d", runtime.FileWatch.Enabled, len(runtime.FileWatch.Paths)),
	})

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ok":true,"session_id":%q,"enabled":%t,"paths":%d}`, req.SessionID, runtime.FileWatch.Enabled, len(runtime.FileWatch.Paths))
}

// handleApprove resolves a pending daemon approval.
// Body: {"session_id":"...","approval_id":"...","session":true,"persist":false}
func (d *Daemon) handleApprove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID  string `json:"session_id"`
		ApprovalID string `json:"approval_id"`
		Session    bool   `json:"session"`
		Persist    bool   `json:"persist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || req.ApprovalID == "" {
		http.Error(w, `{"error":"session_id and approval_id required"}`, http.StatusBadRequest)
		return
	}
	allow := r.URL.Path == "/approvals/approve"

	d.mu.Lock()
	active := d.activeRuns[req.SessionID]
	if active == nil || active.Control == nil {
		d.mu.Unlock()
		http.Error(w, `{"error":"active run not found"}`, http.StatusNotFound)
		return
	}
	if _, ok := active.Approvals[req.ApprovalID]; !ok {
		d.mu.Unlock()
		http.Error(w, `{"error":"approval not found"}`, http.StatusNotFound)
		return
	}
	delete(active.Approvals, req.ApprovalID)
	if entry := d.registry[req.SessionID]; entry != nil {
		entry.Runtime.Run.Status = agent.RunStatusRunning
		entry.Runtime.Wait = agent.RuntimeWaitMeta{}
		runtime := entry.Runtime
		path := entry.Path
		d.mu.Unlock()
		_ = saveRuntimeMeta(path, runtime)
		action := "approval_denied"
		if allow {
			action = "approval_approved"
		}
		d.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       action,
			Source:     "api",
			Step:       "deterministic",
			RunStatus:  runtime.Run.Status,
			GoalStatus: runtime.Goal.Status,
			WaitKind:   "approval",
			WaitID:     req.ApprovalID,
		})
		active.Control.Approve(req.ApprovalID, allow, req.Session, req.Persist)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":true,"session_id":%q,"approval_id":%q,"allow":%t}`, req.SessionID, req.ApprovalID, allow)
		return
	}
	d.mu.Unlock()
	http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
}

// handleAnswer resolves a pending daemon ask request.
// Body: {"session_id":"...","ask_id":"...","selected":"..."} or
// {"session_id":"...","ask_id":"...","answers":[...]}.
func (d *Daemon) handleAnswer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string            `json:"session_id"`
		AskID     string            `json:"ask_id"`
		Selected  string            `json:"selected"`
		Answers   []event.AskAnswer `json:"answers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || req.AskID == "" {
		http.Error(w, `{"error":"session_id and ask_id required"}`, http.StatusBadRequest)
		return
	}

	d.mu.Lock()
	active := d.activeRuns[req.SessionID]
	if active == nil || active.Control == nil {
		d.mu.Unlock()
		http.Error(w, `{"error":"active run not found"}`, http.StatusNotFound)
		return
	}
	ask, ok := active.Asks[req.AskID]
	if !ok {
		d.mu.Unlock()
		http.Error(w, `{"error":"ask not found"}`, http.StatusNotFound)
		return
	}
	delete(active.Asks, req.AskID)
	if len(req.Answers) == 0 && req.Selected != "" {
		if len(ask.Questions) > 0 {
			for _, q := range ask.Questions {
				req.Answers = append(req.Answers, event.AskAnswer{QuestionID: q.ID, Selected: []string{req.Selected}})
			}
		} else {
			req.Answers = []event.AskAnswer{{Selected: []string{req.Selected}}}
		}
	}
	if entry := d.registry[req.SessionID]; entry != nil {
		entry.Runtime.Run.Status = agent.RunStatusRunning
		entry.Runtime.Wait = agent.RuntimeWaitMeta{}
		runtime := entry.Runtime
		path := entry.Path
		d.mu.Unlock()
		_ = saveRuntimeMeta(path, runtime)
		d.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       "ask_answered",
			Source:     "api",
			Step:       "deterministic",
			RunStatus:  runtime.Run.Status,
			GoalStatus: runtime.Goal.Status,
			WaitKind:   "ask",
			WaitID:     req.AskID,
		})
		active.Control.AnswerQuestion(req.AskID, req.Answers)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":true,"session_id":%q,"ask_id":%q}`, req.SessionID, req.AskID)
		return
	}
	d.mu.Unlock()
	http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
}
