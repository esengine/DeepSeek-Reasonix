package bestof

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/runtime/isolation"
)

var ErrJudgePanic = errors.New("best-of judge panicked")

func invokeJudge(ctx context.Context, spec JudgeSpec, a args, results []result, eligible []int) (winner int, reason string, err error) {
	returned := false
	defer func() {
		value := recover()
		if !returned {
			if cause, ok := value.(error); ok {
				err = fmt.Errorf("%w: %w", ErrJudgePanic, cause)
			} else {
				err = fmt.Errorf("%w: %v", ErrJudgePanic, value)
			}
		}
	}()
	winner, reason, err = judge(ctx, spec, a, results, eligible)
	returned = true
	return
}

// Recovery belongs on each worker goroutine. Only a normal return can enter
// completion; unwinding an unknown runner is never a cleanup receipt.
func invokeCandidate(ctx context.Context, runner Runner, run Run) (out Outcome, err error) {
	returned := false
	defer func() {
		value := recover()
		if !returned {
			if cause, ok := value.(error); ok {
				err = fmt.Errorf("%w: %w", isolation.ErrRunnerPanic, cause)
			} else {
				err = fmt.Errorf("%w: %v", isolation.ErrRunnerPanic, value)
			}
		}
	}()
	out, err = runner(ctx, run)
	returned = true
	return out, err
}

func candidateIDs(candidates []*isolation.ManagedRun) string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID())
	}
	return strings.Join(ids, ", ")
}

func retainedReport(candidates []*isolation.ManagedRun) string {
	return "No candidate was automatically applied. Original Store result IDs: " + candidateIDs(candidates) +
		". Completed candidates can be recovered with the existing isolated-result controls when worktree isolation is enabled. Running or unknown executions stay retained and refuse settlement, including after restart; they require host reconciliation, not another best_of_n run."
}
