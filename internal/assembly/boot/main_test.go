package boot

import (
	"os"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeComputerHelperEnv) != "" {
		runFakeComputerHelper(os.Stdin, os.Stdout)
		return
	}
	// A stated home has no process variable to carry this, and the OS keychain
	// is not isolated by the disposable home.
	_ = os.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	testenv.RunWithIsolatedUserState(m)
}
