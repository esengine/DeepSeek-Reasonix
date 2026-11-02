// Package bestof is the best_of_n tool: one task run by several unattended
// candidates, each in its own worktree cut from the live workspace, then one
// judge picks the result that lands. The candidates never touch the user's
// files; only the winner is written back, and only onto the state it started
// from. The host builds each candidate's kernel through Runner, so this package
// never assembles one itself.
package bestof

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/runtime/isolation"
)

// Name is the tool name the model calls.
const Name = "best_of_n"

const (
	minCandidates     = 2
	maxCandidates     = 4
	defaultCandidates = 3
)

// ErrNoCandidateFinished is a run where every candidate failed, so there was
// nothing to judge and nothing was applied.
var ErrNoCandidateFinished = errors.New("best_of_n: no candidate finished")

// Run is one candidate's assignment.
type Run struct {
	Index         int
	WorkspaceRoot string
	Prompt        string
	Model         string // empty runs the session's own model
}

// Outcome is how an attempt ended. Unverified is the host's reason when the
// attempt stopped short of its own readiness checks; its work is still judged.
// Host is the last completion summary the attempt's kernel emitted; nil when
// the turn changed nothing and flagged nothing.
type Outcome struct {
	Answer     string
	Unverified string
	Host       *event.CompletionSummaryInfo
}

// Runner builds an unattended kernel at Run.WorkspaceRoot, runs the prompt to
// the end, and reports how it ended. An error means there is nothing to judge.
type Runner func(ctx context.Context, run Run) (Outcome, error)

// Spec is what the host supplies once at boot.
type Spec struct {
	Store       *isolation.Store // shared with the session's result tools
	Repo        gitcmd.Repo      // the workspace's identity, resolved when it opened
	ManagedRoot string
	Runner      Runner
	KnownModel  func(ref string) bool
	Judge       JudgeSpec
}

type bestOf struct{ spec Spec }

// New returns the best_of_n tool.
func init() { tool.RegisterConditional(bestOf{}) }

func New(spec Spec) tool.Tool {
	if spec.Store == nil && spec.ManagedRoot != "" {
		spec.Store = isolation.NewStore(spec.ManagedRoot, spec.Repo.Dir, spec.Repo)
	}
	return bestOf{spec: spec}
}

func (bestOf) Name() string { return Name }

func (bestOf) Description() string {
	return "Run one well-specified coding task 2–4 times in parallel, each attempt by an unattended agent in its own git worktree copied from the workspace as it is now (uncommitted and untracked files included), then have a judge compare the attempts and write only the best one back into the workspace. Use it for a change where approaches can reasonably differ and one attempt may go wrong — a tricky fix, a refactor with several designs — not for small edits or questions. The prompt must stand alone: the attempts do not see this conversation. Each attempt costs a full agent run. If the workspace changes while the attempts run, nothing is written back and the winning attempt is kept for inspection."
}

func (bestOf) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"prompt":{"type":"string","description":"The complete task, as you would give it to a new agent that has not seen this conversation: goal, relevant files, constraints, how to verify."},` +
		`"n":{"type":"integer","minimum":2,"maximum":4,"description":"How many attempts. Default 3."},` +
		`"models":{"type":"array","items":{"type":"string"},"maxItems":4,"description":"Optional configured model per attempt, reused in order when shorter than n. Omitted: every attempt uses the session's model."},` +
		`"criteria":{"type":"string","description":"Optional: what the judge should weigh beyond correctness, such as the smallest diff or no new dependencies."}` +
		`},"required":["prompt"],"additionalProperties":false}`)
}

func (bestOf) ReadOnly() bool { return false }

type args struct {
	Prompt   string   `json:"prompt"`
	N        int      `json:"n"`
	Models   []string `json:"models"`
	Criteria string   `json:"criteria"`
}

func (b bestOf) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var a args
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	a.Prompt = strings.TrimSpace(a.Prompt)
	if a.Prompt == "" {
		return "", errors.New("invalid args: prompt is required")
	}
	if a.N == 0 {
		a.N = defaultCandidates
	}
	if a.N < minCandidates || a.N > maxCandidates {
		return "", fmt.Errorf("invalid args: n must be between %d and %d", minCandidates, maxCandidates)
	}
	for _, m := range a.Models {
		if b.spec.KnownModel != nil && !b.spec.KnownModel(strings.TrimSpace(m)) {
			return "", fmt.Errorf("invalid args: model %q is not configured", m)
		}
	}
	prepared, err := b.spec.Store.Prepare(ctx)
	if err != nil {
		return "", fmt.Errorf("best_of_n needs a Git workspace: %w", err)
	}
	cands, err := b.createCandidates(ctx, prepared, a.N)
	if err != nil {
		return "", err
	}
	results := b.runCandidates(ctx, cands, a)
	return b.settle(ctx, cands, results, a)
}

func (b bestOf) createCandidates(ctx context.Context, prepared *isolation.Preparation, n int) ([]*isolation.ManagedRun, error) {
	cands := make([]*isolation.ManagedRun, 0, n)
	for range n {
		c, err := prepared.BeginManaged(ctx)
		if c != nil {
			cands = append(cands, c)
		}
		if err != nil {
			for _, made := range cands {
				_, cleanupErr := b.spec.Store.Discard(context.WithoutCancel(ctx), made.ID())
				err = errors.Join(err, cleanupErr)
			}
			return nil, fmt.Errorf("candidate preparation failed; original results %s: %w", candidateIDs(cands), err)
		}
	}
	return cands, nil
}

// result is one candidate after its run: what it said, what it changed, or why
// it has neither.
type result struct {
	id    string
	model string
	Outcome
	tree    string
	changes []worktree.Change
	patch   string
	err     error
}

func (r result) finished() bool { return r.err == nil }

func (b bestOf) runCandidates(ctx context.Context, cands []*isolation.ManagedRun, a args) []result {
	out := make([]result, len(cands))
	var wg sync.WaitGroup
	for i, c := range cands {
		model := ""
		if len(a.Models) > 0 {
			model = strings.TrimSpace(a.Models[i%len(a.Models)])
		}
		out[i].model = model
		out[i].id = c.ID()
		wg.Add(1)
		go func(i int, c *isolation.ManagedRun) {
			defer wg.Done()
			r := &out[i]
			if r.err = c.Start(ctx); r.err != nil {
				return
			}
			r.Outcome, r.err = invokeCandidate(ctx, b.spec.Runner, Run{Index: i + 1, WorkspaceRoot: c.WorkspaceRoot(), Prompt: a.Prompt, Model: model})
			if r.err != nil {
				r.err = errors.Join(r.err, c.RetainUnknown(context.WithoutCancel(ctx)))
				return
			}
			if r.err = c.Complete(context.WithoutCancel(ctx)); r.err != nil {
				return
			}
			var entry *isolation.Entry
			entry, r.patch, r.err = b.spec.Store.Preview(ctx, c.ID())
			if r.err == nil {
				r.tree, r.changes = entry.Tree, entry.Changes
			}
		}(i, c)
	}
	wg.Wait()
	return out
}

// settle judges, applies the winner, and removes every worktree it no longer
// needs. A winner that could not be applied keeps its worktree so its work is
// not lost; everything else is removed.
func (b bestOf) settle(ctx context.Context, cands []*isolation.ManagedRun, results []result, a args) (string, error) {
	cleanup := context.WithoutCancel(ctx)
	var eligible []int
	var failures []error
	for i, r := range results {
		if r.finished() {
			eligible = append(eligible, i)
		} else {
			failures = append(failures, fmt.Errorf("attempt %d (%s): %w", i+1, r.id, r.err))
		}
	}
	if len(eligible) == 0 {
		return retainedReport(cands), errors.Join(append(failures, ErrNoCandidateFinished)...)
	}
	if len(failures) != 0 {
		return retainedReport(cands), errors.Join(failures...)
	}
	winner, reason := eligible[0], "the only attempt that finished"
	if len(eligible) > 1 {
		w, why, err := invokeJudge(ctx, b.spec.Judge, a, results, eligible)
		if err != nil {
			return retainedReport(cands), fmt.Errorf("best_of_n: the judge could not decide, so nothing was applied: %w", err)
		}
		winner, reason = w, why
	}
	_, applyErr := b.spec.Store.ApplyPrepared(ctx, cands[winner].ID())
	var cleanupErr error
	for i, c := range cands {
		if i == winner {
			continue
		}
		_, err := b.spec.Store.Discard(cleanup, c.ID())
		cleanupErr = errors.Join(cleanupErr, err)
	}
	out := report(results, winner, reason, applyErr, cands[winner].ID())
	if cleanupErr != nil {
		out += "\nCleanup needs retry for original Store result IDs: " + candidateIDs(cands)
	}
	if applyErr != nil && !errors.Is(applyErr, worktree.ErrWorkspaceMoved) {
		return out, errors.Join(applyErr, cleanupErr)
	}
	return out, cleanupErr
}

// ApprovalScope says that approving best_of_n also starts attempts that run
// their own tool calls unattended.
func (bestOf) ApprovalScope() string { return tool.ApprovalScopeUnattendedAttempts }
