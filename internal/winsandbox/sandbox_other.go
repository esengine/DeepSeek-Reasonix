//go:build !windows

package winsandbox

// Available reports whether the native Windows sandbox backend is available.
func Available() bool {
	return false
}

// Run executes argv in a native Windows sandbox.
func Run(_ Spec, _ []string, _ RunOptions) (Result, error) {
	return Result{}, ErrUnsupported
}
