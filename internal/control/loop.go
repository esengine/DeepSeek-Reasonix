package control

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
//	"/loop 5m check the deploy" — fixed cron schedule
//	"/loop check the deploy"    — dynamic: agent picks each delay via schedule_wakeup
//	"/loop 5m" / "/loop"        — loop.md (or the built-in maintenance prompt)
//
// A fixed interval is a leading token parseable by scheduler.ParseInterval;
// anything else is the prompt. A prompt-only loop is dynamic. No prompt at
// all falls back to loop.md then the maintenance prompt; with an interval it
// runs on the fixed schedule, without one it is dynamic.
func (c *Controller) StartLoop(input string) (string, error) {
	sched := c.scheduler
	if sched == nil {
		return "", fmt.Errorf("scheduler is unavailable in this session")
	}
	interval, prompt := parseLoopArgs(input)
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
	id, err := sched.Add(cronExpr, strings.TrimSpace(prompt), time.Now(), false)
	if err != nil {
		return "", err
	}
	if cronExpr != "" {
		return fmt.Sprintf("loop started — task %s: every %s\n%s", id, interval, promptPreview(prompt)), nil
	}
	return fmt.Sprintf("loop started — task %s: dynamic schedule, first fire now\n%s", id, promptPreview(prompt)), nil
}

// parseLoopArgs splits /loop arguments into an optional leading interval token
// and the remaining prompt.
func parseLoopArgs(input string) (interval, prompt string) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", ""
	}
	if _, ok := scheduler.ParseInterval(fields[0]); ok {
		return fields[0], strings.TrimSpace(strings.TrimPrefix(input, fields[0]))
	}
	return "", input
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

// runScheduledTurn fires a scheduled task as a turn between foreground turns.
// It is invoked from the scheduler's ticker goroutine; the work itself runs on
// the controller's turn machinery, parking while a foreground turn is active
// so a due task never interrupts mid-response. MarkStarted runs inside the
// admitted body so a parked fire keeps its firing flag (no duplicate queued
// turns) until the turn genuinely begins.
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

// Scheduler exposes the session scheduler to frontends (the TUI's Esc-to-pause
// reads HasPendingDynamic through it).
func (c *Controller) Scheduler() *scheduler.Scheduler {
	return c.scheduler
}
