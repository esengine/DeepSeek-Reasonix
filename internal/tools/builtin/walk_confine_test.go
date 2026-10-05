package builtin

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/secrets"
	"reasonix/internal/base/testenv"
)

// A walk's shortcut must never answer differently from confineRead: every
// entry of a tree holding a forbidden directory, sensitive names, and links
// into and out of the forbidden place gets the same verdict both ways.
func TestWalkConfineAgreesWithConfineRead(t *testing.T) {
	secrets.SetProtectSensitiveFiles(true)
	t.Cleanup(func() { secrets.SetProtectSensitiveFiles(false) })
	root := testenv.TempDir(t)
	outside := testenv.TempDir(t)
	write := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "src", "a.go"))
	write(filepath.Join(root, "src", "deep", "b.go"))
	write(filepath.Join(root, "src", ".env"))
	write(filepath.Join(root, "certs", "server.PEM"))
	write(filepath.Join(root, "secret", "plans.txt"))
	write(filepath.Join(outside, "loot.txt"))
	links := 0
	link := func(target, at string) {
		if runtime.GOOS == "windows" {
			if info, err := os.Stat(target); err == nil && info.IsDir() {
				if exec.Command("cmd", "/c", "mklink", "/J", at, target).Run() == nil {
					links++
				}
				return
			}
		}
		if os.Symlink(target, at) == nil {
			links++
		}
	}
	link(filepath.Join(root, "secret"), filepath.Join(root, "src", "into-secret"))
	link(filepath.Join(root, "secret", "plans.txt"), filepath.Join(root, "src", "plans-link.txt"))
	link(outside, filepath.Join(root, "src", "outside"))
	forbid := realRoots([]string{filepath.Join(root, "secret"), outside})

	// The relative spelling is what a tool is handed as often as not.
	walkRoots := []string{root, filepath.Join(root, "src")}
	if rel, err := filepath.Rel(mustGetwd(t), root); err == nil && !strings.HasPrefix(rel, "..") {
		walkRoots = append(walkRoots, rel)
	}
	blockedSeen := 0
	for _, walkRoot := range walkRoots {
		w := newWalkConfine(forbid, nil, walkRoot)
		_ = filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			want := confineRead(forbid, path)
			if got := w.blocked(path, d); got != want {
				t.Errorf("%s (type %v): walk says blocked=%v, confineRead says %v", path, d.Type(), got, want)
			}
			if want {
				blockedSeen++
			}
			return nil
		})
	}
	if blockedSeen < 3 {
		t.Fatalf("only %d blocked entries seen: the tree does not exercise the deny side", blockedSeen)
	}
	t.Logf("links made: %d", links)
}

// glob over the tree above answers without the forbidden file, the sensitive
// names, or anything reached through a link into the forbidden directory.
func TestGlobWalkKeepsConfinement(t *testing.T) {
	root := testenv.TempDir(t)
	for _, p := range []string{"keep/a.txt", "secret/b.txt"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if os.Symlink(filepath.Join(root, "secret", "b.txt"), filepath.Join(root, "keep", "b-link.txt")) != nil {
		t.Log("file symlinks unavailable; the link case is not exercised")
	}
	g := globTool{workDir: root, forbidRoots: realRoots([]string{filepath.Join(root, "secret")})}
	out, err := g.Execute(context.Background(), json.RawMessage(`{"pattern":"**/*.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.txt") || strings.Contains(out, "b.txt") || strings.Contains(out, "b-link.txt") {
		t.Fatalf("glob = %q, want a.txt only", out)
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}
