package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/tools/jobs"
)

// bash_output / kill_shell / wait operate the background jobs registered by
// bash(run_in_background) and task(run_in_background). They reach the session's
// job manager through the call context (jobs.FromContext) — the agent stamps it
// onto every tool call — and degrade to a clear error when it isn't available
// (a headless context with no manager). Together they poll a job's new output,
// terminate a job, and block until jobs finish.

func init() {
	tool.RegisterBuiltin(bashOutput{})
	tool.RegisterBuiltin(killShell{})
	tool.RegisterBuiltin(waitJob{})
}

// bash_output: poll a background job's new output (non-blocking)

// The three background-job tools are unavailable for one and the same reason,
// so they say it once. What makes them available is a job context on the turn.
type noJobs struct{}

// The fact all three prove is the same one: this turn carries no job context.
var noJobContext = tool.Refusal{
	Code:    "jobs.no_job_context",
	Message: "background jobs are not available in this context",
}

func (noJobs) Unavailable(context.Context) tool.Refusal { return noJobContext }

type bashOutput struct{ noJobs }

func (bashOutput) Name() string { return "bash_output" }

func (bashOutput) Description() string {
	return "Read new output from a background job started with bash(run_in_background=true) or task(run_in_background=true). Returns the output produced since the last bash_output call for that job, plus its status (running/done/failed/killed). Does not block."
}

func (bashOutput) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"job_id":{"type":"string","description":"The background job id (e.g. \"bash-1\") returned when it was started."},"filter":{"type":"string","description":"Optional regular expression; only matching lines of the new output are returned."}},"required":["job_id"]}`)
}

func (bashOutput) ReadOnly() bool { return true }

// Sequential is true even though ReadOnly is: it reads what a job started
// earlier in this same reply has produced, so it runs after, not beside.
func (bashOutput) Sequential(context.Context, json.RawMessage) bool { return true }

func (bashOutput) ProviderVisible(ctx context.Context) bool {
	_, ok := jobs.FromContext(ctx)
	return ok
}

func (bashOutput) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		JobID  string `json:"job_id"`
		Filter string `json:"filter"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.JobID == "" {
		return "", fmt.Errorf("job_id is required")
	}
	jm, ok := jobs.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("background jobs are not available in this context")
	}
	text, status, found := jm.OutputForSession(jobs.SessionFromContext(ctx), p.JobID)
	if !found {
		return "", fmt.Errorf("no background job %q", p.JobID)
	}
	if status != jobs.Running {
		collectBackgroundEvidence(ctx, jm, p.JobID)
	}
	if p.Filter != "" && text != "" {
		filtered, err := filterLines(text, p.Filter)
		if err != nil {
			return "", err
		}
		text = filtered
	}
	header := fmt.Sprintf("[%s] %s%s", p.JobID, status, status.Cause())
	if strings.TrimSpace(text) == "" {
		return header + "\n(no new output)", nil
	}
	return header + "\n" + text, nil
}

// filterLines keeps only the lines of s matching the regular expression re.
func filterLines(s, re string) (string, error) {
	rx, err := regexp.Compile(re)
	if err != nil {
		return "", fmt.Errorf("invalid filter regexp: %w", err)
	}
	var keep []string
	for line := range strings.SplitSeq(s, "\n") {
		if rx.MatchString(line) {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n"), nil
}

// kill_shell: terminate a running background job

type killShell struct{ noJobs }

func (killShell) Name() string { return "kill_shell" }

func (killShell) Description() string {
	return "Terminate a running background job (bash or task) started with run_in_background. A no-op if the job has already finished or the id is unknown."
}

func (killShell) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"job_id":{"type":"string","description":"The background job id to terminate (e.g. \"bash-1\")."}},"required":["job_id"]}`)
}

func (killShell) ReadOnly() bool { return false }

func (killShell) ProviderVisible(ctx context.Context) bool {
	_, ok := jobs.FromContext(ctx)
	return ok
}

func (killShell) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.JobID == "" {
		return "", fmt.Errorf("job_id is required")
	}
	jm, ok := jobs.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("background jobs are not available in this context")
	}
	if jm.KillForSession(jobs.SessionFromContext(ctx), p.JobID) {
		return fmt.Sprintf("Killed background job %q.", p.JobID), nil
	}
	return fmt.Sprintf("Background job %q was not running (already finished or unknown).", p.JobID), nil
}

// wait: block until background jobs finish, then return their results

type waitJob struct{ noJobs }

func (waitJob) Name() string { return "wait" }

func (waitJob) Description() string {
	return "Block until background jobs finish, then return each job's status and final output/answer. Use to collect the result of a task(run_in_background) or bash(run_in_background) before continuing. Omit job_ids to wait for every running job."
}

func (waitJob) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"job_ids":{"type":"array","items":{"type":"string"},"description":"Background job ids to wait for. Omit to wait for every currently-running job."},"timeout_seconds":{"type":"integer","description":"Optional maximum seconds to block before returning current progress. Omit to wait until the jobs finish — one long wait costs one round, where polling in short steps costs one per step.","minimum":1},"until_output_matches":{"type":"string","description":"Optional regular expression (RE2). Return early as soon as a job writes something matching it. Use it to wait once for the line you actually care about — a completion marker, or an error — instead of polling to check."}}}`)
}

func (waitJob) ReadOnly() bool { return true }

// Sequential is true even though ReadOnly is: a wait is for jobs started
// earlier in this reply, and one that runs beside them waits for nothing.
func (waitJob) Sequential(context.Context, json.RawMessage) bool { return true }

func (waitJob) ProviderVisible(ctx context.Context) bool {
	_, ok := jobs.FromContext(ctx)
	return ok
}

func (waitJob) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		JobIDs         []string `json:"job_ids"`
		TimeoutSeconds int      `json:"timeout_seconds"`
		UntilMatches   string   `json:"until_output_matches"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
	}
	jm, ok := jobs.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("background jobs are not available in this context")
	}
	opts := jobs.WaitOptions{Timeout: time.Duration(p.TimeoutSeconds) * time.Second}
	if pattern := strings.TrimSpace(p.UntilMatches); pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return "", fmt.Errorf("until_output_matches is not a valid regular expression: %w", err)
		}
		opts.Match = re
	}
	results, outcome := jm.WaitForSession(ctx, jobs.SessionFromContext(ctx), p.JobIDs, opts)
	if len(results) == 0 {
		return "No background jobs to wait for.", nil
	}
	var b strings.Builder
	if outcome == jobs.WaitMatched {
		b.WriteString("Returned early: a job wrote something matching until_output_matches.\n\n")
	}
	for i, r := range results {
		if r.Status != jobs.Running {
			collectBackgroundEvidence(ctx, jm, r.ID)
		}
		if i > 0 {
			b.WriteString("\n\n")
		}
		label := r.ID
		if r.Label != "" {
			label = fmt.Sprintf("%s (%s)", r.ID, r.Label)
		}
		fmt.Fprintf(&b, "[%s] %s%s", label, r.Status, r.Status.Cause())
		b.WriteString(stillRunning(r.Progress))
		if strings.TrimSpace(r.Output) != "" {
			b.WriteString("\n" + r.Output)
		}
	}
	return b.String(), nil
}

// stillRunning states what happened while the caller waited. No thresholds and
// no verdict: a task making slow progress and one that has stopped differ by
// whether anything arrived, and the caller is the one who knows which of those
// its own task should look like.
func stillRunning(p *jobs.Progress) string {
	if p == nil {
		return ""
	}
	produced := "nothing on its own output so far"
	if p.Produced > 0 {
		produced = fmt.Sprintf("%s produced", byteCount(p.Produced))
	}
	arrived := "nothing arrived while waiting"
	if p.Delta > 0 {
		arrived = fmt.Sprintf("%s arrived while waiting", byteCount(p.Delta))
	}
	return fmt.Sprintf(" — running %s, %s, %s", p.Running.Round(time.Second), produced, arrived)
}

func byteCount(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func collectBackgroundEvidence(ctx context.Context, jm *jobs.Manager, jobID string) {
	// A Plan turn should not consume a finished background writer's mutation
	// receipts before the workflow reaches execution. Writers may still run after
	// Permissions approval; leave their evidence on the job so the first
	// post-approval collection can merge and audit it.
	if planmode.Active(ctx) {
		return
	}
	ledger, ok := evidence.FromContext(ctx)
	if !ok || ledger == nil || jm == nil {
		return
	}
	session := jobs.SessionFromContext(ctx)
	// A non-Running status from bash_output/wait does not guarantee the job's
	// run goroutine has actually flushed PublishEvidence and closed done: kill_shell
	// flips status to Killed synchronously, well before its cancelled goroutine
	// unwinds. Check readiness before noting the lease — noting it on an empty,
	// not-yet-ready read would dedupe away every later retry in this turn (the
	// lease is idempotent per turn) while the job later publishes real mutation
	// evidence nobody ever merges or reviews.
	summary, ready := jm.TryLeaseEvidenceForSession(session, jobID)
	if !ready {
		return
	}
	// Note the lease before merging so a second wait/bash_output in the same
	// turn does not double-count. The merge is provisional: the lease does not
	// consume, so if this turn fails the agent never commits and the next turn
	// re-collects. The agent commits leased jobs only after the turn passes its
	// delivery gates.
	if !ledger.NoteBackgroundLease(session, jobID) {
		return
	}
	ledger.MergeChild(summary)
}
