package isolation

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Run is one isolated run: the task as a new agent would receive it, and the
// worktree it runs in.
type Run struct {
	WorkspaceRoot string
	Prompt        string
	Model         string
}

// Outcome is what the run ended with. Unverified carries the readiness reason
// when the run stopped without proving its work.
type Outcome struct {
	Answer     string
	Unverified string
	Evidence   *CandidateEvidence
}

// Runner carries a Run to its end in a kernel rooted at Run.WorkspaceRoot.
type Runner func(ctx context.Context, run Run) (Outcome, error)

// Execute runs prompt in a fresh worktree of workspaceRoot and reports what it
// changed. The changes stay in the worktree, under the returned report's id,
// until apply_isolated or discard_isolated settles them. A run that failed
// after changing files keeps them, and the error names the id.
func (s *Store) Execute(ctx context.Context, runner Runner, workspaceRoot, prompt, model string) (string, error) {
	if runner == nil {
		return "", errUnavailable
	}
	e, err := s.Begin(ctx, workspaceRoot)
	if err != nil {
		return "", err
	}
	out, runErr := invokeRunner(ctx, runner, Run{WorkspaceRoot: e.Cand.WorkspaceRoot, Prompt: prompt, Model: model})
	return s.Complete(ctx, e, out, runErr)
}

func (s *Store) Complete(ctx context.Context, e *Entry, out Outcome, runErr error) (string, error) {
	if out.Evidence != nil {
		if e.Evidence != nil {
			out.Evidence.Receipts = append(e.Evidence.Receipts, out.Evidence.Receipts...)
		}
		e.Evidence = out.Evidence
	}
	state := Pending
	if runErr != nil {
		state = Failed
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			state = Cancelled
		}
	}
	kept, finishErr := s.finishState(context.WithoutCancel(ctx), e, state)
	if finishErr != nil {
		return "", fmt.Errorf("isolated result %s retained for recovery: %w", e.ID, errors.Join(runErr, finishErr))
	}
	if runErr != nil {
		if kept {
			status := "failed"
			if state == Cancelled {
				status = "was cancelled"
			}
			return "", fmt.Errorf("isolated task %s; the changes it made are kept as %s (apply_isolated or discard_isolated): %w", status, e.ID, runErr)
		}
		return "", runErr
	}
	return report(e, kept, out), nil
}

func invokeRunner(ctx context.Context, runner Runner, run Run) (out Outcome, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("isolated runner panicked: %v", recovered)
		}
	}()
	return runner(ctx, run)
}

func report(e *Entry, kept bool, out Outcome) string {
	var b strings.Builder
	if kept && len(e.Changes) == 0 {
		fmt.Fprintf(&b, "Isolated task %s has no publishable Git changes. Its environment is retained for continuation; copied dependencies may have changed.\n", e.ID)
	} else if kept {
		fmt.Fprintf(&b, "Isolated task %s finished in its own worktree. Its changes are NOT in the workspace yet:\n%s\n", e.ID, statTable(e))
		fmt.Fprintf(&b, "Review them, then call apply_isolated {\"id\":%q} to write them in, or discard_isolated to drop them.\n", e.ID)
	} else {
		b.WriteString("Isolated task finished without changing any file.\n")
	}
	if out.Unverified != "" {
		fmt.Fprintf(&b, "It stopped without proving its work: %s\n", out.Unverified)
	}
	if answer := strings.TrimSpace(out.Answer); answer != "" {
		b.WriteString("\nIts final answer:\n")
		b.WriteString(answer)
	}
	return strings.TrimRight(b.String(), "\n")
}
