package boot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

type backgroundMutationProvider struct {
	calls   atomic.Int32
	command string
}

func (*backgroundMutationProvider) Name() string { return "background-mutation" }

func (p *backgroundMutationProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	if p.calls.Add(1) == 1 {
		raw, _ := json.Marshal(map[string]any{"command": p.command, "run_in_background": true})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "background-writer", Name: "bash", Arguments: string(raw)}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "started"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectRunningBackgroundProcessLeavesTheWorkspaceWritable(t *testing.T) {
	home, root := statedBootHome(t), t.TempDir()
	sh := pinnedEffectShell()
	p := &backgroundMutationProvider{command: "echo ready > ready; sleep 120"}
	if sh.Kind == sandbox.ShellPowerShell {
		p.command = "Set-Content ready ready; Start-Sleep -Seconds 120"
	}
	provider.Register("background-mutation", func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", `default_model="test-model"
[agent]
collaboration="shared"
[codegraph]
enabled=false
[[providers]]
name="test-model"
kind="background-mutation"
model="x"
`)
	mirrorToUserConfigAt(t, home, root)
	approveWorkspaceAt(t, home, root)
	b := newBuilder()
	built, err := b.build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard, resolvedShell: sh})
	if err != nil {
		t.Fatal(err)
	}
	defer built.Controller.Close()
	built.Controller.EnsureSessionPath()
	built.Controller.SetToolApprovalMode(control.ToolApprovalYolo)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := built.Controller.RunTurn(ctx, "Start the background writer"); err != nil {
		t.Fatal(err)
	}
	history, _ := json.Marshal(built.Controller.History())
	jobID := regexp.MustCompile(`bash-[0-9]+`).FindString(string(history))
	if jobID == "" {
		t.Fatalf("missing background job: %s", history)
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("background writer did not start")
		}
	}
	other, err := mutation.New(root, config.WorkspaceLeaseDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		l, err := other.TryAcquire(ctx, mutation.Intent{Effect: mutation.Workspace, SessionID: "other"})
		if err == nil {
			l.End(mutation.NotExecuted)
			break
		}
		if !errors.Is(err, mutation.ErrBusy) || time.Now().After(deadline) {
			t.Fatalf("a running background service kept the workspace from other writers: %v", err)
		}
		<-ticker.C
	}
	if !b.session.jobs.KillForSession(sessionstore.BranchID(built.Controller.SessionPath()), jobID) {
		t.Fatal("background service ended before another writer was admitted")
	}
}
