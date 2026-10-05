package boot

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/hook"
	"reasonix/internal/safety/sandbox"
)

// TestUserHookRunnerIgnoresCheckoutShell holds the review hook runner's
// interpreter to the user's config: resolving a shell probes its path, so a
// checkout's [tools.shell] must never reach the resolver or the runtime.
func TestUserHookRunnerIgnoresCheckoutShell(t *testing.T) {
	isolateConfigHome(t)
	checkout := t.TempDir()
	// An installed-looking path: one where sandboxed commands write is refused
	// before it could reach the merged config this test starts from.
	evil := filepath.Join(filepath.VolumeName(os.TempDir())+string(filepath.Separator), "opt", "reasonix-test", "evil-bash")
	project := "[tools.shell]\nprefer = \"bash\"\npath = " + tomlString(evil) + "\n"
	if err := os.WriteFile(filepath.Join(checkout, "reasonix.toml"), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}

	type call struct{ prefer, path string }
	run := func(t *testing.T) call {
		t.Helper()
		cfg, err := config.LoadForRoot(checkout)
		if err != nil {
			t.Fatal(err)
		}
		// Approved, so the session config does carry the checkout's shell.
		if err := config.NewProjectProgramStore(cfg.Roots().Home()).Approve(checkout, cfg.PendingProjectPrograms()...); err != nil {
			t.Fatal(err)
		}
		if cfg, err = config.LoadForRoot(checkout); err != nil {
			t.Fatal(err)
		}
		if cfg.Tools.Shell.Path != evil {
			t.Fatalf("session config Tools.Shell.Path = %q, want the checkout's %q (the fixture must reach the merged config)", cfg.Tools.Shell.Path, evil)
		}
		var got []call
		newUserHookRunner(cfg, checkout, io.Discard, func(prefer, path string, _ io.Writer) sandbox.Shell {
			got = append(got, call{prefer, path})
			return sandbox.Shell{Kind: sandbox.ShellBash, Path: path}
		}, hook.NewDefaultSpawner)
		if len(got) != 1 {
			t.Fatalf("resolver calls = %v, want exactly one", got)
		}
		if got[0].path == evil {
			t.Fatalf("hook shell resolved from the checkout: %+v", got[0])
		}
		return got[0]
	}

	t.Run("no user shell falls back to auto", func(t *testing.T) {
		if got := run(t); got != (call{}) {
			t.Fatalf("resolver got %+v, want auto-detection", got)
		}
	})

	t.Run("user shell is honoured", func(t *testing.T) {
		userShell := filepath.Join(t.TempDir(), "user-bash")
		userConfig := config.UserConfigPath()
		if err := os.MkdirAll(filepath.Dir(userConfig), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "[tools.shell]\nprefer = \"bash\"\npath = " + tomlString(userShell) + "\n"
		if err := os.WriteFile(userConfig, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := run(t); got != (call{"bash", userShell}) {
			t.Fatalf("resolver got %+v, want the user's bash %q", got, userShell)
		}
	})
}

// TestUserHookRunnerDisablesCwdExeSearch holds the review hook process to
// PATH lookup: it starts in the checkout, where cmd.exe would otherwise find a
// python.exe the branch ships before the user's own interpreter.
func TestUserHookRunnerDisablesCwdExeSearch(t *testing.T) {
	isolateConfigHome(t)
	checkout := t.TempDir()
	if err := hook.Save(hook.ScopeGlobal, "", hook.Settings{Hooks: map[hook.Event][]hook.HookConfig{
		hook.PreToolUse: {{Match: "read_file", Command: `python C:\hooks\guard.py`}},
	}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadForRoot(checkout)
	if err != nil {
		t.Fatal(err)
	}
	var got []hook.SpawnInput
	runner := newUserHookRunner(cfg, checkout, io.Discard, func(string, string, io.Writer) sandbox.Shell {
		return sandbox.Shell{}
	}, func(hook.RuntimeOptions) hook.Spawner {
		return func(_ context.Context, in hook.SpawnInput) hook.SpawnResult {
			got = append(got, in)
			return hook.SpawnResult{}
		}
	})
	if block, msg := runner.PreToolUse(context.Background(), "read_file", json.RawMessage(`{"path":"secret.txt"}`)); block {
		t.Fatalf("recorded hook blocked: %s", msg)
	}
	if len(got) != 1 {
		t.Fatalf("spawns = %d, want the one global hook", len(got))
	}
	if got[0].Cwd != checkout {
		t.Fatalf("hook cwd = %q, want the checkout %q so the hook can inspect it", got[0].Cwd, checkout)
	}
	if v := got[0].Env["NoDefaultCurrentDirectoryInExePath"]; v != "1" {
		t.Fatalf("hook env NoDefaultCurrentDirectoryInExePath = %q, want \"1\" (env %v)", v, got[0].Env)
	}
}

func tomlString(s string) string {
	return "'" + s + "'"
}
