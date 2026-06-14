package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agent"
)

const goalContinuationLimitReason = "goal continuation limit reached"

// Scheduler runs periodic checks on all tracked sessions and triggers wakeups
// when their schedule fires. It respects guards: goal must be active, run must
// not be in-flight, no duplicate wakeup for the same event, and max turns not
// exceeded.
type Scheduler struct {
	daemon   *Daemon
	logger   *slog.Logger
	interval time.Duration // tick interval for checking schedules

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

// NewScheduler creates a scheduler bound to the given daemon.
func NewScheduler(d *Daemon, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		daemon:   d,
		logger:   logger,
		interval: 30 * time.Second, // check every 30s
	}
}

// Start begins the scheduling loop. It runs until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.running = true
	s.mu.Unlock()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

// Stop halts the scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
}

// tick evaluates every tracked session's schedule and fires wakeups as needed.
func (s *Scheduler) tick() {
	s.daemon.mu.RLock()
	entries := make([]SessionEntry, 0, len(s.daemon.registry))
	for _, e := range s.daemon.registry {
		entries = append(entries, *e)
	}
	s.daemon.mu.RUnlock()

	now := time.Now()
	for _, entry := range entries {
		if s.shouldWakeupTime(&entry, now) {
			s.wakeupTimeSession(entry.ID, now)
		} else if s.shouldWakeup(&entry, now) {
			s.wakeupSession(entry.ID, now)
		}
	}
}

// shouldWakeup performs deterministic pre-checks before triggering a wakeup:
//  1. Scheduler is enabled
//  2. Goal is active (running or blocked)
//  3. Run is not already in-flight
//  4. NextWakeupAt has passed
//  5. No duplicate schedule window (LastWakeupEventID/LastWakeupKey check)
func (s *Scheduler) shouldWakeup(entry *SessionEntry, now time.Time) bool {
	return s.shouldWakeupRuntime(entry.ID, entry.Runtime, now)
}

func (s *Scheduler) shouldWakeupRuntime(id string, runtime agent.RuntimeMeta, now time.Time) bool {
	sched := runtime.Scheduler
	if !sched.Enabled {
		return false
	}

	// Goal must be active.
	goalStatus := runtime.Goal.Status
	if goalStatus != "running" && goalStatus != "blocked" {
		return false
	}
	if isGoalContinuationLimitBlocked(runtime.Goal) {
		return false
	}

	// Run must not be in-flight.
	if agent.IsRunInFlight(runtime.Run.Status) {
		return false
	}

	due := s.dueTime(sched, now)
	if due.IsZero() {
		return false
	}

	// Dedup: don't fire the same schedule window twice, even if the daemon
	// restarts or the tick arrives late in the same window.
	eventID := s.eventIDFor(id, sched, due)
	key := s.wakeupKeyFor(id, sched, due)
	if eventID == sched.LastWakeupEventID || (key != "" && key == sched.LastWakeupKey) {
		return false
	}

	return true
}

func (s *Scheduler) shouldWakeupTime(entry *SessionEntry, now time.Time) bool {
	return s.shouldWakeupTimeRuntime(entry.ID, entry.Runtime, now)
}

func (s *Scheduler) shouldWakeupTimeRuntime(id string, runtime agent.RuntimeMeta, now time.Time) bool {
	wait := runtime.Wait
	if wait.Kind != "time" || wait.Until.IsZero() || wait.Until.After(now) {
		return false
	}
	goalStatus := runtime.Goal.Status
	if goalStatus != "running" && goalStatus != "blocked" {
		return false
	}
	if isGoalContinuationLimitBlocked(runtime.Goal) {
		return false
	}
	if agent.IsRunInFlight(runtime.Run.Status) {
		return false
	}
	key := s.timeWaitKeyFor(id, wait)
	if key != "" && key == runtime.Scheduler.LastWakeupKey &&
		runtime.Scheduler.LastWakeupReason == "budget_blocked:time" &&
		!runtime.Budget.LastBlockedAt.IsZero() &&
		budgetWindowStart(runtime.Budget.LastBlockedAt).Equal(budgetWindowStart(now)) {
		return false
	}
	return true
}

func isGoalContinuationLimitBlocked(goal agent.RuntimeGoalMeta) bool {
	return goal.Status == "blocked" && strings.EqualFold(strings.TrimSpace(goal.BlockReason), goalContinuationLimitReason)
}

// wakeup fires a scheduled continuation for the session.
func (s *Scheduler) wakeup(entry *SessionEntry, now time.Time) {
	s.wakeupSession(entry.ID, now)
}

func (s *Scheduler) wakeupTimeSession(id string, now time.Time) {
	s.daemon.mu.Lock()
	entry, ok := s.daemon.registry[id]
	if !ok || !s.shouldWakeupTimeRuntime(entry.ID, entry.Runtime, now) {
		s.daemon.mu.Unlock()
		return
	}
	wait := entry.Runtime.Wait
	eventID := s.timeWaitEventIDFor(entry.ID, wait)
	wakeupKey := s.timeWaitKeyFor(entry.ID, wait)
	if ok, reason := reserveAutoWakeupBudget(&entry.Runtime, "time", now); !ok {
		entry.Runtime.Scheduler.LastWakeupAt = now
		entry.Runtime.Scheduler.LastWakeupReason = "budget_blocked:time"
		entry.Runtime.Scheduler.LastWakeupEventID = eventID
		entry.Runtime.Scheduler.LastWakeupKey = wakeupKey
		runtime := entry.Runtime
		path := entry.Path
		s.daemon.mu.Unlock()
		if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
			s.logger.Warn("scheduler: save runtime after time wait budget block", "err", err, "session", id)
		}
		s.daemon.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       "wakeup_budget_blocked",
			Source:     "time",
			Reason:     reason,
			EventID:    eventID,
			Step:       "deterministic",
			RunStatus:  runtime.Run.Status,
			GoalStatus: runtime.Goal.Status,
			WaitKind:   wait.Kind,
			Subject:    wait.Subject,
			Message:    reason,
		})
		return
	}

	entry.Runtime.Run.Status = agent.RunStatusQueued
	entry.Runtime.Run.LastWakeupReason = "time"
	entry.Runtime.Run.ResumeCount++
	entry.Runtime.Wait = agent.RuntimeWaitMeta{}
	entry.Runtime.Scheduler.LastWakeupAt = now
	entry.Runtime.Scheduler.LastWakeupReason = "time"
	entry.Runtime.Scheduler.LastWakeupEventID = eventID
	entry.Runtime.Scheduler.LastWakeupKey = wakeupKey
	runtime := entry.Runtime
	path := entry.Path
	s.daemon.mu.Unlock()

	if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
		s.logger.Warn("scheduler: save runtime after time wait wakeup", "err", err, "session", id)
	}
	s.daemon.appendTimeline(path, agent.RuntimeTimelineEvent{
		Type:       "wait_time_reached",
		Source:     "time",
		EventID:    eventID,
		Step:       "deterministic",
		RunStatus:  runtime.Run.Status,
		GoalStatus: runtime.Goal.Status,
		Subject:    wait.Subject,
		Reason:     wait.Reason,
	})
	s.daemon.enqueueIntent(RunIntent{
		SessionID:   id,
		SessionPath: path,
		Source:      "time",
		Reason:      "time",
		EventID:     eventID,
		Context:     boundedTimeWaitContext(wait),
	})
}

func (s *Scheduler) wakeupSession(id string, now time.Time) {
	s.daemon.mu.Lock()
	entry, ok := s.daemon.registry[id]
	if !ok || !s.shouldWakeupRuntime(entry.ID, entry.Runtime, now) {
		s.daemon.mu.Unlock()
		return
	}

	due := s.dueTime(entry.Runtime.Scheduler, now)
	if due.IsZero() {
		s.daemon.mu.Unlock()
		return
	}
	eventID := s.eventIDFor(entry.ID, entry.Runtime.Scheduler, due)
	wakeupKey := s.wakeupKeyFor(entry.ID, entry.Runtime.Scheduler, due)
	sched := entry.Runtime.Scheduler
	previousRunStatus := entry.Runtime.Run.Status
	s.logger.Info("scheduler wakeup", "session", entry.ID, "event", eventID)
	if ok, reason := reserveAutoWakeupBudget(&entry.Runtime, "cron", now); !ok {
		entry.Runtime.Scheduler.LastWakeupAt = now
		entry.Runtime.Scheduler.LastWakeupReason = "budget_blocked:cron"
		entry.Runtime.Scheduler.LastWakeupEventID = eventID
		entry.Runtime.Scheduler.LastWakeupKey = wakeupKey
		entry.Runtime.Scheduler.NextWakeupAt = s.computeNextWakeup(entry.Runtime.Scheduler, now)
		runtime := entry.Runtime
		path := entry.Path
		s.daemon.mu.Unlock()
		if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
			s.logger.Warn("scheduler: save runtime after budget block", "err", err, "session", id)
		}
		s.daemon.appendTimeline(path, agent.RuntimeTimelineEvent{
			Type:       "wakeup_budget_blocked",
			Source:     "cron",
			Reason:     reason,
			EventID:    eventID,
			Step:       "deterministic",
			RunStatus:  runtime.Run.Status,
			GoalStatus: runtime.Goal.Status,
			Message:    reason,
		})
		return
	}

	entry.Runtime.Scheduler.LastWakeupAt = now
	entry.Runtime.Scheduler.LastWakeupReason = "cron"
	entry.Runtime.Scheduler.LastWakeupEventID = eventID
	entry.Runtime.Scheduler.LastWakeupKey = wakeupKey
	entry.Runtime.Run.Status = agent.RunStatusQueued
	entry.Runtime.Run.LastWakeupReason = "cron"
	entry.Runtime.Run.ResumeCount++

	// Advance NextWakeupAt.
	next := s.computeNextWakeup(entry.Runtime.Scheduler, now)
	entry.Runtime.Scheduler.NextWakeupAt = next
	runtime := entry.Runtime
	path := entry.Path
	s.daemon.mu.Unlock()

	// Persist the updated runtime.
	if err := agent.SaveRuntimeMeta(path, runtime); err != nil {
		s.logger.Warn("scheduler: save runtime after wakeup", "err", err, "session", id)
	}
	s.daemon.enqueueIntent(RunIntent{
		SessionID:   id,
		SessionPath: path,
		Source:      "cron",
		Reason:      "cron",
		EventID:     eventID,
		Context:     boundedCronWakeupContext(sched, due, next, eventID, wakeupKey, previousRunStatus),
	})
}

func (s *Scheduler) dueTime(sched agent.RuntimeSchedMeta, now time.Time) time.Time {
	if !sched.NextWakeupAt.IsZero() {
		if sched.NextWakeupAt.After(now) {
			return time.Time{}
		}
		return sched.NextWakeupAt
	}
	next := s.computeNextWakeup(sched, now)
	if next.IsZero() || next.After(now) {
		return time.Time{}
	}
	return next
}

// computeNextWakeup determines when the next wakeup should fire based on the
// schedule configuration.
func (s *Scheduler) computeNextWakeup(sched agent.RuntimeSchedMeta, after time.Time) time.Time {
	if sched.Interval > 0 {
		// Fixed interval from last wakeup (or now if never fired).
		base := sched.LastWakeupAt
		if base.IsZero() {
			base = after
		}
		next := base.Add(sched.Interval)
		// If next is in the past, advance to the next interval from now.
		for next.Before(after) {
			next = next.Add(sched.Interval)
		}
		return next
	}

	if sched.DailyAt != "" {
		return nextDailyTimeInLocation(sched.DailyAt, after, scheduleLocation(sched, after))
	}

	return time.Time{}
}

// eventID generates a dedup key for a wakeup event.
func (s *Scheduler) eventID(entry *SessionEntry, t time.Time) string {
	// Use session + schedule-window timestamp as a simple dedup key.
	// For daily: round to the day. For interval: round to the interval.
	return s.eventIDFor(entry.ID, entry.Runtime.Scheduler, t)
}

func (s *Scheduler) eventIDFor(id string, sched agent.RuntimeSchedMeta, t time.Time) string {
	if sched.DailyAt != "" {
		return fmt.Sprintf("daily:%s:%s", id, t.In(scheduleLocation(sched, t)).Format("2006-01-02"))
	}
	if sched.Interval > 0 {
		seconds := int64(sched.Interval.Seconds())
		if seconds <= 0 {
			seconds = 1
		}
		epoch := t.Unix() / seconds
		return fmt.Sprintf("interval:%s:%d", id, epoch)
	}
	return fmt.Sprintf("manual:%s:%d", id, t.Unix())
}

func (s *Scheduler) wakeupKeyFor(id string, sched agent.RuntimeSchedMeta, due time.Time) string {
	if due.IsZero() {
		return ""
	}
	return "schedule:" + s.eventIDFor(id, sched, due)
}

func (s *Scheduler) timeWaitEventIDFor(id string, wait agent.RuntimeWaitMeta) string {
	unix := wait.Until.UTC().Unix()
	if unix < 0 {
		unix = 0
	}
	return fmt.Sprintf("time:%s:%d", id, unix)
}

func (s *Scheduler) timeWaitKeyFor(id string, wait agent.RuntimeWaitMeta) string {
	if wait.Until.IsZero() {
		return ""
	}
	return "wait:" + s.timeWaitEventIDFor(id, wait)
}

func boundedTimeWaitContext(wait agent.RuntimeWaitMeta) string {
	parts := []string{"Time wait reached; continue the active goal."}
	if !wait.Until.IsZero() {
		parts = append(parts, "until="+wait.Until.UTC().Format(time.RFC3339))
	}
	if wait.Subject != "" {
		parts = append(parts, "subject="+wait.Subject)
	}
	if wait.Reason != "" {
		parts = append(parts, "reason="+wait.Reason)
	}
	return strings.Join(parts, " ")
}

func boundedCronWakeupContext(sched agent.RuntimeSchedMeta, due, next time.Time, eventID, wakeupKey, previousRunStatus string) string {
	parts := []string{"Cron wakeup reached; continue the active goal."}
	if eventID != "" {
		parts = append(parts, "event_id="+eventID)
	}
	if wakeupKey != "" {
		parts = append(parts, "schedule_id="+wakeupKey)
	}
	if previousRunStatus != "" {
		parts = append(parts, "previous_run_status="+previousRunStatus)
	}
	if sched.DailyAt != "" {
		parts = append(parts, "daily_at="+sched.DailyAt)
		if sched.Timezone != "" {
			parts = append(parts, "timezone="+sched.Timezone)
		}
	}
	if sched.Interval > 0 {
		parts = append(parts, "interval="+sched.Interval.String())
	}
	if !due.IsZero() {
		parts = append(parts, "due_at="+due.UTC().Format(time.RFC3339))
	}
	if !next.IsZero() {
		parts = append(parts, "next_wakeup_at="+next.UTC().Format(time.RFC3339))
	}
	return strings.Join(parts, " ")
}

// nextDailyTime parses "HH:MM" and returns the next occurrence after `after`
// in after's local time.
func nextDailyTime(spec string, after time.Time) time.Time {
	return nextDailyTimeInLocation(spec, after, after.Location())
}

func nextDailyTimeInLocation(spec string, after time.Time, loc *time.Location) time.Time {
	var hour, min int
	if _, err := fmt.Sscanf(spec, "%d:%d", &hour, &min); err != nil {
		return time.Time{}
	}
	if hour < 0 || hour > 23 || min < 0 || min > 59 {
		return time.Time{}
	}

	if loc == nil {
		loc = after.Location()
	}
	localAfter := after.In(loc)
	today := time.Date(localAfter.Year(), localAfter.Month(), localAfter.Day(), hour, min, 0, 0, loc)
	if today.After(localAfter) {
		return today
	}
	return time.Date(localAfter.Year(), localAfter.Month(), localAfter.Day()+1, hour, min, 0, 0, loc)
}

func scheduleLocation(sched agent.RuntimeSchedMeta, fallback time.Time) *time.Location {
	name := strings.TrimSpace(sched.Timezone)
	if name == "" {
		return fallback.Location()
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return fallback.Location()
	}
	return loc
}
