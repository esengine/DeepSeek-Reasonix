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
// Direct read-only launches use AppContainer. Shell and writer launches use a
// WRITE_RESTRICTED token whose restricting SIDs carry only the selected
// directory capabilities. ForbidReadRoots are denied with temporary deny ACEs.
// Network=false is supported for AppContainer launches; restricted-token
// launches fail closed because WRITE_RESTRICTED does not isolate network.
type Spec struct {
	ReadableRoots       []string
	WritableRoots       []string
	ForbidReadRoots     []string
	ProtectedWriteRoots []string
	Network             bool
	Writable            bool
	ReadOnly            bool
	TempDir             string
	TempPrefix          string
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
