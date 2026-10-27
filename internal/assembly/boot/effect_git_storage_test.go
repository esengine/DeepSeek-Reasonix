package boot

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

func TestEffectIsolatedTaskOwnsGitStorage(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	for _, mode := range []string{"ambient_existing", "ambient_missing", "control"} {
		t.Run(mode, func(t *testing.T) {
			home, root := statedBootHome(t), testenv.TempDir(t)
			kind := "boot-git-storage-" + mode
			rec := &isolationScriptProvider{dir: root, managed: filepath.Join(config.RootsForHome(home).DeliveryWorktreeDir(), "isolated")}
			provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
			writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
worktree_isolation = true
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
			gitInDir(t, root, "init", "-q")
			mirrorToUserConfigAt(t, home, root)
			gitInDir(t, root, "add", "-A")
			gitInDir(t, root, "commit", "-q", "-m", "fixture")
			index := filepath.Join(root, ".git", "index")
			before, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			ambient := filepath.Join(testenv.TempDir(t), "ambient-index")
			var original []byte
			if mode == "ambient_existing" {
				writeFile(t, root, "ambient-only.txt", "staged in another index\n")
				for _, args := range [][]string{{"read-tree", "HEAD"}, {"add", "ambient-only.txt"}} {
					cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
					cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+ambient)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("ambient fixture: %v %s", err, out)
					}
				}
				if err := os.Remove(filepath.Join(root, "ambient-only.txt")); err != nil {
					t.Fatal(err)
				}
				original, err = os.ReadFile(ambient)
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode != "control" {
				t.Setenv("GIT_INDEX_FILE", ambient)
			}
			ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			if err := ctrl.Run(t.Context(), "make out.txt without touching my tree until I say"); err != nil {
				t.Fatal(err)
			}
			ctrl.Close()
			if mode == "ambient_existing" {
				after, err := os.ReadFile(ambient)
				if err != nil || !bytes.Equal(original, after) {
					t.Fatal("isolated task rewrote the ambient index")
				}
			}
			if mode == "ambient_missing" {
				if _, err := os.Stat(ambient); !os.IsNotExist(err) {
					t.Fatalf("isolated task created an ambient index: %v", err)
				}
			}
			after, err := os.ReadFile(index)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("isolated task changed the source index")
			}
			out, err := os.ReadFile(filepath.Join(root, "out.txt"))
			if err != nil || strings.ReplaceAll(string(out), "\r\n", "\n") != "made in isolation\n" {
				t.Fatalf("applied result = %q %v", out, err)
			}
			if rec.landedBeforeApp || rec.childWorktree == "" {
				t.Fatal("task did not write in its observed worktree before apply")
			}
		})
	}
}
