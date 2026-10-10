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
	Repo        gitcmd.Repo // the workspace's identity, resolved when it opened
	ManagedRoot string
	Runner      Runner
	KnownModel  func(ref string) bool
	Judge       JudgeSpec
}

type bestOf struct{ spec Spec }

// New returns the best_of_n tool.
func init() { tool.RegisterConditional(bestOf{}) }

func New(spec Spec) tool.Tool { return bestOf{spec: spec} }

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
	snap, err := worktree.TakeSnapshot(ctx, b.spec.Repo)
	if err != nil {
		return "", fmt.Errorf("best_of_n needs a Git workspace: %w", err)
	}
	cands, err := b.createCandidates(ctx, snap, a.N)
	if err != nil {
		return "", err
	}
	results := b.runCandidates(ctx, snap, cands, a)
	return b.settle(ctx, snap, cands, results, a)
}

func (b bestOf) createCandidates(ctx context.Context, snap worktree.Snapshot, n int) ([]worktree.Candidate, error) {
	cands := make([]worktree.Candidate, 0, n)
	for range n {
		c, err := worktree.CreateCandidate(ctx, snap, b.spec.ManagedRoot)
		if err != nil {
			for _, made := range cands {
				_ = worktree.RemoveCandidate(context.WithoutCancel(ctx), snap, made)
			}
			return nil, err
		}
		cands = append(cands, c)
	}
	return cands, nil
}

// result is one candidate after its run: what it said, what it changed, or why
// it has neither.
type result struct {
	model string
	Outcome
	tree    string
	changes []worktree.Change
	patch   string
	err     error
}

func (r result) finished() bool { return r.err == nil }

func (b bestOf) runCandidates(ctx context.Context, snap worktree.Snapshot, cands []worktree.Candidate, a args) []result {
	out := make([]result, len(cands))
	var wg sync.WaitGroup
	for i, c := range cands {
		model := ""
		if len(a.Models) > 0 {
			model = strings.TrimSpace(a.Models[i%len(a.Models)])
		}
		out[i].model = model
		wg.Add(1)
		go func(i int, c worktree.Candidate) {
			defer wg.Done()
			r := &out[i]
			r.Outcome, r.err = b.spec.Runner(ctx, Run{Index: i + 1, WorkspaceRoot: c.WorkspaceRoot, Prompt: a.Prompt, Model: model})
			if r.err != nil {
				return
			}
			if r.tree, r.changes, r.err = worktree.CandidateTree(ctx, snap, c); r.err != nil {
				return
			}
			r.patch, r.err = worktree.Patch(ctx, snap, r.tree)
		}(i, c)
	}
	wg.Wait()
	return out
}

// settle judges, applies the winner, and removes every worktree it no longer
// needs. A winner that could not be applied keeps its worktree so its work is
// not lost; everything else is removed.
func (b bestOf) settle(ctx context.Context, snap worktree.Snapshot, cands []worktree.Candidate, results []result, a args) (string, error) {
	cleanup := context.WithoutCancel(ctx)
	keep := -1
	defer func() {
		for i, c := range cands {
			if i != keep {
				_ = worktree.RemoveCandidate(cleanup, snap, c)
			}
		}
	}()
	var eligible []int
	for i, r := range results {
		if r.finished() {
			eligible = append(eligible, i)
		}
	}
	if len(eligible) == 0 {
		return "", fmt.Errorf("%w: %s", ErrNoCandidateFinished, failureSummary(results))
	}
	winner, reason := eligible[0], "the only attempt that finished"
	if len(eligible) > 1 {
		w, why, err := judge(ctx, b.spec.Judge, a, results, eligible)
		if err != nil {
			return "", fmt.Errorf("best_of_n: the judge could not decide, so nothing was applied: %w", err)
		}
		winner, reason = w, why
	}
	applyErr := worktree.Apply(ctx, snap, results[winner].tree, results[winner].changes)
	if applyErr != nil {
		keep = winner
	}
	return report(results, winner, reason, applyErr, cands[winner].WorkspaceRoot), nil
}

// ApprovalScope says that approving best_of_n also starts attempts that run
// their own tool calls unattended.
func (bestOf) ApprovalScope() string { return tool.ApprovalScopeUnattendedAttempts }
