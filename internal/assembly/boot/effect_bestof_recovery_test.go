package boot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/runtime/isolation"
)

type bestOfRecoveryProvider struct {
	dir, mode, recoverID string
	arrived              sync.WaitGroup
	children             atomic.Int32
	judges               atomic.Int32
}

func (*bestOfRecoveryProvider) Name() string { return "bestof-store-recovery" }

func (p *bestOfRecoveryProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	call := func(name string, args any) {
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "recovery-call", Name: name, Arguments: string(raw)}}
	}
	switch {
	case len(req.Tools) == 0:
		p.judges.Add(1)
		if err := os.WriteFile(filepath.Join(p.dir, "out.txt"), []byte("concurrent user edit\n"), 0o644); err != nil {
			return nil, err
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: `{"winner":1,"reason":"first"}`}
	case strings.Contains(firstUser(req), candidateTask):
		if !hasToolResult(req) {
			call("write_file", map[string]string{"path": "out.txt", "content": "retained Boot bytes\n"})
		} else {
			if p.mode == "panic" {
				n := p.children.Add(1)
				if n <= 2 {
					p.arrived.Done()
					p.arrived.Wait()
				}
				if n == 1 {
					panic("Boot candidate failed after a real write")
				}
			}
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "candidate complete"}
		}
	case hasToolResult(req):
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	case strings.Contains(firstUser(req), "RECOVER-ORIGINAL-ID"):
		call("apply_isolated", map[string]string{"id": p.recoverID})
	default:
		call("best_of_n", map[string]any{"prompt": candidateTask, "n": 2})
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func setupBestOfRecovery(t *testing.T, mode string) (string, string, *bestOfRecoveryProvider) {
	t.Helper()
	home, dir := statedBootHome(t), testenv.TempDir(t)
	p := &bestOfRecoveryProvider{dir: dir, mode: mode}
	p.arrived.Add(2)
	kind := "boot-bestof-store-" + mode
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"
[agent]
system_prompt = "BASE"
best_of_n = true
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
	return home, dir, p
}

func TestEffectBestOfRetainsOriginalIDsAcrossBoots(t *testing.T) {
	for _, mode := range []string{"conflict", "panic"} {
		t.Run(mode, func(t *testing.T) {
			home, dir, p := setupBestOfRecovery(t, mode)
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
			if err != nil {
				t.Fatal(err)
			}
			if err := ctrl.Run(t.Context(), "try candidates"); err != nil {
				t.Fatal(err)
			}
			var reported []string
			for _, m := range ctrl.History() {
				if m.Role == provider.RoleTool {
					reported = append(reported, isoIDPattern.FindAllString(m.Content, -1)...)
				}
			}
			ctrl.Close()
			managed := filepath.Join(config.RootsForHome(home).DeliveryWorktreeDir(), "isolated")
			repo, err := gitcmd.Open(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			store := isolation.NewStore(managed, dir, repo)
			ids := store.Pending()
			if len(ids) == 0 {
				t.Fatal("Boot returned no retained IDs")
			}
			for _, id := range ids {
				if !slices.Contains(reported, id) {
					t.Fatalf("model did not receive original ID %s: %v", id, reported)
				}
			}
			if mode == "panic" {
				if p.judges.Load() != 0 || len(ids) != 2 {
					t.Fatalf("panic reached judge or lost siblings: %v", ids)
				}
				if _, err := os.Stat(filepath.Join(dir, "out.txt")); !os.IsNotExist(err) {
					t.Fatalf("panic published source: %v", err)
				}
				unknown := 0
				for _, id := range ids {
					if _, err := store.Paths(id); errors.Is(err, isolation.ErrCandidateUnsettled) {
						unknown++
					}
				}
				if unknown != 1 {
					t.Fatalf("unknown scopes: %d", unknown)
				}
				return
			}
			if len(ids) != 1 || p.judges.Load() != 1 {
				t.Fatalf("conflicted winner: %v", ids)
			}
			if err := os.Remove(filepath.Join(dir, "out.txt")); err != nil {
				t.Fatal(err)
			}
			p.recoverID = ids[0]
			rebuilt, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
			if err != nil {
				t.Fatal(err)
			}
			defer rebuilt.Close()
			if err := rebuilt.Run(t.Context(), "RECOVER-ORIGINAL-ID"); err != nil {
				t.Fatal(err)
			}
			if raw, err := os.ReadFile(filepath.Join(dir, "out.txt")); err != nil || string(raw) != "retained Boot bytes\n" {
				t.Fatalf("recovery bytes %q: %v", raw, err)
			}
			if len(store.Pending()) != 0 {
				t.Fatal("recovered original result remains pending")
			}
		})
	}
}
