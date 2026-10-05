package builtin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/safety/sandbox"
)

func TestGrepSandboxUnavailableRootDoesNotSearchHome(t *testing.T) {
	if !sandbox.Available() {
		t.Skip("no usable bubblewrap sandbox")
	}
	rg, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("ripgrep not installed")
	}
	fixture, err := os.MkdirTemp("/tmp", "reasonix-grep-scope-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fixture) })
	home, root := filepath.Join(fixture, "home"), filepath.Join(fixture, "external")
	for _, dir := range []string{home, root} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	const needle = "HOME_SCOPE_SENTINEL"
	if err := os.WriteFile(filepath.Join(home, "private.txt"), []byte(needle+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	alias := filepath.Join(fixture, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	const token = "__reasonix_external_folder/scope/source"
	paths := NewPathResolver()
	paths.RegisterReadRoot(token, alias)
	for _, mode := range []string{"tmpfs", "session-temp"} {
		t.Run(mode, func(t *testing.T) {
			spec := sandbox.Spec{Mode: "enforce", WriteRoots: []string{home}, MinimalWrites: true}
			if mode == "session-temp" {
				spec.SessionTemp = t.TempDir()
			}
			g := grepTool{rg: rg, sb: spec, paths: paths}
			out := runTool(t, g, map[string]any{"pattern": needle, "path": home})
			if !strings.Contains(out, needle) {
				t.Fatalf("control search did not reach the explicitly requested home: %s", out)
			}
			result, err := g.Execute(t.Context(), argsJSON(t, map[string]any{"pattern": needle, "path": token}))
			if err == nil || result != "" {
				t.Fatalf("unmapped search root returned result=%q, err=%v", result, err)
			}
			if !strings.Contains(err.Error(), token) || strings.Contains(err.Error(), fixture) {
				t.Fatalf("error must use the external token without leaking local paths: %v", err)
			}
			rp := resolveReadablePath("", token, paths)
			for _, wide := range []bool{false, true} {
				out, _, wrapped, err := g.ripgrepPass(t.Context(), needle, rp.Path, "", true, rp, wide)
				if !wrapped || err == nil || len(out) != 0 {
					t.Fatalf("wide=%v: out=%q, wrapped=%v, err=%v", wide, out, wrapped, err)
				}
			}
		})
	}
}
