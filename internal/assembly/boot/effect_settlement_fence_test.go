package boot

import (
	"context"
	"crypto/sha256"
	"fmt"
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
	"reasonix/internal/state/sessionstore"
)

func TestEffectSettlementFenceStopsApplyAcrossControllerRebuild(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, rebuild := range []bool{false, true} {
		t.Run(fmt.Sprint(rebuild), func(t *testing.T) {
			home, dir := statedBootHome(t), testenv.TempDir(t)
			kind := uniqueKind("boot-settlement-fence")
			rec := &restartIsolationProvider{}
			provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
			writeFile(t, dir, "reasonix.toml", `
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
			gitInDir(t, dir, "init", "-q")
			mirrorToUserConfigAt(t, home, dir)
			gitInDir(t, dir, "add", "-A")
			gitInDir(t, dir, "commit", "-q", "-m", "init")
			build := func() *control.Controller {
				ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
				if err != nil {
					t.Fatal(err)
				}
				ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
				t.Cleanup(ctrl.Close)
				return ctrl
			}
			ctrl := build()
			path := filepath.Join(dir, ".reasonix", "sessions", "settlement-fence.jsonl")
			ctrl.SetSessionPath(path)
			if err := ctrl.Run(t.Context(), "DELEGATE make out.txt in isolation"); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			id := isoIDPattern.FindString(string(raw))
			if id == "" {
				t.Fatal("task never produced a retained isolated result")
			}
			fences := filepath.Join(config.RootsForHome(home).DeliveryWorktreeDir(), "isolated", ".settlements-v1")
			if err := os.MkdirAll(fences, 0o700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(fences, fmt.Sprintf("%x.json", sha256.Sum256([]byte(id))))
			body := []byte(`{"version":999,"phase":"future-recovery"}`)
			if err := os.WriteFile(marker, body, 0o600); err != nil {
				t.Fatal(err)
			}
			if rebuild {
				ctrl.Close()
				loaded, err := sessionstore.LoadSession(path)
				if err != nil || loaded == nil {
					t.Fatalf("load retained session: %v", err)
				}
				ctrl = build()
				if err := ctrl.Resume(loaded, path); err != nil {
					t.Fatal(err)
				}
			}
			if err := ctrl.Run(t.Context(), "APPLY the pending isolated result"); err != nil {
				t.Fatal(err)
			}
			rec.mu.Lock()
			result := rec.applyResult
			rec.mu.Unlock()
			if !strings.Contains(result, "settlement") || !strings.Contains(result, "recovery") {
				t.Fatalf("provider did not receive the actual host refusal: %q", result)
			}
			if _, err := os.Stat(filepath.Join(dir, "out.txt")); !os.IsNotExist(err) {
				t.Fatalf("fenced apply reached the source: %v", err)
			}
			if got, err := os.ReadFile(marker); err != nil || string(got) != string(body) {
				t.Fatalf("future record changed: %q %v", got, err)
			}
		})
	}
}
