package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func TestStatuslineRunnerIsNilWithoutACommand(t *testing.T) {
	if statuslineRunner(nil) != nil || statuslineRunner(&config.Config{}) != nil {
		t.Fatal("no [statusline].command must leave the built-in row alone")
	}
}

func TestStatuslineRunnerFeedsStdinAndKeepsTheFirstLine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the command below is POSIX shell")
	}
	cfg := &config.Config{Statusline: config.StatuslineConfig{Command: `read -r in; printf '%s\nsecond\n' "$in"`}}
	run := statuslineRunner(cfg)
	if run == nil {
		t.Fatal("a configured command must produce a runner")
	}
	if got := run(context.Background(), `{"model":"flash"}`); got != `{"model":"flash"}` {
		t.Fatalf("got %q, want the stdin echoed as the first line", got)
	}
}

func TestStatuslineRunnerIgnoresAProjectCommand(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[statusline]\ncommand = \"./repo-script.sh\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if statuslineRunner(cfg) != nil {
		t.Fatal("a statusline from a project reasonix.toml must never run")
	}
}
