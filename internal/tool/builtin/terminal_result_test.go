package builtin

import (
	"context"
	"errors"
	"testing"
	"time"

	"reasonix/internal/tool"
)

func TestApplyTerminalResultTypedOutcomes(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ex := &tool.ShellExecution{Kind: "shell"}
		applyTerminalResult(ex, nil)
		if ex.State != tool.ShellStateCompleted || ex.ExitCode == nil || *ex.ExitCode != 0 {
			t.Fatalf("%+v", ex)
		}
	})
	t.Run("exit code", func(t *testing.T) {
		ex := &tool.ShellExecution{Kind: "shell"}
		applyTerminalResult(ex, TerminalExitError{Code: 7})
		if ex.State != tool.ShellStateFailed || ex.FailurePhase != tool.ShellPhaseExecution {
			t.Fatalf("%+v", ex)
		}
		if ex.ExitCode == nil || *ex.ExitCode != 7 {
			t.Fatalf("exitCode=%v", ex.ExitCode)
		}
		if ex.MutationRisk != tool.ShellMutationMayBePartial {
			t.Fatalf("mutationRisk=%q", ex.MutationRisk)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		ex := &tool.ShellExecution{Kind: "shell"}
		applyTerminalResult(ex, TerminalTimeoutError{Timeout: 2 * time.Second})
		if ex.State != tool.ShellStateTimedOut || ex.FailurePhase != tool.ShellPhaseTimeout {
			t.Fatalf("%+v", ex)
		}
		if ex.MutationRisk != tool.ShellMutationMayBePartial {
			t.Fatalf("mutationRisk=%q", ex.MutationRisk)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		ex := &tool.ShellExecution{Kind: "shell"}
		applyTerminalResult(ex, context.Canceled)
		if ex.State != tool.ShellStateCancelled || ex.FailurePhase != tool.ShellPhaseCancellation {
			t.Fatalf("%+v", ex)
		}
	})
	t.Run("legacy plain error", func(t *testing.T) {
		ex := &tool.ShellExecution{Kind: "shell"}
		applyTerminalResult(ex, errors.New("exit status 3"))
		if ex.State != tool.ShellStateFailed || ex.ExitCode != nil {
			// Plain errors cannot recover the code; still mark partial risk.
			t.Fatalf("legacy outcome = %+v", ex)
		}
		if ex.MutationRisk != tool.ShellMutationMayBePartial {
			t.Fatalf("mutationRisk=%q", ex.MutationRisk)
		}
	})
}
