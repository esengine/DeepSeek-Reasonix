package boot

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/safety/sandbox"
)

func pinnedEffectShell() *sandbox.Shell {
	if runtime.GOOS == "windows" {
		shell := sandbox.ResolveShell("bash", "", io.Discard)
		return &shell
	}
	return &sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}
}

func initEffectRepository(t *testing.T, root string) {
	t.Helper()
	config := filepath.Join(t.TempDir(), "global.gitconfig")
	if err := os.WriteFile(config, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := gitcmd.Command(context.Background(), root, "init", "--template=")
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL="+config, "GIT_CONFIG_SYSTEM="+config)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, out)
	}
}

func isolateGitGlobalConfig(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "global.gitconfig")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", path)
}
