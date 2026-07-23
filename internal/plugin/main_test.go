package plugin

import (
	"os"
	"testing"

	"reasonix/internal/testenv"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	// MCP helper subprocesses run this same test binary under the sandbox and
	// inherit the already-isolated parent environment. They must not try to
	// allocate a second user home outside their allowed roots.
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" || os.Getenv("GO_WANT_HELPER_STDERR_EXIT") == "1" {
		os.Exit(m.Run())
	}
	cleanupUserState, err := testenv.IsolateUserState()
	if err != nil {
		panic(err)
	}
	goleak.VerifyTestMain(m, goleak.Cleanup(func(exitCode int) {
		cleanupUserState()
		os.Exit(exitCode)
	}))
}
