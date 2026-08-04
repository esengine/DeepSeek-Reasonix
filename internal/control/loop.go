package control

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/scheduler"
)

// loopMaintenancePrompt is the built-in prompt for a bare /loop (no interval,
// no prompt). It continues work already in flight without starting new
// initiatives — the loop's job is tending, not exploring.
const loopMaintenancePrompt = `Continue the current session's ongoing work:

1. Continue any unfinished work from the conversation, in order.
2. Tend to the current branch's pull request: new review comments, failed CI runs, merge conflicts.
3. Run cleanup passes (bug hunts, simplification) only when nothing else is pending.

Do not start new initiatives outside that scope. Irreversible actions (pushing, deleting) proceed only when they continue something the transcript already authorized. When everything is handled, say so in one line.`

// maxLoopMDBytes caps loop.md content; longer files are truncated (matching
// the 25,000-byte budget the reference implementation uses).
const maxLoopMDBytes = 25_000

// StartLoop creates a scheduled task from a /loop command's arguments and
// returns a human-readable confirmation. Interval and prompt are both
// optional:
//
//	"/loop 5m check the deploy"      — fixed cron schedule
//	"/loop check the deploy"         — dynamic: agent picks each delay via schedule_wakeup
//	"/loop --forever 5m check deploy" — endless: exempt from the 7-day expiry
//	"/loop 5m" / "/loop"             — loop.md (or the built-in maintenance prompt)
//
// A fixed interval is a leading token parseable by scheduler.ParseInterval;
// anything else is the prompt. A prompt-only loop is dynamic. No prompt at
// all falls back to loop.md then the maintenance prompt; with an interval it
// runs on the fixed schedule, without one it is dynamic. A leading
// "--forever" marks the task NoExpire so it survives session resume past the
// 7-day prune (endless cron jobs).
func (c *Controller) StartLoop(input string) (string, error) {
	sched := c.scheduler
	if sched == nil {
		return "", fmt.Errorf("scheduler is unavailable in this session")
	}
	interval, prompt, noExpire := parseLoopArgs(input)
	if strings.TrimSpace(prompt) == "" {
		prompt = c.loadLoopMD()
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = loopMaintenancePrompt
	}
	var cronExpr string
	if interval != "" {
		cronExpr, _ = scheduler.ParseInterval(interval)
	}
	id, err := sched.Add(cronExpr, strings.TrimSpace(prompt), time.Now(), false, noExpire)
	if err != nil {
		return "", err
	}
	note := ""
	if noExpire {
		note = " (no expiry)"
	}
	if cronExpr != "" {
		return fmt.Sprintf("loop started — task %s: every %s%s\n%s", id, interval, note, promptPreview(prompt)), nil
	}
	return fmt.Sprintf("loop started — task %s: dynamic schedule, first fire now%s\n%s", id, note, promptPreview(prompt)), nil
}

// parseLoopArgs splits /loop arguments into an optional leading --forever
// flag, an optional interval token, and the remaining prompt.
func parseLoopArgs(input string) (interval, prompt string, noExpire bool) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", "", false
	}
	rest := input
	if fields[0] == "--forever" {
		noExpire = true
		rest = strings.TrimSpace(strings.TrimPrefix(rest, fields[0]))
		fields = strings.Fields(rest)
		if len(fields) == 0 {
			return "", "", true
		}
	}
	if _, ok := scheduler.ParseInterval(fields[0]); ok {
		return fields[0], strings.TrimSpace(strings.TrimPrefix(rest, fields[0])), noExpire
	}
	return "", rest, noExpire
}

// promptPreview shortens a loop prompt for confirmation/notice text.
func promptPreview(prompt string) string {
	p := strings.Join(strings.Fields(prompt), " ")
	if len(p) > 120 {
		return p[:117] + "..."
	}
	return p
}

// loadLoopMD returns the default loop prompt from loop.md: the project file
// (<root>/.reasonix/loop.md) wins over the user file (<home>/loop.md). It is
// re-read on every call so edits take effect on the next iteration; an empty
// result means no loop.md exists (callers fall back to the built-in prompt).
func (c *Controller) loadLoopMD() string {
	candidates := []string{}
	if root := c.workspaceRoot; strings.TrimSpace(root) != "" {
		candidates = append(candidates, filepath.Join(root, ".reasonix", "loop.md"))
	}
	candidates = append(candidates, filepath.Join(config.ReasonixHomeDir(), "loop.md"))
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(data) > maxLoopMDBytes {
			data = data[:maxLoopMDBytes]
		}
		if text := strings.TrimSpace(string(data)); text != "" {
			return text
		}
	}
	return ""
}

// runScheduledTurn fires a scheduled task, either by steering the prompt into
// the active turn's message queue (mid-turn fire — the agent picks it up at
// its next natural step) or, when no active turn can accept it, as a turn
// between foreground turns. It is invoked from the scheduler's ticker
// goroutine. MarkStarted runs on the accepted delivery path (injectScheduledTask)
// or inside the admitted body so a parked fire keeps its firing flag (no
// duplicate queued turns) until the turn genuinely begins.
func (c *Controller) runScheduledTurn(task scheduler.Task) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		// Contract: a delivered-but-unstarted fire must never leave the
		// task's firing flag set, even when the controller is already torn
		// down — release it so any survivor path (or a rebind that skips
		// Load) cannot wedge the task.
		c.scheduler.ReleaseFiring(task.ID)
		return
	}
	if c.injectScheduledTask(task) {
		// The fire was delivered into the running turn's message queue:
		// MarkStarted consumed the firing flag and re-armed the cron
		// schedule from now, so cycles that passed while the turn ran are
		// skipped instead of catching up.
		return
	}
	result := c.runGuardedOrPark(func(ctx context.Context) error {
		// Notice inside the admitted body: a fire that parks (or is dropped
		// during rotation) does not spam "fired" notices — the user only sees
		// one when the turn actually begins.
		c.scheduler.MarkStarted(task.ID)
		c.notice(fmt.Sprintf("⏰ scheduled task %s running", task.ID))
		return c.runGoalLoopWithRaw(ctx, task.Prompt, task.Prompt)
	})
	if result != turnStarted && result != turnParked {
		// Admission dropped the turn (controller rotating or closed) so the
		// body will never call MarkStarted. Release the firing flag: a cron
		// task re-fires on the next tick, a dynamic task stays paused instead
		// of silently dying with its wakeup consumed.
		c.scheduler.ReleaseFiring(task.ID)
	}
}

// injectScheduledTask delivers a due scheduled task into the active turn's
// message queue as a labeled steering message. It reports whether the fire
// was accepted: true means the running agent will see the prompt at its next
// natural step (between tool rounds, never mid-tool); false means the caller
// falls back to the parked-turn path (turn in its finishing window, steer
// intake already closed, or no executor bound).
//
// Consent posture: steering runs inside the CURRENT turn's approval context —
// any tool calls the model makes in response pass the same approval gates as
// the running turn's own calls, so a scheduled fire opens no new consent
// surface. The injected message is explicitly labeled as a scheduled task
// (never as the user) and the notice shows a promptPreview, so a poisoned
// prompt is visible in the transcript and UI rather than silently executed.
func (c *Controller) injectScheduledTask(task scheduler.Task) bool {
	if !c.TrySteer(agent.MidTurnScheduledMessage(task.ID, task.Prompt)) {
		return false
	}
	c.scheduler.MarkStarted(task.ID)
	c.notice(fmt.Sprintf("⏰ scheduled task %s injected into the running turn — %s", task.ID, promptPreview(task.Prompt)))
	return true
}

// rearmUnappliedScheduledTask retries a scheduled fire whose injection was
// accepted into the steer queue but never reached the model: the turn ended
// abnormally (cancel, provider error, rotation) and the steer was flushed
// unapplied. Rearming makes the task due again on the next tick — matching
// the parked path's ReleaseFiring semantics for dropped turns, so a fire is
// never silently spent. User steers and plain text carry no scheduled-task
// label and are ignored.
func (c *Controller) rearmUnappliedScheduledTask(text string) {
	id, ok := agent.ScheduledTaskID(text)
	if !ok {
		return
	}
	c.mu.Lock()
	sched := c.scheduler
	c.mu.Unlock()
	if sched != nil {
		sched.Rearm(id)
	}
}

// Scheduler exposes the session scheduler to frontends (the TUI's Esc-to-pause
// reads HasPendingDynamic through it).
func (c *Controller) Scheduler() *scheduler.Scheduler {
	return c.scheduler
}

// LoopListText renders the session's scheduled tasks for a local slash command
// (/looplist) — no model call, no tokens spent.
func (c *Controller) LoopListText() string {
	if c.scheduler == nil {
		return "scheduled tasks are unavailable in this session"
	}
	views := c.scheduler.Tasks()
	if len(views) == 0 {
		return "no scheduled tasks"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d scheduled task(s):\n", len(views))
	for _, v := range views {
		schedule := "dynamic"
		if v.CronExpr != "" {
			schedule = v.CronExpr
		}
		next := v.NextFire
		if next == "" {
			next = "paused"
		}
		oneShot := ""
		if v.OneShot {
			oneShot = " (one-shot)"
		}
		noExpire := ""
		if v.NoExpire {
			noExpire = " (no expiry)"
		}
		fmt.Fprintf(&b, "  %s  %-14s  next %s%s%s\n", v.ID, schedule, next, oneShot, noExpire)
	}
	return strings.TrimRight(b.String(), "\n")
}

// LoopDeleteText cancels a scheduled task by ID for a local slash command
// (/loopdel <id>) — no model call. Returns the confirmation text.
func (c *Controller) LoopDeleteText(id string) string {
	if c.scheduler == nil {
		return "scheduled tasks are unavailable in this session"
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "usage: /loopdel <task-id>"
	}
	if c.scheduler.Delete(id) {
		return "deleted scheduled task " + id
	}
	return "no scheduled task " + id
}
