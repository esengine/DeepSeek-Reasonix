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
)

// ── Data model ──────────────────────────────────────────────────────────────

// HeartbeatTask defines a single scheduled prompt.
type HeartbeatTask struct {
	ID            string `json:"id"`
	Title         string `json:"title"`    // user-visible label
	Prompt        string `json:"prompt"`   // the prompt to submit
	Interval      string `json:"interval"` // e.g. "5m", "1h", "30s"
	Enabled       bool   `json:"enabled"`
	Scope         string `json:"scope,omitempty"`         // "global" or "project"
	WorkspaceRoot string `json:"workspaceRoot,omitempty"` // project root path when scope="project"
	TopicID       string `json:"topicId,omitempty"`       // created topic, reused on re-run
	LastRunAt     int64  `json:"lastRunAt,omitempty"`     // unix millis
	CreatedAt     int64  `json:"createdAt,omitempty"`
}

// heartbeatConfig is the on-disk format.
type heartbeatConfig struct {
	Tasks []HeartbeatTask `json:"tasks"`
}

// ── Engine ──────────────────────────────────────────────────────────────────

// HeartbeatEngine runs scheduled task execution in a background goroutine.
// It is owned by App and started during App.startup.
type HeartbeatEngine struct {
	mu      sync.Mutex
	tasks   []HeartbeatTask
	done    chan struct{}
	running bool
	app     *App // back-reference for topic creation, tab routing, and prompt submission
}

func newHeartbeatEngine(app *App) *HeartbeatEngine {
	return &HeartbeatEngine{
		app:  app,
		done: make(chan struct{}),
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
	b, err := os.ReadFile(e.configPath())
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
// It merges results (topicId, lastRunAt) rather than replacing the full list,
// so concurrent HeartbeatSaveTasks edits are not lost.
func (e *HeartbeatEngine) tick() {
	e.mu.Lock()
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

	// If we already have a topicID, reuse it; otherwise create a new topic.
	var topicID = t.TopicID
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
	controllerReady := false
	for i := 0; i < 40; i++ {
		if ctrl := e.app.ctrlByTabID(tabMeta.ID); ctrl != nil {
			controllerReady = true
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !controllerReady {
		log.Printf("[heartbeat] controller not ready for %q, skipping", t.Title)
		return t // don't update LastRunAt — retry next tick
	}

	// Submit as a plain user turn so scheduled prompts cannot invoke desktop
	// shell or slash-command handlers such as "!cmd", "/clear", or "/compact".
	if !e.app.submitUserTurnToTab(tabMeta.ID, t.Prompt) {
		log.Printf("[heartbeat] submit skipped for %q", t.Title)
		return t
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
	out := make([]HeartbeatTask, len(e.tasks))
	copy(out, e.tasks)
	return out
}

// ReplaceTasks atomically replaces the task list and persists it.
func (e *HeartbeatEngine) ReplaceTasks(tasks []HeartbeatTask) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tasks = tasks
	return e.saveTasks(tasks)
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
	tasks := append([]HeartbeatTask(nil), e.tasks...)
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
	_ = e.saveTasks(tasks)
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
	if baseMillis == 0 {
		return true
	}
	return now.Sub(time.UnixMilli(baseMillis)) >= d
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
		return nil
	}
	return a.heartbeat.ListTasks()
}

// HeartbeatReloadTasks reloads tasks from disk and returns them.
func (a *App) HeartbeatReloadTasks() []HeartbeatTask {
	if a.heartbeat == nil {
		return nil
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
