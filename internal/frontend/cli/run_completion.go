package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"reasonix/internal/base/i18n"
	"reasonix/internal/runtime/agent"
)

// reportRunFailure states why a run ended without a result. Text output — which
// -p/--print also selects — has nowhere else to carry that: its sink prints the
// final response and nothing else, so a failed run exited 1 with an empty
// stderr and a half-finished answer. Structured formats encode the failure in
// their own payload and need this only when no such sink was built.
func reportRunFailure(w io.Writer, format runOutputFormat, structured bool, completion runCompletion, runErr error) {
	if runErr == nil {
		return
	}
	switch {
	case format != runOutputText:
		if !structured {
			fmt.Fprintln(w, "\n"+i18n.M.ErrorPrefix, runErr)
		}
	case completion.isError:
		fmt.Fprintln(w, "\n"+i18n.M.ErrorPrefix, runErr)
	default:
		// A paused run is a reportable outcome, not a failure, and reads better
		// without the error prefix.
		fmt.Fprintln(w, "\n"+runErr.Error())
	}
}

type runCompletion struct {
	outcome string
	subtype string
	// class is the benchmark-facing failure taxonomy. It names which guard or
	// transport ended the run and never affects the exit code or wire outcome.
	class    string
	isError  bool
	exitCode int
	// unverified: the model finished and the host's final-readiness judgement
	// stayed unmet. That is a verdict on the answer, not a failed run.
	unverified bool
}

func classifyRunCompletion(err error) runCompletion {
	if err == nil {
		return runCompletion{subtype: "success", class: "success"}
	}
	if runReadiness(err) != nil {
		return runCompletion{subtype: "success", class: runFailureClass(err), unverified: true}
	}
	return runCompletion{
		subtype:  "error_during_execution",
		class:    runFailureClass(err),
		isError:  true,
		exitCode: 1,
	}
}

func runFailureClass(err error) string {
	if class := agent.PauseClass(err); class != "" {
		return class
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	}
	return "error_during_execution"
}
