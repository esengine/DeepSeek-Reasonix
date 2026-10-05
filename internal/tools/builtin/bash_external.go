package builtin

import (
	"errors"
	"fmt"
	"time"

	"reasonix/internal/contract/tool"
)

var errExternalReference = errors.New("external reference is not a shell path")

// refuseExternalRef stops a command naming a folder mounted read-only from
// outside the workspace. The token is not a path — the mapping lives in the
// read tools' resolver, and giving bash the real directory would be a writable
// way into a read-only grant — so the shell answers "No such file", which reads
// as proof the file is gone. Say what the token is instead.
func (b bash) refuseExternalRef(command string) error {
	hits := b.paths.TokensIn(command)
	if len(hits) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s is this session's read-only reference to a folder outside the workspace, "+
		"not a path on disk — bash cannot reach it. read_file, ls, glob and grep resolve these; use one of them", errExternalReference, hits[0])
}

// notRun ends a call that never reached the shell. The four preflight refusals
// answered with the same four assignments before returning, which is four
// places for "nothing ran" to drift apart.
func notRun(ex *tool.ShellExecution, start time.Time, err error) (tool.DetailedResult, error) {
	ex.State = tool.ShellStateNotRun
	ex.FailurePhase = tool.ShellPhasePreflight
	ex.MutationRisk = tool.ShellMutationNotStarted
	ex.DurationMs = time.Since(start).Milliseconds()
	return tool.DetailedResult{Execution: ex}, err
}
