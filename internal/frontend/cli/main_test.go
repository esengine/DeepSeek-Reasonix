// main_test.go — what the whole cli test binary needs before any test runs.
package cli

import (
	"os"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/base/testenv"
)

func TestMain(m *testing.M) {
	if code, ok := scheduleTestHelperProcess(); ok {
		os.Exit(code)
	}
	cleanupUserState, err := testenv.IsolateUserState()
	if err != nil {
		panic(err)
	}

	// One language for the whole binary: cli.Run installs the host locale
	// globally and the tests through it do not put it back, so a test asserting
	// English fails only when the package runs together.
	os.Unsetenv("REASONIX_LANG")
	os.Unsetenv("LC_ALL")
	os.Unsetenv("LC_MESSAGES")
	os.Setenv("LANG", "en_US.UTF-8")
	i18n.DetectLanguage("en")

	code := m.Run()
	cleanupUserState()
	os.Exit(code)
}
