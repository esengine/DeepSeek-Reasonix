package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/jobs"
)

type automaticWindowsProvider struct{}

func (*automaticWindowsProvider) Name() string { return "automatic-windows" }

func (*automaticWindowsProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	if hasToolResult(req) {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	} else if strings.Contains(firstUser(req), "AUTOMATIC CHILD") {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "write", Name: "write_file", Arguments: `{"path":"result.txt","content":"shared transaction"}`}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "task", Name: "task", Arguments: `{"prompt":"AUTOMATIC CHILD","isolation":"auto","tools":["write_file"],"write_paths":["result.txt"],"run_in_background":true}`}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectAutomaticWindowsTaskFallsBackBeforeCreatingCandidate(t *testing.T) {
	home, root := statedBootHome(t), t.TempDir()
	writeFile(t, root, "base.txt", "base")
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "base")
	var mu sync.Mutex
	var reason string
	opts := installationEffectOptions(t, home, root, &automaticWindowsProvider{})
	cfg := config.Default()
	cfg.Agent.Collaboration = "auto"
	opts.frozenConfig = cfg
	opts.Sink = event.FuncSink(func(e event.Event) {
		if e.Kind == event.ToolProgress && e.Tool.Name == event.SubagentProgressNoticeName && strings.Contains(e.Tool.Output, sandbox.ErrIncompleteIntegrity.Error()) {
			mu.Lock()
			reason = e.Tool.Output
			mu.Unlock()
		}
	})
	b := newBuilder()
	built, err := b.build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer built.Controller.Close()
	bindPresenceSession(t, built.Controller)
	built.Controller.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := built.Controller.Run(t.Context(), "delegate automatic writer"); err != nil {
		t.Fatal(err)
	}
	history, _ := json.Marshal(built.Controller.History())
	id := regexp.MustCompile(`task-[0-9]+`).FindString(string(history))
	if id == "" {
		t.Fatalf("no background task: %s", history)
	}
	owner := sessionstore.BranchID(built.Controller.SessionPath())
	results, _ := b.session.jobs.WaitForSession(t.Context(), owner, []string{id}, jobs.WaitOptions{Timeout: time.Minute})
	if len(results) != 1 || results[0].Status != jobs.Done {
		t.Fatalf("shared fallback failed: %+v", results)
	}
	mu.Lock()
	gotReason := reason
	mu.Unlock()
	if gotReason == "" {
		t.Fatal("automatic fallback omitted its unsupported-integrity reason")
	}
	if body, err := os.ReadFile(filepath.Join(root, "result.txt")); err != nil || string(body) != "shared transaction" {
		t.Fatalf("task did not execute in shared transactions: %q %v", body, err)
	}
	catalog, err := built.Controller.IsolationResults(t.Context())
	if err != nil || len(catalog.Results) != 0 {
		t.Fatalf("failed automatic preparation left a candidate: %+v %v", catalog, err)
	}
	if built.Controller.WorkspaceLeaseState().Acquired {
		t.Fatal("fallback retained a task-lifetime workspace claim")
	}
}
