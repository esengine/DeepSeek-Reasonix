package winsandbox

import (
	"errors"
	"os"
	"time"
)

// ErrUnsupported is returned when the package is used on a non-Windows host or
// when required native Windows sandbox APIs are unavailable.
var ErrUnsupported = errors.New("windows sandbox is unavailable")

// Spec describes one native Windows sandbox launch.
//
// Read-only launches use AppContainer. Writable launches use a low-integrity
// token plus temporary ACL grants for WritableRoots and the per-command temp
// root. ForbidReadRoots are denied with temporary deny ACEs. Network=false is
// supported for read-only AppContainer launches; writable launches fail closed
// because low-integrity tokens do not provide reliable per-process network
// blocking without elevated firewall or WFP setup.
type Spec struct {
	WritableRoots   []string
	ForbidReadRoots []string
	Network         bool
	Writable        bool
	TempPrefix      string
	// LockWait bounds how long this run may queue behind another sandboxed
	// command holding the same per-root lock before failing with a clear
	// error. Zero uses the short interactive default; callers whose run
	// nobody is blocked on (background jobs) pass a longer budget.
	// WINDOWS_SANDBOX_LOCK_MS overrides both.
	LockWait time.Duration
}

// RunOptions carries process IO and environment overrides.
type RunOptions struct {
	Stdin  *os.File
	Stdout *os.File
	Stderr *os.File
	Env    []string
	Dir    string
}

// Result is the completed sandboxed process result.
type Result struct {
	ExitCode int
}
