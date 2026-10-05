package builtin

import (
	"context"
	"fmt"
	"time"
)

// FileOverlay lets a host transport (an ACP client editor, say) serve file
// content instead of the local disk, so tools see unsaved editor buffers. A
// nil overlay or an ok=false answer falls back to direct disk I/O; the overlay
// is consulted only after the tool's own path resolution and confinement
// checks, so it never widens what a tool may touch.
type FileOverlay interface {
	// ReadTextFile returns the current text of path as the host sees it
	// (including unsaved changes). ok=false means the host cannot serve this
	// path and the caller should read the local disk instead.
	ReadTextFile(ctx context.Context, path string) (content string, ok bool)
	// WriteTextFile asks the host to write content to path (updating any open
	// buffer as well as the file). ok=false means the host cannot handle the
	// write and the caller should write the local disk instead; err is only
	// meaningful when ok is true.
	WriteTextFile(ctx context.Context, path, content string) (ok bool, err error)
}

// TerminalRunner lets a host transport run a foreground shell command in a
// host-owned terminal (the ACP terminal/* methods, say) so the user watches it
// live. ok=false means the host cannot run it and the caller should execute
// locally; err is only meaningful when ok is true. Runners are only consulted
// when the local OS sandbox is not enforcing — a host terminal cannot honor
// the local confinement configuration.
//
// envOverrides, when non-nil, is a small map of environment variables the host
// terminal should set for the command (typically TMPDIR/TMP/TEMP for the
// session-private temporary directory). Callers must not pass a full host
// environment dump — only the overrides Reasonix owns.
//
// Prefer typed outcomes when possible:
//   - TerminalExitError for a non-zero process exit (Code is the real exit code)
//   - TerminalTimeoutError when the host-enforced timeout fired
//   - context.Canceled / context.DeadlineExceeded for parent cancellation
//
// Plain fmt.Errorf strings remain accepted for older host runners.
type TerminalRunner interface {
	RunCommand(ctx context.Context, command, cwd string, timeout time.Duration, envOverrides map[string]string) (output string, ok bool, err error)
}

// TerminalExitError is returned by a host TerminalRunner when the command ran
// and produced a non-zero exit status. bash.ExecuteDetailed preserves Code on
// ShellExecution.ExitCode.
type TerminalExitError struct {
	Code int
}

func (e TerminalExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

// TerminalTimeoutError is returned when the host terminal killed the command
// after the tool-local timeout. bash.ExecuteDetailed maps this to
// state=timed_out / failurePhase=timeout.
type TerminalTimeoutError struct {
	Timeout time.Duration
}

func (e TerminalTimeoutError) Error() string {
	if e.Timeout > 0 {
		return fmt.Sprintf("command timed out after %s (terminal killed)", e.Timeout)
	}
	return "command timed out (terminal killed)"
}
