package builtin

import (
	"errors"
	"fmt"
	"time"

	"reasonix/internal/contract/tool"
)

// errCommandLineTooLong marks a launch the OS would refuse because the
// command line itself is over its length ceiling; nothing was started.
var errCommandLineTooLong = errors.New("command line too long")

const CodeCommandLineTooLong = "shell.command_line_too_long"

// checkCommandLine refuses argv that cannot be launched on this OS, so the
// model learns the host limit instead of a misleading "filename or extension
// is too long" from process creation.
func checkCommandLine(argv []string) error {
	units, limit := commandLineUnits(argv)
	if limit == 0 || units <= limit {
		return nil
	}
	return fmt.Errorf("%w: %w", errCommandLineTooLong, tool.Refusal{Code: CodeCommandLineTooLong, Message: fmt.Sprintf("the command is %d characters and Windows accepts at most %d, so it was not run. "+
		"Write long text with write_file or edit_file and have the command read that file, or split the work into smaller commands",
		units, limit)})
}

// refusedBeforeLaunch records a call the host stopped before any process started.
func refusedBeforeLaunch(ex *tool.ShellExecution, start time.Time, phase string, err error) (tool.DetailedResult, error) {
	if errors.Is(err, errCommandLineTooLong) || errors.Is(err, errExternalReference) {
		phase = tool.ShellPhasePreflight
	}
	ex.State = tool.ShellStateNotRun
	ex.FailurePhase = phase
	ex.MutationRisk = tool.ShellMutationNotStarted
	ex.DurationMs = time.Since(start).Milliseconds()
	return tool.DetailedResult{Execution: ex}, err
}
