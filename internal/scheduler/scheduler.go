// Package scheduler manages session-scoped scheduled tasks for /loop and the
// cron_* / schedule_wakeup tools. A Scheduler owns the task set and a
// background ticker; when a task comes due it invokes an injected onFire
// callback (the Controller's runScheduledTurn), which fires a turn between
// foreground turns. The task list persists to <workspace>/.reasonix/
// scheduled-tasks.json — one file per working directory, shared by every chat
// launched there and surviving /new, /clear, and session deletion — so
// --resume and new sessions restore unexpired tasks for that directory.
package scheduler

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"
)

// DefaultTaskLimit caps how many scheduled tasks one session may hold.
const DefaultTaskLimit = 50

// taskExpiry bounds how long a task survives after creation; the scheduler
// prunes expired tasks on load so a forgotten loop cannot run forever.
// Tasks created with NoExpire (via /loop --forever or cron_create
// no_expire) are exempt and run until explicitly cancelled.
const taskExpiry = 7 * 24 * time.Hour

// ErrTaskLimit reports that the session already holds DefaultTaskLimit tasks.
var ErrTaskLimit = errors.New("scheduled task limit reached (50) — cancel a task first")

// Task is one scheduled job.
type Task struct {
	ID       string    `json:"id"`
	CronExpr string    `json:"cron,omitempty"` // 5-field cron; empty = dynamic (agent-controlled)
	Prompt   string    `json:"prompt"`
	OneShot  bool      `json:"oneShot"`            // auto-delete after firing
	NoExpire bool      `json:"noExpire,omitempty"` // exempt from the 7-day expiry on load
	Created  time.Time `json:"created"`
	NextFire time.Time `json:"nextFire"` // zero = paused / no pending wakeup
	Fires    int       `json:"fires"`    // iterations completed (informational)
	// firing marks a delivered task whose turn has not started yet (it may be
	// parked behind a foreground turn). fireDue skips firing tasks so a busy
	// session cannot queue duplicate turns for one due cycle. It is in-memory
	// only: a resumed session re-arms from the persisted NextFire.
	firing bool `json:"-"`
}

// View is a read-only snapshot of a task for list/status rendering.
type View struct {
	ID       string `json:"id"`
	CronExpr string `json:"cron,omitempty"`
	Prompt   string `json:"prompt"`
	OneShot  bool   `json:"oneShot"`
	NoExpire bool   `json:"noExpire,omitempty"`
	Created  string `json:"created,omitempty"`  // RFC3339
	NextFire string `json:"nextFire,omitempty"` // RFC3339, empty when paused
	Fires    int    `json:"fires"`
}

// Scheduler owns the session's scheduled-task set and the ticker goroutine.
type Scheduler struct {
	mu    sync.Mutex
	tasks map[string]*Task
	// deleted remembers task IDs removed by this session so merge-on-save
	// (saveLocked) does not resurrect them from another session's snapshot of
	// the shared per-directory sidecar. In-memory only: Load resets it because
	// a fresh session's source of truth is the file itself.
	deleted  map[string]bool
	started  bool
	stopCh   chan struct{}
	done     chan struct{}
	onFire   func(task Task) // set once via OnFire; called from the ticker goroutine
	persist  string          // sidecar path for Save; "" disables persistence
	writeMu  sync.Mutex      // serializes sidecar writes off mu
	lastSave time.Time       // rate-limit stamp, guarded by mu (Load resets it)
}

// New returns an idle Scheduler. Call Start to begin firing tasks.
func New() *Scheduler {
	return &Scheduler{
		tasks:   map[string]*Task{},
		deleted: map[string]bool{},
		stopCh:  make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// OnFire installs the callback invoked for each due task. Only the first
// install wins; subsequent calls are ignored so a caller can't clobber the
// controller's wiring.
func (s *Scheduler) OnFire(fn func(task Task)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onFire == nil {
		s.onFire = fn
	}
}

// SetPersistPath names the sidecar file saves are written to ("" disables
// persistence). It must be set before Start; changes after Start apply to
// subsequent saves.
func (s *Scheduler) SetPersistPath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persist = path
}

// Start launches the ticker goroutine. It is idempotent and, unlike Stop,
// may be called again after a Stop (fresh channels are created each time).
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	stopCh := make(chan struct{})
	done := make(chan struct{})
	s.stopCh, s.done = stopCh, done
	s.mu.Unlock()
	go s.loop(stopCh, done)
}

func (s *Scheduler) loop(stopCh, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			s.fireDue()
		}
	}
}

// fireDue invokes onFire for every task whose NextFire has passed, then
// advances or removes the task. Called from the ticker goroutine only; the
// onFire callback runs outside the lock and must be non-blocking.
func (s *Scheduler) fireDue() {
	now := time.Now()
	var due []Task
	onFire := s.onFire // snapshot under mu; OnFire only sets it once, pre-Start
	s.mu.Lock()
	for _, t := range s.tasks {
		if t.firing {
			continue // a turn for this task is already queued/starting
		}
		if t.NextFire.IsZero() || t.NextFire.After(now) {
			continue
		}
		if t.CronExpr == "" {
			// Dynamic task: this fire consumes the wakeup; the agent's next
			// schedule_wakeup call sets the following one.
			t.NextFire = time.Time{}
		}
		// Cron task: keep the delivered slot as NextFire. firing=true blocks
		// re-delivery until MarkStarted re-arms from the slot after the turn
		// actually starts, so a turn longer than the interval skips cycles
		// instead of stampeding back-to-back turns.
		t.Fires++
		t.firing = true
		due = append(due, *t)
	}
	ids := make([]string, len(due))
	for i := range due {
		ids[i] = due[i].ID
	}
	s.mu.Unlock()

	for _, t := range due {
		if onFire != nil {
			onFire(t)
		}
	}

	s.mu.Lock()
	for _, id := range ids {
		// Delivery = consumption for one-shots: the reminder is spent the
		// moment its turn is delivered, even if admission later drops that
		// turn (e.g. the controller is rotating). A dropped reminder is a
		// rare edge and the alternative — re-arming it into a possibly
		// far-future slot — risks a stale fire long after the user asked.
		if task, ok := s.tasks[id]; ok && task.OneShot {
			delete(s.tasks, id)
			s.deleted[id] = true
		}
	}
	s.mu.Unlock()
	s.saveLocked()
}

// MarkStarted clears a task's firing flag once its turn actually begins (the
// parked body in runScheduledTurn calls this first) and re-arms a cron
// task's schedule from now — cycles that passed while the turn waited or ran
// are skipped, so a turn longer than the interval never triggers catch-up
// fires. A no-op for unknown IDs.
func (s *Scheduler) MarkStarted(id string) {
	s.mu.Lock()
	if t, ok := s.tasks[id]; ok {
		t.firing = false
		if t.CronExpr != "" {
			t.NextFire = Next(t.CronExpr, time.Now())
		}
	}
	s.mu.Unlock()
}

// ReleaseFiring clears a task's firing flag without re-arming its schedule.
// runScheduledTurn calls it when admission dropped the turn (controller
// rotating or closed) and the body will never start: a cron task then
// re-fires on the next tick, while a dynamic task stays paused until the
// agent calls schedule_wakeup again.
func (s *Scheduler) ReleaseFiring(id string) {
	s.mu.Lock()
	if t, ok := s.tasks[id]; ok {
		t.firing = false
	}
	s.mu.Unlock()
}

// Add registers a new task and returns its 8-char hex ID. A non-empty cronExpr
// recurs on that schedule from the next matching time after now. An empty
// cronExpr creates a dynamic task that fires once at nextFire (pass
// time.Now() for an immediate first iteration) and then waits for
// schedule_wakeup to set the next wakeup. oneShot tasks delete themselves
// after their first fire regardless of cronExpr. noExpire tasks are exempt
// from the 7-day load expiry (endless loops).
func (s *Scheduler) Add(cronExpr, prompt string, nextFire time.Time, oneShot, noExpire bool) (string, error) {
	id, err := newTaskID()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	if len(s.tasks) >= DefaultTaskLimit {
		s.mu.Unlock()
		return "", ErrTaskLimit
	}
	if cronExpr != "" {
		nextFire = Next(cronExpr, time.Now())
	}
	s.tasks[id] = &Task{
		ID:       id,
		CronExpr: cronExpr,
		Prompt:   prompt,
		OneShot:  oneShot,
		NoExpire: noExpire,
		Created:  time.Now(),
		NextFire: nextFire,
	}
	s.mu.Unlock()
	s.saveLocked()
	return id, nil
}

// Delete removes a task by ID. ok reports whether it existed. The ID is
// tombstoned even when this session no longer holds it, so a later
// merge-on-save cannot resurrect it from another session's stale snapshot of
// the shared sidecar. Deletes persist immediately (saveNow) so a tombstone
// can never be dropped inside the rate-limiter window.
func (s *Scheduler) Delete(id string) bool {
	s.mu.Lock()
	_, ok := s.tasks[id]
	if ok {
		delete(s.tasks, id)
	}
	s.deleted[id] = true
	s.mu.Unlock()
	s.saveNow()
	return ok
}

// CancelAll removes every task. Deletes persist immediately (saveNow) so the
// tombstones cannot be dropped inside the rate-limiter window.
func (s *Scheduler) CancelAll() {
	s.mu.Lock()
	had := len(s.tasks) > 0
	for id := range s.tasks {
		s.deleted[id] = true
	}
	s.tasks = map[string]*Task{}
	s.mu.Unlock()
	if had {
		s.saveNow()
	}
}

// CancelDynamic removes only the dynamic (agent-scheduled, cron-less) tasks.
// The TUI's Esc path uses this so a stray Esc can never wipe fixed-interval
// cron loops or one-shot reminders, which are managed explicitly via
// cron_list/cron_delete. It returns the number of tasks removed.
func (s *Scheduler) CancelDynamic() int {
	s.mu.Lock()
	n := 0
	for id, t := range s.tasks {
		if t.CronExpr == "" {
			delete(s.tasks, id)
			s.deleted[id] = true
			n++
		}
	}
	s.mu.Unlock()
	if n > 0 {
		s.saveNow() // tombstones must not be dropped inside the rate-limiter window
	}
	return n
}

// ScheduleWakeup sets the pending wakeup of every dynamic (agent-scheduled)
// task to now+delay — loops share the agent's scheduling intent. It returns
// the number of tasks woken.
func (s *Scheduler) ScheduleWakeup(delay time.Duration) int {
	at := time.Now().Add(delay)
	s.mu.Lock()
	n := 0
	for _, t := range s.tasks {
		if t.CronExpr == "" {
			t.NextFire = at
			n++
		}
	}
	s.mu.Unlock()
	if n > 0 {
		s.saveNow() // tombstones must not be dropped inside the rate-limiter window
	}
	return n
}

// Rearm makes a task due again on the next tick. It is used when an injected
// fire was never consumed — the turn ended abnormally and the steer queue was
// flushed unapplied — so the delivery is retried instead of silently spent
// (matching the parked path's ReleaseFiring semantics for dropped turns).
// The guarantee covers cron and dynamic tasks; one-shot tasks are already
// deleted at delivery, so their fire is intentionally spent. A no-op for
// unknown IDs.
func (s *Scheduler) Rearm(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return false
	}
	t.firing = false
	t.NextFire = time.Now()
	return true
}

// Returns the number of tasks paused.
func (s *Scheduler) StopWakeup() int {
	s.mu.Lock()
	n := 0
	for _, t := range s.tasks {
		if t.CronExpr == "" && !t.NextFire.IsZero() {
			t.NextFire = time.Time{}
			n++
		}
	}
	s.mu.Unlock()
	if n > 0 {
		s.saveLocked()
	}
	return n
}

// Tasks returns a snapshot of all tasks, newest first.
func (s *Scheduler) Tasks() []View {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]View, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, taskView(t))
	}
	// newest first (by Created), then stable by ID
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Created > out[j-1].Created; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func taskView(t *Task) View {
	v := View{
		ID:       t.ID,
		CronExpr: t.CronExpr,
		Prompt:   t.Prompt,
		OneShot:  t.OneShot,
		NoExpire: t.NoExpire,
		Created:  t.Created.Format(time.RFC3339),
		Fires:    t.Fires,
	}
	if !t.NextFire.IsZero() {
		v.NextFire = t.NextFire.Format(time.RFC3339)
	}
	return v
}

// mergeForSave builds the view list to persist: this session's tasks plus any
// on-disk tasks this session has never seen or deleted. The per-directory
// sidecar is shared by every chat in the folder, so a plain snapshot save
// would silently drop another session's tasks (last-writer-wins clobbering).
// Merge-on-save bounds the loss to genuinely conflicting edits of the same
// task ID; tombstones (s.deleted) stop a Delete in this session from being
// undone by a stale snapshot that still contains the task. Cross-process
// staleness remains for tasks another session holds in memory after this
// session deleted them — unavoidable without a shared live store.
func (s *Scheduler) mergeForSave() []View {
	live := s.Tasks()
	s.mu.Lock()
	deleted := make(map[string]bool, len(s.deleted))
	for id := range s.deleted {
		deleted[id] = true
	}
	path := s.persist
	s.mu.Unlock()
	if path == "" {
		return live
	}
	liveIDs := make(map[string]bool, len(live))
	for _, v := range live {
		liveIDs[v.ID] = true
	}
	var foreign []View
	for _, t := range loadTasks(path) {
		if t.ID == "" || liveIDs[t.ID] || deleted[t.ID] {
			continue
		}
		foreign = append(foreign, taskView(&t))
	}
	return append(live, foreign...)
}

// Count returns the number of active tasks.
func (s *Scheduler) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tasks)
}

// HasPendingDynamic reports whether any dynamic (agent-scheduled) task has a
// pending wakeup — the TUI uses this for Esc-to-pause.
func (s *Scheduler) HasPendingDynamic() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.CronExpr == "" && !t.NextFire.IsZero() {
			return true
		}
	}
	return false
}

// HasDynamic reports whether any dynamic (agent-scheduled) task exists at all,
// pending or paused — the TUI uses this to decide whether Esc should interact
// with loops instead of the rewind gesture.
func (s *Scheduler) HasDynamic() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.CronExpr == "" {
			return true
		}
	}
	return false
}

// NextDue returns the task with the earliest pending fire (id, time). ok is
// false when no task has a pending wakeup. The TUI renders this in the status
// footer as the next scheduled fire.
func (s *Scheduler) NextDue() (id string, at time.Time, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.NextFire.IsZero() {
			continue
		}
		if !ok || t.NextFire.Before(at) {
			id, at, ok = t.ID, t.NextFire, true
		}
	}
	return id, at, ok
}

// Stop terminates the ticker goroutine, waits for it to exit, and flushes any
// pending sidecar write. It is idempotent and does not clear tasks — a later
// Start on the same instance resumes firing.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	s.started = false
	close(s.stopCh)
	s.mu.Unlock()
	<-s.done
	s.Flush()
}

// Flush forces a persistence write now, bypassing the rate limiter. It is a
// no-op when no persist path is set.
func (s *Scheduler) Flush() {
	s.mu.Lock()
	path := s.persist
	s.mu.Unlock()
	if path == "" {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := saveTasks(path, s.mergeForSave()); err != nil {
		_ = err
	}
}

// saveLocked persists the task list to the configured sidecar (best-effort,
// rate-limited to one write per 250ms to keep bursty mutations cheap). The
// written set is merged with the on-disk snapshot so one session's save never
// drops another session's tasks (see mergeForSave).
func (s *Scheduler) saveLocked() {
	s.mu.Lock()
	path := s.persist
	if path == "" {
		s.mu.Unlock()
		return
	}
	if time.Since(s.lastSave) < 250*time.Millisecond {
		s.mu.Unlock()
		return
	}
	s.lastSave = time.Now()
	s.mu.Unlock()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := saveTasks(path, s.mergeForSave()); err != nil {
		// Persistence is best-effort; a failed sidecar write must not break
		// the loop machinery. The controller surfaces load errors separately.
		_ = err
	}
}

// saveNow persists immediately, bypassing the rate limiter. Deletion paths
// use it: a tombstone suppressed by the 250ms coalescing window would let a
// rebind (Load resets s.deleted) or crash right after a Delete resurrect the
// task from the stale disk snapshot.
func (s *Scheduler) saveNow() {
	s.mu.Lock()
	path := s.persist
	s.mu.Unlock()
	if path == "" {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := saveTasks(path, s.mergeForSave()); err != nil {
		_ = err
	}
}

// Load hydrates tasks from the per-directory store file, pruning expired,
// malformed, and missed one-shot entries, and capping the set at
// DefaultTaskLimit (newest first). It replaces any in-memory tasks first, so
// a rebind to a new session picks up whatever the working directory currently
// schedules (per-directory semantics: every chat in the folder shares one
// cron system). Recurring tasks roll their schedule forward so a fire missed
// during downtime does not fire immediately on resume; dynamic tasks keep
// their wakeup (a stale one simply fires, resuming the loop). It must be
// called before Start on the construction path.
func (s *Scheduler) Load(path string) {
	tasks := loadTasks(path)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := map[string]*Task{}
	for i := range tasks {
		t := &tasks[i]
		if t.ID == "" || t.Prompt == "" {
			continue // malformed entry
		}
		if !t.NoExpire && now.Sub(t.Created) > taskExpiry {
			continue // seven-day expiry (exempted by NoExpire)
		}
		if t.OneShot && !t.NextFire.After(now) {
			continue // missed one-shot: it already ran or is moot
		}
		if t.CronExpr != "" {
			t.NextFire = Next(t.CronExpr, now)
		}
		t.firing = false // a resumed task must be able to fire again
		kept[t.ID] = t
	}
	if len(kept) > DefaultTaskLimit {
		// The file is shared across sessions and Add already enforces the
		// limit per session, so a file can legitimately exceed it (several
		// chats each added up to the cap). Trim to the newest tasks so a
		// hostile or runaway store cannot flood the session.
		kept = newestTasks(kept, DefaultTaskLimit)
	}
	s.tasks = kept
	// A fresh session's source of truth is the file itself: forget every
	// tombstone the previous binding accumulated so the merged view matches
	// what is actually on disk.
	s.deleted = map[string]bool{}
	s.lastSave = time.Time{} // a rebind must not suppress the new session's first write
}

// newestTasks returns up to limit tasks from set, newest by Created first
// (ties broken by ID for determinism).
func newestTasks(set map[string]*Task, limit int) map[string]*Task {
	if len(set) <= limit {
		return set
	}
	all := make([]*Task, 0, len(set))
	for _, t := range set {
		all = append(all, t)
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].Created.Equal(all[j].Created) {
			return all[i].Created.After(all[j].Created)
		}
		return all[i].ID < all[j].ID
	})
	out := make(map[string]*Task, limit)
	for _, t := range all[:limit] {
		out[t.ID] = t
	}
	return out
}

// newTaskID returns an 8-char hex task id.
func newTaskID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
