// Precheck gate support for the heartbeat engine — optional per-task commands
// run before each execution. Non-zero exit skips the run (exit 2 = business
// skip, anything else = gate failure); stdin mirrors hook payloads.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/proc"
)

// HeartbeatPrecheckRun records a single precheck gate execution outcome.
type HeartbeatPrecheckRun struct {
	At      int64  `json:"at"`      // unix millis execution time
	Status  string `json:"status"`  // "passed" | "skipped" | "failed"
	Summary string `json:"summary"` // human-readable outcome (pass note, skip reason, or failure detail)
}

// precheckOutcome classifies a precheck gate result into the three states the
// engine acts on: pass (run the task), skip (business decision, e.g. no tasks
// waiting), and fail (the gate itself is broken — also blocks the run but is
// recorded distinctly so a broken script is not mistaken for a normal skip).
type precheckOutcome int

const (
	precheckPass precheckOutcome = iota
	precheckSkip
	precheckFail
)

// maxPrecheckHistory caps how many recent precheck gate outcomes are kept.
const maxPrecheckHistory = 20

// runPrecheckGate runs a task's precheck command when configured, records the
// outcome in PrecheckHistory, and returns whether the run must be skipped.
// Skipped runs still advance LastRunAt so the gate is re-evaluated next tick.
func (e *HeartbeatEngine) runPrecheckGate(t HeartbeatTask) (HeartbeatTask, bool) {
	if t.Precheck == "" {
		return t, false
	}
	outcome, summary := e.runPrecheck(t)
	now := time.Now().UnixMilli()
	status := "passed"
	switch outcome {
	case precheckSkip:
		status = "skipped"
	case precheckFail:
		status = "failed"
	}
	t.PrecheckHistory = append(t.PrecheckHistory, HeartbeatPrecheckRun{At: now, Status: status, Summary: summary})
	if len(t.PrecheckHistory) > maxPrecheckHistory {
		t.PrecheckHistory = t.PrecheckHistory[len(t.PrecheckHistory)-maxPrecheckHistory:]
	}
	if outcome != precheckPass {
		t.LastSkippedAt = now
		if status == "failed" {
			// A broken gate must not masquerade as a normal skip: it still
			// blocks the run, but the recorded reason is clearly marked.
			t.LastSkippedReason = "precheck failed: " + summary
		} else {
			t.LastSkippedReason = summary
		}
		t.LastRunAt = now
		log.Printf("[heartbeat] task %q precheck %s: %s", t.Title, status, summary)
		return t, true
	}
	return t, false
}

// heartbeatPrecheckTimeout bounds a single precheck command run.
const heartbeatPrecheckTimeout = 10 * time.Second

// precheckPayload is the JSON written to the precheck command's stdin, shaped
// like a hook payload so the same scripts can be reused for both.
type precheckPayload struct {
	Event         string `json:"event"`
	Cwd           string `json:"cwd"`
	WorkspaceRoot string `json:"workspaceRoot,omitempty"`
	TaskID        string `json:"taskId"`
	TaskTitle     string `json:"taskTitle"`
}

// runPrecheck executes a task's precheck command. It returns ok=false with a
// reason when the gate fails (non-zero exit, timeout, or spawn error). The
// command runs with the task's workspace root as cwd (the process cwd for
// global tasks) and receives a JSON payload on stdin, mirroring hooks.
func (e *HeartbeatEngine) runPrecheck(t HeartbeatTask) (precheckOutcome, string) {
	ctx, cancel := context.WithTimeout(context.Background(), heartbeatPrecheckTimeout)
	defer cancel()
	var shell, flag string
	if runtime.GOOS == "windows" {
		shell, flag = "cmd", "/c"
	} else {
		shell, flag = "sh", "-c"
	}
	cmd := proc.CommandContext(ctx, shell, flag, t.Precheck)
	cwd := t.WorkspaceRoot
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	cmd.Dir = cwd
	payload, err := json.Marshal(precheckPayload{
		Event:         "HeartbeatPrecheck",
		Cwd:           cwd,
		WorkspaceRoot: t.WorkspaceRoot,
		TaskID:        t.ID,
		TaskTitle:     t.Title,
	})
	if err != nil {
		return precheckFail, "cannot build precheck payload: " + err.Error()
	}
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		// Passed: surface the script's stdout (if any) as the pass note so the
		// UI can show what the gate observed (e.g. "3 tasks waiting").
		summary := strings.TrimSpace(stdout.String())
		if summary == "" {
			summary = "passed"
		}
		return precheckPass, truncateHeartbeatReason(summary)
	}
	reason := strings.TrimSpace(stderr.String())
	if reason == "" {
		reason = strings.TrimSpace(stdout.String())
	}
	if reason == "" {
		reason = err.Error()
	}
	if ctx.Err() == context.DeadlineExceeded {
		reason = "precheck timed out after " + heartbeatPrecheckTimeout.String() + ": " + reason
		return precheckFail, truncateHeartbeatReason(reason)
	}
	// exit 2 is the business "skip" signal (e.g. no tasks waiting); any other
	// non-zero exit or a spawn error is a gate failure.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
		return precheckSkip, truncateHeartbeatReason(reason)
	}
	return precheckFail, truncateHeartbeatReason(reason)
}

// truncateHeartbeatReason caps a skipped-reason string for display/storage.
func truncateHeartbeatReason(s string) string {
	const max = 400
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// PrecheckTestResult is the Wails contract for a manual precheck test run. It
// mirrors the three-state gate outcome so the UI can show what a precheck
// command would do without waiting for the next scheduled tick.
type PrecheckTestResult struct {
	Status  string `json:"status"`  // "passed" | "skipped" | "failed"
	Summary string `json:"summary"` // human-readable outcome
}

// TestPrecheck runs a precheck command once, in the given workspace root, and
// returns the resulting outcome. It is read-only (the gate itself does not
// create topics or submit prompts), so it is safe to call from the UI.
func (e *HeartbeatEngine) TestPrecheck(command, workspaceRoot string) PrecheckTestResult {
	if strings.TrimSpace(command) == "" {
		return PrecheckTestResult{Status: "failed", Summary: "empty precheck command"}
	}
	t := HeartbeatTask{Precheck: command, WorkspaceRoot: workspaceRoot, Title: "precheck test"}
	outcome, summary := e.runPrecheck(t)
	status := "failed"
	switch outcome {
	case precheckPass:
		status = "passed"
	case precheckSkip:
		status = "skipped"
	}
	return PrecheckTestResult{Status: status, Summary: summary}
}

// HeartbeatTestPrecheck runs a precheck command once for the UI "test" button.
func (a *App) HeartbeatTestPrecheck(precheckCommand, workspaceRoot string) PrecheckTestResult {
	if a.heartbeat == nil {
		return PrecheckTestResult{Status: "failed", Summary: "heartbeat engine not available"}
	}
	return a.heartbeat.TestPrecheck(precheckCommand, workspaceRoot)
}
