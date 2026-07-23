// Heartbeat task engine — scheduled AI prompts that create or update topics.
//
// Each task is a prompt submitted to a dedicated topic on a schedule.
// The config file under the Reasonix user state directory is human- and
// AI-editable; the engine runs the schedule in a background goroutine and
// exposes Wails bindings on App for the frontend panel.
//
// Design goal: minimal upstream intrusion — one file, zero changes to existing
// Go code (App field + startup line + bindings are the only touch points).

package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

// ── Data model ──────────────────────────────────────────────────────────────

// HeartbeatTask defines a single scheduled prompt.
type HeartbeatTask struct {
	ID                     string `json:"id"`
	Title                  string `json:"title"`    // user-visible label
	Prompt                 string `json:"prompt"`   // the prompt to submit
	Interval               string `json:"interval"` // e.g. "5m", "1h", "30s"
	Enabled                bool   `json:"enabled"`
	Scope                  string `json:"scope,omitempty"`                  // "global" or "project"
	WorkspaceRoot          string `json:"workspaceRoot,omitempty"`          // project root path when scope="project"
	TopicID                string `json:"topicId,omitempty"`                // created topic, reused on re-run
	LastRunAt              int64  `json:"lastRunAt,omitempty"`              // unix millis
	NewConversationEachRun bool   `json:"newConversationEachRun,omitempty"` // true = create new topic every run
	CreatedAt              int64  `json:"createdAt,omitempty"`
	ApprovalMode           string `json:"approvalMode"`              // "ask" | "auto" | "yolo"; empty defaults to "yolo"
	TimeWindowStart        string `json:"timeWindowStart,omitempty"` // "HH:MM" — interval tasks only run after this time (inclusive)
	TimeWindowEnd          string `json:"timeWindowEnd,omitempty"`   // "HH:MM" — interval tasks only run before this time (exclusive)
	NotifyChannels         *bool  `json:"notifyChannels,omitempty"`  // true = push to bot channels; nil/false = skip
}

// heartbeatConfig is the on-disk format.
type heartbeatConfig struct {
	Tasks []HeartbeatTask `json:"tasks"`
}

// ── Engine ──────────────────────────────────────────────────────────────────

// HeartbeatEngine runs scheduled task execution in a background goroutine.
// It is owned by App and started during App.startup.
type HeartbeatEngine struct {
	mu            sync.Mutex
	tasks         []HeartbeatTask
	cfgMod        time.Time                        // config-file mtime as of the engine's last read/write (external-edit probe)
	pendingTopics map[string]heartbeatPendingTopic // in-memory retry/in-flight safety for NewConversationEachRun
	done          chan struct{}
	running       bool
	app           *App // back-reference for topic creation, tab routing, and prompt submission
}

type heartbeatPendingTopic struct {
	TopicID   string
	Submitted bool
}

func newHeartbeatEngine(app *App) *HeartbeatEngine {
	return &HeartbeatEngine{
		app:           app,
		done:          make(chan struct{}),
		pendingTopics: make(map[string]heartbeatPendingTopic),
	}
}

// configPath returns the JSON file path.
func (e *HeartbeatEngine) configPath() string {
	dir := config.MemoryUserDir()
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "heartbeat-tasks.json")
}

// loadTasks reads tasks from disk.
func (e *HeartbeatEngine) loadTasks() []HeartbeatTask {
	b, err := readFileUTF8(e.configPath())
	if err != nil {
		return nil
	}
	var cfg heartbeatConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		log.Printf("[heartbeat] invalid config: %v", err)
		return nil
	}
	return cfg.Tasks
}

// noteConfigModLocked records the config file's current mtime so the tick
// external-edit probe does not re-read a file the engine itself just wrote.
func (e *HeartbeatEngine) noteConfigModLocked() {
	if info, err := os.Stat(e.configPath()); err == nil {
		e.cfgMod = info.ModTime()
	}
}

// adoptExternalEditsLocked re-reads the config when its mtime moved since the
// engine last touched the file: heartbeat-tasks.json is documented as human-
// and AI-editable, so an external edit should be scheduled by the next tick
// rather than waiting for a manual Refresh or an app restart.
func (e *HeartbeatEngine) adoptExternalEditsLocked() {
	info, err := os.Stat(e.configPath())
	if err != nil || info.ModTime().Equal(e.cfgMod) {
		return
	}
	e.cfgMod = info.ModTime()
	e.tasks = e.loadTasks()
	e.prunePendingTopicsLocked(e.tasks)
}

// saveTasks writes tasks to disk atomically.
func (e *HeartbeatEngine) saveTasks(tasks []HeartbeatTask) error {
	if tasks == nil {
		tasks = []HeartbeatTask{}
	}
	cfg := heartbeatConfig{Tasks: tasks}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := e.configPath()
	// Ensure the parent directory exists before writing.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Start launches the scheduler goroutine.
func (e *HeartbeatEngine) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return
	}
	e.tasks = e.loadTasks()
	e.noteConfigModLocked()
	e.running = true
	go e.loop()
	log.Printf("[heartbeat] engine started (%d tasks)", len(e.tasks))
}

// Stop signals the scheduler goroutine to exit.
func (e *HeartbeatEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		return
	}
	e.running = false
	close(e.done)
}

// loop is the main scheduler loop — tick every 30s and check each enabled task.
func (e *HeartbeatEngine) loop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-e.done:
			return
		case <-ticker.C:
			e.tick()
		}
	}
}

// tick checks every enabled task and runs those whose interval has elapsed.
// It first adopts any external edit to the config file (human/AI-editable),
// then merges results (topicId, lastRunAt) rather than replacing the full
// list, so concurrent HeartbeatSaveTasks edits are not lost.
func (e *HeartbeatEngine) tick() {
	e.mu.Lock()
	e.adoptExternalEditsLocked()
	tasks := append([]HeartbeatTask(nil), e.tasks...)
	e.mu.Unlock()

	now := time.Now()
	updates := make(map[string]HeartbeatTask)
	for i, t := range tasks {
		if !t.Enabled {
			continue
		}
		if !heartbeatTaskDueAt(t, now) {
			continue
		}
		// Run this task
		tasks[i] = e.executeTask(t)
		updates[t.ID] = tasks[i]
	}

	e.mu.Lock()
	e.mergeRunUpdatesLocked(updates)
	e.mu.Unlock()
}

// normalizeHeartbeatApprovalMode returns a valid approval mode for the task.
// Empty or unknown values default to "yolo" so that scheduled tasks run
// without interrupting the user for permission prompts.
func normalizeHeartbeatApprovalMode(mode string) string {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	switch normalized {
	case "ask", "auto", "yolo":
		return normalized
	default:
		return "yolo"
	}
}

type heartbeatRuntimeStatus interface {
	RuntimeStatus() control.RuntimeStatus
}

func heartbeatControllerBusy(ctrl heartbeatRuntimeStatus) bool {
	status := ctrl.RuntimeStatus()
	return status.Running || status.PendingPrompt
}

// executeTask runs one heartbeat: creates/opens topic, submits prompt.
// Returns the updated task (topicId and LastRunAt may change).
// On controller failure the task is returned WITHOUT updating LastRunAt,
// so it will be retried on the next tick.
func (e *HeartbeatEngine) executeTask(t HeartbeatTask) HeartbeatTask {
	title := "Heartbeat: " + t.Title
	scope := t.Scope
	workspaceRoot := t.WorkspaceRoot
	if scope == "" {
		scope = "global"
	}

	// Determine which topic to use.
	//
	// For NewConversationEachRun:
	//   - Reuse a pending topic from a failed pre-submit attempt.
	//   - Re-check a submitted topic until its controller is idle, so a long
	//     previous run cannot overlap with the next scheduled fresh topic.
	//   - Once the submitted topic is idle and due again, clear it and create a
	//     fresh topic.
	//   - topicId is always updated to the latest conversation so the task list
	//     always points to the most recent session regardless of mode switch.
	//
	// For the legacy mode:
	//   - Reuse the persisted topicID if available; create one on first run.
	var topicID string
	var pendingSubmitted bool
	if t.NewConversationEachRun {
		e.mu.Lock()
		pending := e.pendingTopics[t.ID]
		e.mu.Unlock()
		topicID = pending.TopicID
		pendingSubmitted = pending.Submitted
		if topicID == "" {
			// No pending topic — create a fresh one.
			meta, err := e.app.CreateTopic(scope, workspaceRoot, title)
			if err != nil {
				log.Printf("[heartbeat] CreateTopic(%q): %v", t.Title, err)
				t.LastRunAt = time.Now().UnixMilli()
				return t
			}
			topicID = meta.ID
			t.TopicID = topicID // always persist the latest topic
			// Save in-memory for retry safety (NOT persisted to disk).
			e.mu.Lock()
			if e.pendingTopics == nil {
				e.pendingTopics = make(map[string]heartbeatPendingTopic)
			}
			e.pendingTopics[t.ID] = heartbeatPendingTopic{TopicID: topicID}
			e.mu.Unlock()
		}
	} else {
		topicID = t.TopicID
		if topicID == "" {
			meta, err := e.app.CreateTopic(scope, workspaceRoot, title)
			if err != nil {
				log.Printf("[heartbeat] CreateTopic(%q): %v", t.Title, err)
				t.LastRunAt = time.Now().UnixMilli()
				return t
			}
			topicID = meta.ID
			t.TopicID = topicID
		}
	}

	// Open the tab for the topic (creates one if needed) without changing the
	// user's active tab or active workspace pointer.
	var tabMeta TabMeta
	var err error
	if scope == "project" && workspaceRoot != "" {
		tabMeta, err = e.app.openProjectTabInactive(workspaceRoot, topicID)
	} else {
		tabMeta, err = e.app.openGlobalTabInactive(topicID)
	}
	if err != nil {
		log.Printf("[heartbeat] OpenTab(%q): %v", t.Title, err)
		t.LastRunAt = time.Now().UnixMilli()
		return t
	}

	// Wait for the tab's controller to be built (it's started
	// asynchronously in a goroutine by openTopicTab).
	var ctrl heartbeatRuntimeStatus
	for i := 0; i < 40; i++ {
		if candidate := e.app.ctrlByTabID(tabMeta.ID); candidate != nil {
			ctrl = candidate
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if ctrl == nil {
		log.Printf("[heartbeat] controller not ready for %q, skipping", t.Title)
		return t // don't update LastRunAt — retry next tick
	}
	if heartbeatControllerBusy(ctrl) {
		log.Printf("[heartbeat] controller busy for %q, skipping", t.Title)
		return t // don't change approval mode for an existing turn — retry next tick
	}
	if t.NewConversationEachRun && pendingSubmitted {
		e.mu.Lock()
		if pending := e.pendingTopics[t.ID]; pending.TopicID == topicID && pending.Submitted {
			delete(e.pendingTopics, t.ID)
		}
		e.mu.Unlock()
		return e.executeTask(t)
	}

	// Set the task's approval mode only after confirming the controller is idle.
	// SetToolApprovalModeForTab may drain pending approvals for auto/yolo modes,
	// so applying it to a busy reused topic would accidentally approve a previous
	// turn instead of preparing this heartbeat prompt.
	mode := normalizeHeartbeatApprovalMode(t.ApprovalMode)
	t.ApprovalMode = mode
	e.app.SetToolApprovalModeForTab(tabMeta.ID, mode)

	// Attach bot event forwarding if the bot runtime is active and has
	// session-mapped targets. The forwarder is set on the tab's event sink
	// so AI output events are streamed to connected bot channels in
	// real-time alongside the desktop UI.
	var botForwarder event.Sink
	if t.NotifyChannels != nil && *t.NotifyChannels {
		botForwarder = e.newBotForwarder(tabMeta.ID)
	}

	// Submit as a plain user turn so scheduled prompts cannot invoke desktop
	// shell or slash-command handlers such as "!cmd", "/clear", or "/compact".
	if !e.app.submitUserTurnToTabWithSink(tabMeta.ID, t.Prompt, botForwarder) {
		log.Printf("[heartbeat] submit skipped for %q", t.Title)
		return t
	}

	// After a successful submit, keep the topic as an in-flight guard. The next
	// due run will busy-check this controller before creating a fresh topic.
	if t.NewConversationEachRun {
		e.mu.Lock()
		if e.pendingTopics == nil {
			e.pendingTopics = make(map[string]heartbeatPendingTopic)
		}
		e.pendingTopics[t.ID] = heartbeatPendingTopic{TopicID: topicID, Submitted: true}
		e.mu.Unlock()
	}

	t.LastRunAt = time.Now().UnixMilli()
	if t.CreatedAt == 0 {
		t.CreatedAt = t.LastRunAt
	}
	return t
}

// ListTasks returns a copy of the current tasks (in-memory).
func (e *HeartbeatEngine) ListTasks() []HeartbeatTask {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]HeartbeatTask, len(e.tasks))
	copy(out, e.tasks)
	return out
}

// ReloadTasks reloads the task list from disk and replaces the in-memory copy.
func (e *HeartbeatEngine) ReloadTasks() []HeartbeatTask {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tasks = e.loadTasks()
	e.noteConfigModLocked()
	e.prunePendingTopicsLocked(e.tasks)
	out := make([]HeartbeatTask, len(e.tasks))
	copy(out, e.tasks)
	return out
}

// ReplaceTasks atomically replaces the task list and persists it.
func (e *HeartbeatEngine) ReplaceTasks(tasks []HeartbeatTask) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tasks = tasks
	e.prunePendingTopicsLocked(tasks)
	err := e.saveTasks(tasks)
	e.noteConfigModLocked()
	return err
}

func (e *HeartbeatEngine) prunePendingTopicsLocked(tasks []HeartbeatTask) {
	if len(e.pendingTopics) == 0 {
		return
	}
	keep := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if task.NewConversationEachRun {
			keep[task.ID] = true
		}
	}
	for id := range e.pendingTopics {
		if !keep[id] {
			delete(e.pendingTopics, id)
		}
	}
}

// TriggerNow runs a single task immediately by ID.
func (e *HeartbeatEngine) TriggerNow(id string) {
	e.mu.Lock()
	tasks := append([]HeartbeatTask(nil), e.tasks...)
	e.mu.Unlock()
	updates := make(map[string]HeartbeatTask, 1)
	for i, t := range tasks {
		if t.ID == id {
			tasks[i] = e.executeTask(t)
			updates[id] = tasks[i]
			break
		}
	}
	if len(updates) == 0 {
		return
	}
	e.mu.Lock()
	e.mergeRunUpdatesLocked(updates)
	e.mu.Unlock()
}

func (e *HeartbeatEngine) mergeRunUpdatesLocked(updates map[string]HeartbeatTask) {
	if len(updates) == 0 {
		return
	}
	// Rebase onto the on-disk list before the full-list save: the config file
	// is documented as human- and AI-editable, so an external edit may have
	// landed after the in-memory snapshot this tick ran from. The engine owns
	// only the run-state fields (TopicID, LastRunAt, CreatedAt backfill);
	// task definitions added, edited, or deleted externally are adopted from
	// disk, so the save below can never silently roll an external edit back.
	tasks := e.loadTasks()
	if tasks == nil {
		// Missing or unreadable file: fall back to the in-memory list so the
		// run state still lands (the historical behavior).
		tasks = append([]HeartbeatTask(nil), e.tasks...)
	}
	for i := range tasks {
		update, ok := updates[tasks[i].ID]
		if !ok {
			continue
		}
		if update.TopicID != "" {
			tasks[i].TopicID = update.TopicID
		}
		if update.LastRunAt != 0 {
			tasks[i].LastRunAt = update.LastRunAt
		}
		if tasks[i].CreatedAt == 0 && update.CreatedAt != 0 {
			tasks[i].CreatedAt = update.CreatedAt
		}
	}
	e.tasks = tasks
	e.prunePendingTopicsLocked(tasks)
	_ = e.saveTasks(tasks)
	e.noteConfigModLocked()
}

// parseInterval converts a string like "5m", "1h", "30s" to time.Duration.
// Suffix after '|' is stripped (e.g. "24h|daily@09:00" -> "24h").
// Empty or invalid strings return 0, nil (task will be skipped).
func parseInterval(s string) (time.Duration, error) {
	if idx := strings.Index(s, "|"); idx >= 0 {
		s = s[:idx]
	}
	if len(s) == 0 {
		return 0, nil
	}
	// Support common suffixed intervals
	switch s[len(s)-1] {
	case 's', 'm', 'h':
		return time.ParseDuration(s)
	default:
		// Try "Xm" as default assumption
		return time.ParseDuration(s + "m")
	}
}

func heartbeatTaskDueAt(t HeartbeatTask, now time.Time) bool {
	if scheduled, ok := previousHeartbeatScheduleAt(t, now); ok {
		if t.CreatedAt != 0 && scheduled.Before(time.UnixMilli(t.CreatedAt)) {
			return false
		}
		if t.LastRunAt != 0 && !time.UnixMilli(t.LastRunAt).Before(scheduled) {
			return false
		}
		return !scheduled.After(now)
	}

	d, err := parseInterval(t.Interval)
	if err != nil || d <= 0 {
		return false
	}
	baseMillis := t.LastRunAt
	if baseMillis == 0 {
		baseMillis = t.CreatedAt
	}
	hasTimeWindow := t.TimeWindowStart != "" || t.TimeWindowEnd != ""
	if baseMillis == 0 {
		if hasTimeWindow {
			return heartbeatWithinTimeWindow(t, now)
		}
		return true
	}
	if now.Sub(time.UnixMilli(baseMillis)) < d {
		return false
	}

	// For interval-based tasks with a time window, check if current time
	// falls within the configured window. If outside, defer until the next
	// tick that falls within the window.
	if hasTimeWindow {
		return heartbeatWithinTimeWindow(t, now)
	}

	return true
}

// heartbeatWithinTimeWindow returns true when now falls within the task's
// configured time window. If the window is empty it returns true.
// Format: "HH:MM" in 24-hour clock; start inclusive, end exclusive.
func heartbeatWithinTimeWindow(t HeartbeatTask, now time.Time) bool {
	startH, startM, startOK := parseHeartbeatClock(t.TimeWindowStart)
	endH, endM, endOK := parseHeartbeatClock(t.TimeWindowEnd)

	if !startOK && !endOK {
		return true // no window configured
	}

	minutes := now.Hour()*60 + now.Minute()

	// If only start is set: allow from start to end of day
	if startOK && !endOK {
		return minutes >= startH*60+startM
	}

	// If only end is set: allow from midnight to end
	if !startOK && endOK {
		return minutes < endH*60+endM
	}

	startMin := startH*60 + startM
	endMin := endH*60 + endM

	if startMin < endMin {
		// Normal window: 09:00-17:00
		return minutes >= startMin && minutes < endMin
	}
	// Cross-midnight window: 22:00-06:00
	return minutes >= startMin || minutes < endMin
}

type heartbeatSchedule struct {
	kind     string
	days     []time.Weekday
	month    int
	day      int
	hour     int
	minute   int
	hasRules bool
}

func parseHeartbeatSchedule(interval string) (heartbeatSchedule, bool) {
	idx := strings.Index(interval, "|")
	if idx < 0 {
		return heartbeatSchedule{}, false
	}
	raw := strings.TrimSpace(interval[idx+1:])
	if raw == "" {
		return heartbeatSchedule{}, false
	}
	at := "09:00"
	if parts := strings.SplitN(raw, "@", 2); len(parts) == 2 {
		raw = parts[0]
		at = parts[1]
	}
	hour, minute, ok := parseHeartbeatClock(at)
	if !ok {
		return heartbeatSchedule{}, false
	}
	kind := raw
	rule := ""
	if parts := strings.SplitN(raw, ":", 2); len(parts) == 2 {
		kind = parts[0]
		rule = parts[1]
	}
	s := heartbeatSchedule{kind: kind, hour: hour, minute: minute, hasRules: true}
	switch kind {
	case "daily":
		return s, true
	case "weekly", "biweekly":
		for _, part := range strings.Split(rule, ",") {
			if wd, ok := parseHeartbeatWeekday(part); ok {
				s.days = append(s.days, wd)
			}
		}
		return s, len(s.days) > 0
	case "monthly":
		s.day = parsePositiveInt(rule, 1)
		return s, true
	case "yearly":
		parts := strings.SplitN(rule, "-", 2)
		s.month = parsePositiveInt(firstString(parts), 1)
		s.day = 1
		if len(parts) == 2 {
			s.day = parsePositiveInt(parts[1], 1)
		}
		if s.month < 1 {
			s.month = 1
		}
		if s.month > 12 {
			s.month = 12
		}
		return s, true
	default:
		return heartbeatSchedule{}, false
	}
}

func previousHeartbeatScheduleAt(t HeartbeatTask, now time.Time) (time.Time, bool) {
	s, ok := parseHeartbeatSchedule(t.Interval)
	if !ok || !s.hasRules {
		return time.Time{}, false
	}
	switch s.kind {
	case "daily":
		candidate := dateAt(now.Year(), now.Month(), now.Day(), s.hour, s.minute, now.Location())
		if candidate.After(now) {
			candidate = candidate.AddDate(0, 0, -1)
		}
		return candidate, true
	case "weekly":
		return previousHeartbeatWeeklyAt(s, now, 7, time.Time{})
	case "biweekly":
		anchor := heartbeatScheduleAnchor(t, now)
		return previousHeartbeatWeeklyAt(s, now, 14, anchor)
	case "monthly":
		return previousHeartbeatMonthlyAt(s, now), true
	case "yearly":
		return previousHeartbeatYearlyAt(s, now), true
	default:
		return time.Time{}, false
	}
}

func previousHeartbeatWeeklyAt(s heartbeatSchedule, now time.Time, windowDays int, anchor time.Time) (time.Time, bool) {
	var best time.Time
	for offset := 0; offset < windowDays; offset++ {
		day := now.AddDate(0, 0, -offset)
		for _, wd := range s.days {
			if day.Weekday() != wd {
				continue
			}
			candidate := dateAt(day.Year(), day.Month(), day.Day(), s.hour, s.minute, now.Location())
			if candidate.After(now) {
				continue
			}
			if !anchor.IsZero() && weeksBetween(weekStart(anchor), weekStart(candidate))%2 != 0 {
				continue
			}
			if best.IsZero() || candidate.After(best) {
				best = candidate
			}
		}
	}
	return best, !best.IsZero()
}

func previousHeartbeatMonthlyAt(s heartbeatSchedule, now time.Time) time.Time {
	candidate := monthlyCandidate(now.Year(), now.Month(), s.day, s.hour, s.minute, now.Location())
	if candidate.After(now) {
		prev := now.AddDate(0, -1, 0)
		candidate = monthlyCandidate(prev.Year(), prev.Month(), s.day, s.hour, s.minute, now.Location())
	}
	return candidate
}

func previousHeartbeatYearlyAt(s heartbeatSchedule, now time.Time) time.Time {
	month := time.Month(s.month)
	candidate := monthlyCandidate(now.Year(), month, s.day, s.hour, s.minute, now.Location())
	if candidate.After(now) {
		candidate = monthlyCandidate(now.Year()-1, month, s.day, s.hour, s.minute, now.Location())
	}
	return candidate
}

func heartbeatScheduleAnchor(t HeartbeatTask, now time.Time) time.Time {
	if t.CreatedAt != 0 {
		return time.UnixMilli(t.CreatedAt)
	}
	if t.LastRunAt != 0 {
		return time.UnixMilli(t.LastRunAt)
	}
	return now
}

func parseHeartbeatClock(s string) (int, int, bool) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	hour := parsePositiveInt(parts[0], -1)
	minute := parsePositiveInt(parts[1], -1)
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func parseHeartbeatWeekday(s string) (time.Weekday, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "sun":
		return time.Sunday, true
	case "mon":
		return time.Monday, true
	case "tue":
		return time.Tuesday, true
	case "wed":
		return time.Wednesday, true
	case "thu":
		return time.Thursday, true
	case "fri":
		return time.Friday, true
	case "sat":
		return time.Saturday, true
	default:
		return time.Sunday, false
	}
}

func parsePositiveInt(s string, fallback int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return fallback
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func dateAt(year int, month time.Month, day, hour, minute int, loc *time.Location) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, loc)
}

func monthlyCandidate(year int, month time.Month, day, hour, minute int, loc *time.Location) time.Time {
	if day < 1 {
		day = 1
	}
	if max := daysInMonth(year, month, loc); day > max {
		day = max
	}
	return dateAt(year, month, day, hour, minute, loc)
}

func daysInMonth(year int, month time.Month, loc *time.Location) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
}

func weekStart(t time.Time) time.Time {
	dayOffset := (int(t.Weekday()) + 6) % 7
	base := dateAt(t.Year(), t.Month(), t.Day(), 0, 0, t.Location())
	return base.AddDate(0, 0, -dayOffset)
}

func weeksBetween(a, b time.Time) int {
	if b.Before(a) {
		a, b = b, a
	}
	return int(b.Sub(a).Hours() / 24 / 7)
}

// ── Wails bindings on App ───────────────────────────────────────────────────

// HeartbeatListTasks returns all heartbeat tasks.
func (a *App) HeartbeatListTasks() []HeartbeatTask {
	if a.heartbeat == nil {
		return []HeartbeatTask{}
	}
	return a.heartbeat.ListTasks()
}

// HeartbeatReloadTasks reloads tasks from disk and returns them.
func (a *App) HeartbeatReloadTasks() []HeartbeatTask {
	if a.heartbeat == nil {
		return []HeartbeatTask{}
	}
	return a.heartbeat.ReloadTasks()
}

// HeartbeatSaveTasks replaces the full task list and persists it.
func (a *App) HeartbeatSaveTasks(tasks []HeartbeatTask) error {
	if a.heartbeat == nil {
		return nil
	}
	return a.heartbeat.ReplaceTasks(tasks)
}

// HeartbeatTriggerNow immediately executes the task with the given ID.
func (a *App) HeartbeatTriggerNow(id string) {
	if a.heartbeat == nil {
		return
	}
	a.heartbeat.TriggerNow(id)
}

// HeartbeatGenerateID returns a random id for new tasks.
func (a *App) HeartbeatGenerateID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// newBotForwarder builds event forwarding for a heartbeat turn. The caller
// attaches it only after acquiring the tab's turn-admission gate.
func (e *HeartbeatEngine) newBotForwarder(tabID string) event.Sink {
	runtime := e.app.botRuntime
	if runtime == nil || !runtime.Running() {
		return nil
	}
	cfg, err := e.app.loadDesktopBotConfig()
	if err != nil {
		log.Printf("[heartbeat] load config for bot forward: %v", err)
		return nil
	}
	targets := runtime.ForwardTargets(cfg)
	if len(targets) == 0 {
		return nil // no session-mapped channels to forward to
	}
	tab := e.app.tabByID(tabID)
	if tab == nil || tab.sink == nil {
		return nil
	}
	log.Printf("[heartbeat] bot forwarding attached: %d target(s) for tab %s", len(targets), tabID)
	return newBotEventForwarder(runtime, targets)
}
