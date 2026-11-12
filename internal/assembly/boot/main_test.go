package boot

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// hostGoCache is the go build cache the developer's own toolchain uses, read
// before the isolated home would move the default under it. A build started
// from a test then reuses compiled dependencies instead of recompiling them
// into an empty cache, which dominates a cold Windows build.
var hostGoCache = func() string {
	if v := os.Getenv("GOCACHE"); v != "" {
		return v
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "go-build")
	}
	return ""
}()

// goBuildEnv is the environment for a `go build` of a checked-in example.
func goBuildEnv() []string {
	env := append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	if hostGoCache != "" {
		env = append(env, "GOCACHE="+hostGoCache)
	}
	return env
}

func TestMain(m *testing.M) {
	if os.Getenv(fakeComputerHelperEnv) != "" {
		runFakeComputerHelper(os.Stdin, os.Stdout)
		return
	}
	// A stated home has no process variable to carry this, and the OS keychain
	// is not isolated by the disposable home.
	_ = os.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	feedbackRetired = false
	testenv.RunWithIsolatedUserState(m)
}
