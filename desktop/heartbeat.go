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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
	"reasonix/internal/secrets"
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
	Revision uint64          `json:"revision,omitempty"`
	Tasks    []HeartbeatTask `json:"tasks"`
}

// ErrHeartbeatConfigConflict means another writer changed the config after
// this engine last read it. Callers should reload before retrying the edit.
var ErrHeartbeatConfigConflict = errors.New("heartbeat config changed concurrently")

type heartbeatConfigSnapshot struct {
	cfg    heartbeatConfig
	digest [sha256.Size]byte
	exists bool
}

// HeartbeatConfigView is the revisioned Wails contract used by current
// frontends. ETag detects external editors that do not increment Revision.
type HeartbeatConfigView struct {
	Revision uint64          `json:"revision"`
	ETag     string          `json:"etag"`
	Tasks    []HeartbeatTask `json:"tasks"`
}

type HeartbeatConfigUpdate struct {
	Revision uint64          `json:"revision"`
	ETag     string          `json:"etag"`
	Tasks    []HeartbeatTask `json:"tasks"`
}

func (s heartbeatConfigSnapshot) view() HeartbeatConfigView {
	tasks := s.cfg.Tasks
	if tasks == nil {
		tasks = []HeartbeatTask{}
	}
	etag := ""
	if s.exists {
		etag = hex.EncodeToString(s.digest[:])
	}
	return HeartbeatConfigView{Revision: s.cfg.Revision, ETag: etag, Tasks: tasks}
}

// ── Engine ──────────────────────────────────────────────────────────────────

// HeartbeatEngine runs scheduled task execution in a background goroutine.
// It is owned by App and started during App.startup.
type HeartbeatEngine struct {
	mu             sync.Mutex
	tasks          []HeartbeatTask
	cfgRevision    uint64                           // persisted config revision last observed
	cfgDigest      [sha256.Size]byte                // decoded config bytes last observed
	cfgKnown       bool                             // cfgDigest describes an existing file
	cfgInitialized bool                             // engine has observed existing or missing config state
	cfgDeleted     bool                             // an existing config was removed externally
	pendingTopics  map[string]heartbeatPendingTopic // in-memory retry/in-flight safety for NewConversationEachRun
	runningTasks   map[string]struct{}              // task-level execution reservation shared by tick and TriggerNow
	done           chan struct{}
	running        bool
	app            *App // back-reference for topic creation, tab routing, and prompt submission
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
		runningTasks:  make(map[string]struct{}),
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
	snapshot, err := e.readConfigSnapshot()
	if err != nil {
		return nil
	}
	return snapshot.cfg.Tasks
}

func (e *HeartbeatEngine) readConfigSnapshot() (heartbeatConfigSnapshot, error) {
	path := e.configPath()
	b, err := readFileUTF8(path)
	if err != nil {
		if os.IsNotExist(err) {
			return heartbeatConfigSnapshot{}, nil
		}
		return heartbeatConfigSnapshot{}, err
	}
	var cfg heartbeatConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return heartbeatConfigSnapshot{}, fmt.Errorf("invalid config: %w", err)
	}
	snapshot := heartbeatConfigSnapshot{
		cfg:    cfg,
		digest: sha256.Sum256(b),
		exists: true,
	}
	return snapshot, nil
}

func (e *HeartbeatEngine) recordConfigSnapshotLocked(snapshot heartbeatConfigSnapshot) {
	e.cfgRevision = snapshot.cfg.Revision
	e.cfgDigest = snapshot.digest
	e.cfgKnown = snapshot.exists
	e.cfgInitialized = true
	if snapshot.exists {
		e.cfgDeleted = false
	}
}

// adoptExternalEditsLocked compares the exact content digest so edits are
// detected even on filesystems with coarse mtimes.
func (e *HeartbeatEngine) adoptExternalEditsLocked() {
	snapshot, err := e.readConfigSnapshot()
	if err != nil {
		log.Printf("[heartbeat] invalid external config: %v", err)
		return
	}
	if !snapshot.exists {
		// A config that existed when this engine started was explicitly removed
		// by the user. Treat that deletion as authoritative: retaining the old
		// in-memory tasks would execute a side effect on the next tick and then
		// recreate the file from stale state.
		if e.cfgInitialized && e.cfgKnown {
			e.tasks = nil
			e.pendingTopics = make(map[string]heartbeatPendingTopic)
			e.cfgDeleted = true
			e.recordConfigSnapshotLocked(snapshot)
		}
		return
	}
	if e.cfgKnown && snapshot.digest == e.cfgDigest {
		return
	}
	e.recordConfigSnapshotLocked(snapshot)
	e.tasks = snapshot.cfg.Tasks
	e.prunePendingTopicsLocked(e.tasks)
}

// saveTasks writes tasks to disk atomically.
func (e *HeartbeatEngine) saveTasks(tasks []HeartbeatTask) error {
	return e.writeTasks(tasks, heartbeatConfigSnapshot{}, false)
}

func (e *HeartbeatEngine) writeTasks(tasks []HeartbeatTask, expected heartbeatConfigSnapshot, compare bool) error {
	if tasks == nil {
		tasks = []HeartbeatTask{}
	}
	path := e.configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	lockCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	release, err := filelock.Acquire(lockCtx, path+".lock")
	if err != nil {
		return err
	}
	defer release()

	current, err := e.readConfigSnapshot()
	if err != nil {
		return err
	}
	if compare && (current.exists != expected.exists || current.digest != expected.digest || current.cfg.Revision != expected.cfg.Revision) {
		return ErrHeartbeatConfigConflict
	}
	revision := current.cfg.Revision + 1
	if !current.exists {
		revision = 1
	}
	cfg := heartbeatConfig{Revision: revision, Tasks: tasks}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, b, 0o644)
}

// Start launches the scheduler goroutine.
func (e *HeartbeatEngine) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		return
	}
	snapshot, err := e.readConfigSnapshot()
	if err != nil {
		log.Printf("[heartbeat] invalid config: %v", err)
	} else {
		e.recordConfigSnapshotLocked(snapshot)
		e.tasks = snapshot.cfg.Tasks
	}
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
	if !e.claimTask(t.ID) {
		log.Printf("[heartbeat] task %q is already running, skipping overlapping trigger", t.Title)
		return t
	}
	releaseLease, err := e.tryAcquireTaskLease(t.ID)
	if err != nil {
		log.Printf("[heartbeat] task %q is already owned by another runtime, skipping", t.Title)
		e.releaseTask(t.ID)
		return t
	}
	defer func() {
		releaseLease()
		e.releaseTask(t.ID)
	}()
	return e.executeTaskOwned(t)
}

// tryAcquireTaskLease extends the in-process reservation to other Reasonix
// processes. The lease is held from before topic creation through prompt
// submission, and the OS releases it automatically if the process is killed.
func (e *HeartbeatEngine) tryAcquireTaskLease(taskID string) (func(), error) {
	path := e.heartbeatTaskLeasePath(taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return filelock.TryAcquire(path)
}

func (e *HeartbeatEngine) heartbeatTaskLeasePath(taskID string) string {
	digest := sha256.Sum256([]byte(taskID))
	return e.configPath() + "." + hex.EncodeToString(digest[:8]) + ".run.lock"
}

func (e *HeartbeatEngine) claimTask(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.runningTasks == nil {
		e.runningTasks = make(map[string]struct{})
	}
	if _, exists := e.runningTasks[id]; exists {
		return false
	}
	e.runningTasks[id] = struct{}{}
	return true
}

func (e *HeartbeatEngine) releaseTask(id string) {
	e.mu.Lock()
	delete(e.runningTasks, id)
	e.mu.Unlock()
}

func (e *HeartbeatEngine) executeTaskOwned(t HeartbeatTask) HeartbeatTask {
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
		log.Printf("[heartbeat] OpenTab(%q): %s", t.Title, secrets.RedactError(err))
		t.LastRunAt = time.Now().UnixMilli()
		return t
	}

	// Wait for the tab's controller to be built (it's started
	// asynchronously in a goroutine by openTopicTab).
	var ctrl heartbeatRuntimeStatus
	for range 40 {
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
		return e.executeTaskOwned(t)
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
	return e.ReloadConfig().Tasks
}

func (e *HeartbeatEngine) ReloadConfig() HeartbeatConfigView {
	e.mu.Lock()
	defer e.mu.Unlock()
	snapshot, err := e.readConfigSnapshot()
	if err != nil {
		log.Printf("[heartbeat] reload config: %v", err)
		return heartbeatConfigSnapshot{cfg: heartbeatConfig{Tasks: []HeartbeatTask{}}}.view()
	}
	e.recordConfigSnapshotLocked(snapshot)
	e.tasks = snapshot.cfg.Tasks
	e.prunePendingTopicsLocked(e.tasks)
	return snapshot.view()
}

// ReplaceTasks atomically replaces the task list and persists it.
func (e *HeartbeatEngine) ReplaceTasks(tasks []HeartbeatTask) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	expected, err := e.readConfigSnapshot()
	if err != nil {
		return err
	}
	if e.cfgInitialized && (expected.exists != e.cfgKnown || expected.digest != e.cfgDigest || expected.cfg.Revision != e.cfgRevision) {
		return ErrHeartbeatConfigConflict
	}
	if err := e.writeTasks(tasks, expected, true); err != nil {
		return err
	}
	latest, err := e.readConfigSnapshot()
	if err != nil {
		return err
	}
	e.recordConfigSnapshotLocked(latest)
	e.tasks = tasks
	e.prunePendingTopicsLocked(tasks)
	return nil
}

// ReplaceConfig applies a frontend edit only when its revision and ETag still
// identify the exact config the user edited. This prevents a stale panel from
// overwriting an external or second-process change.
func (e *HeartbeatEngine) ReplaceConfig(update HeartbeatConfigUpdate) (HeartbeatConfigView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	expected, err := e.readConfigSnapshot()
	if err != nil {
		return HeartbeatConfigView{}, err
	}
	if expected.cfg.Revision != update.Revision || expected.view().ETag != update.ETag {
		return expected.view(), ErrHeartbeatConfigConflict
	}
	if err := e.writeTasks(update.Tasks, expected, true); err != nil {
		return expected.view(), err
	}
	latest, err := e.readConfigSnapshot()
	if err != nil {
		return HeartbeatConfigView{}, err
	}
	e.recordConfigSnapshotLocked(latest)
	e.tasks = latest.cfg.Tasks
	e.prunePendingTopicsLocked(e.tasks)
	return latest.view(), nil
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
	for range 3 {
		expected, err := e.readConfigSnapshot()
		if err != nil {
			log.Printf("[heartbeat] cannot read config before run-state merge: %v", err)
			return
		}
		tasks := expected.cfg.Tasks
		if !expected.exists {
			switch {
			case e.cfgDeleted || (e.cfgInitialized && e.cfgKnown):
				// Observe deletion in this CAS loop too. A run can finish before the
				// next scheduler tick adopts external edits; relying only on tick
				// would let that completion recreate a file the user just removed.
				e.tasks = nil
				e.pendingTopics = make(map[string]heartbeatPendingTopic)
				e.cfgDeleted = true
				e.recordConfigSnapshotLocked(expected)
				return
			case !e.cfgInitialized:
				// Only an engine that has never observed disk may bootstrap from an
				// in-memory list. Once a config existed, deletion is authoritative.
				tasks = append([]HeartbeatTask(nil), e.tasks...)
			}
		}
		mergeHeartbeatRunUpdates(tasks, updates)
		if err := e.writeTasks(tasks, expected, true); err != nil {
			if errors.Is(err, ErrHeartbeatConfigConflict) {
				continue
			}
			log.Printf("[heartbeat] run-state merge failed: %v", err)
			return
		}
		latest, err := e.readConfigSnapshot()
		if err != nil {
			log.Printf("[heartbeat] reload after run-state merge: %v", err)
			return
		}
		e.recordConfigSnapshotLocked(latest)
		e.tasks = tasks
		e.prunePendingTopicsLocked(tasks)
		return
	}
	log.Printf("[heartbeat] run-state merge lost repeated config races; next tick will retry")
}

func mergeHeartbeatRunUpdates(tasks []HeartbeatTask, updates map[string]HeartbeatTask) {
	for i := range tasks {
		update, ok := updates[tasks[i].ID]
		if !ok {
			continue
		}
		// Run state is monotonic. A runtime that lost the cross-process task
		// lease returns the task snapshot it started with; if its later config
		// merge lands after the owner, that stale completion must not roll back
		// the owner's timestamp or fresh-conversation topic.
		newerRun := update.LastRunAt > tasks[i].LastRunAt
		if update.TopicID != "" && (tasks[i].TopicID == "" || newerRun) {
			tasks[i].TopicID = update.TopicID
		}
		if newerRun {
			tasks[i].LastRunAt = update.LastRunAt
		}
		if tasks[i].CreatedAt == 0 && update.CreatedAt != 0 {
			tasks[i].CreatedAt = update.CreatedAt
		}
	}
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
	_, after, ok0 := strings.Cut(interval, "|")
	if !ok0 {
		return heartbeatSchedule{}, false
	}
	raw := strings.TrimSpace(after)
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
		for part := range strings.SplitSeq(rule, ",") {
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
	for offset := range windowDays {
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

// HeartbeatReloadConfig returns tasks with the CAS token used by current UIs.
func (a *App) HeartbeatReloadConfig() HeartbeatConfigView {
	if a.heartbeat == nil {
		return HeartbeatConfigView{Tasks: []HeartbeatTask{}}
	}
	return a.heartbeat.ReloadConfig()
}

// HeartbeatSaveTasks replaces the full task list and persists it.
func (a *App) HeartbeatSaveTasks(tasks []HeartbeatTask) error {
	if a.heartbeat == nil {
		return nil
	}
	return a.heartbeat.ReplaceTasks(tasks)
}

// HeartbeatSaveConfig replaces tasks only when the frontend's revision and
// ETag still match the exact file it loaded.
func (a *App) HeartbeatSaveConfig(update HeartbeatConfigUpdate) (HeartbeatConfigView, error) {
	if a.heartbeat == nil {
		return HeartbeatConfigView{Tasks: []HeartbeatTask{}}, nil
	}
	return a.heartbeat.ReplaceConfig(update)
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
