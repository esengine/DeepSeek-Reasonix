package hook

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/sessiontemp"
)

func TestHookCannotRunWithoutRequiredIntegrity(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "unexpected")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []ExecutionMode{ExecutionExec, ExecutionShell, ExecutionLegacy} {
		t.Run(string(mode), func(t *testing.T) {
			spec := sandbox.Spec{Mode: "enforce", RequiredIntegrity: "unavailable_backend"}
			input := SpawnInput{Command: self, Args: []string{"-test.run=^TestConfinedHookMarkerHelper$"}, Env: map[string]string{"REASONIX_TEST_HOOK_MARKER": marker}, Mode: mode, Cwd: dir, Timeout: time.Second}
			if mode != ExecutionExec {
				input.Args = nil
				input.Command = "echo escaped > '" + strings.ReplaceAll(marker, "'", "'\\''") + "'"
				if runtime.GOOS == "windows" {
					input.Command = "echo escaped > \"" + marker + "\""
					input.Shell = "cmd"
				}
			}
			result := NewDefaultSpawner(RuntimeOptions{Sandbox: &spec})(t.Context(), input)
			if !errors.Is(result.SpawnErr, sandbox.ErrIncompleteIntegrity) {
				t.Fatal("hook launched without the required boundary")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("hook changed files: %v", err)
			}
		})
	}
	_, err = confineHookCommand(context.Background(), nil, RuntimeOptions{Sandbox: &sandbox.Spec{RequiredIntegrity: sandbox.CompleteFileIntegrity}})
	if !errors.Is(err, sandbox.ErrIncompleteIntegrity) {
		t.Fatalf("lost integrity identity: %v", err)
	}
}

func TestConfinedHookMarkerHelper(t *testing.T) {
	marker := os.Getenv("REASONIX_TEST_HOOK_MARKER")
	if marker == "" {
		return
	}
	if err := os.WriteFile(marker, []byte("escaped"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConfinedHookUsesPrivateTemporaryDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || !sandbox.Available() {
		t.Skip("requires native Unix sandbox")
	}
	candidate, source := t.TempDir(), t.TempDir()
	manager := sessiontemp.NewWithRoot(t.TempDir())
	manager.Retain()
	t.Cleanup(manager.Release)
	spec := sandbox.Spec{Mode: "enforce", RequiredIntegrity: sandbox.CompleteFileIntegrity, MinimalWrites: true, WriteRoots: []string{candidate}, ProtectedWriteRoots: []string{source}}
	result := NewDefaultSpawner(RuntimeOptions{Sandbox: &spec, SessionTemp: manager})(t.Context(), SpawnInput{
		Command: `printf private > "$TMPDIR/hook-private"`, Mode: ExecutionShell, Cwd: candidate, Timeout: 10 * time.Second,
	})
	if result.SpawnErr != nil || result.ExitCode != 0 {
		t.Fatalf("confined hook failed: %+v", result)
	}
	if body, err := os.ReadFile(filepath.Join(manager.Dir(), "hook-private")); err != nil || string(body) != "private" {
		t.Fatalf("hook did not use private temporary storage: %q %v", body, err)
	}
}
