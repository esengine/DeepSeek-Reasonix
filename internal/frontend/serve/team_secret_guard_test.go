package serve

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/safety/sandbox"
)

// Whether the secrets file is out of agents' reach is what the sandbox setting
// and the platform make it: never on Windows, never with the sandbox off.
func TestSecretsAreGuardedOnlyWhereReadsAreConfined(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, "home"))
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(home, "state"))
	write := func(bash string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config.UserConfigPath(), []byte("[sandbox]\nbash = \""+bash+"\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("off")
	if secretsGuarded() {
		t.Fatal("guarded with the sandbox off")
	}
	write("enforce")
	want := runtime.GOOS != "windows" && sandbox.AvailableFor(sandbox.Spec{})
	if got := secretsGuarded(); got != want {
		t.Fatalf("guarded with the sandbox enforced = %v, want %v on %s", got, want, runtime.GOOS)
	}
}
