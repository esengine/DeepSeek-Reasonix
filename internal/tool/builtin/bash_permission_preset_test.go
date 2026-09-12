//go:build !windows

package builtin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/sandbox"
)

func TestWorkspaceWriteRunsPipeInlineScriptAndCommandSubstitution(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("native sandbox unavailable")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	work := t.TempDir()
	command := "printf 'ok\\n' | " + shellQuote(python) + " -c 'import sys; print(sys.stdin.read().strip().upper())' > result.txt; value=$(cat result.txt); printf '%s' \"$value\""
	ctx := sandbox.WithPermissionPreset(context.Background(), "workspace-write")
	out, err := (bash{workDir: work}).Execute(ctx, argsJSON(t, map[string]any{"command": command}))
	if err != nil {
		t.Fatalf("workspace command failed: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "OK") {
		t.Fatalf("workspace command output = %q, want OK", out)
	}
	body, err := os.ReadFile(filepath.Join(work, "result.txt"))
	if err != nil || strings.TrimSpace(string(body)) != "OK" {
		t.Fatalf("workspace output file = %q, %v", body, err)
	}
}

func TestReadOnlyPresetBlocksWorkspaceWrite(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("native sandbox unavailable")
	}
	work := t.TempDir()
	ctx := sandbox.WithPermissionPreset(context.Background(), "read-only")
	out, err := (bash{workDir: work}).Execute(ctx, argsJSON(t, map[string]any{"command": "printf blocked > denied.txt"}))
	if err == nil {
		t.Fatalf("read-only write unexpectedly succeeded: %q", out)
	}
	if _, statErr := os.Stat(filepath.Join(work, "denied.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("read-only command created denied.txt: %v", statErr)
	}
}
