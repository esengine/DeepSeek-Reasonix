package boot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

// failedJobProvider starts a background command that fails in the START turn,
// then in the CHECK turn waits for it in the same batch as an ordinary read,
// keeping what each call answered.
type failedJobProvider struct {
	mu      sync.Mutex
	answers map[string]string
}

func (p *failedJobProvider) Name() string { return "boot-failed-job" }

func (p *failedJobProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	for _, m := range req.Messages {
		if m.Role == provider.RoleTool {
			p.answers[m.ToolCallID] = m.Content
		}
	}
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	call := func(id, name string, args any) {
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}}
	}
	task, answered := "", map[string]bool{}
	for _, m := range req.Messages {
		switch {
		case m.Role == provider.RoleTool:
			answered[m.ToolCallID] = true
		case m.Role == provider.RoleUser && strings.Contains(m.Content, "CHECK"):
			task = "CHECK"
		case m.Role == provider.RoleUser && strings.Contains(m.Content, "START"):
			task = "START"
		}
	}
	switch {
	case task == "CHECK" && !answered["w1"]:
		id := ""
		for _, m := range req.Messages {
			if found := jobIDPattern.FindString(m.Content); found != "" {
				id = found
			}
		}
		call("w1", "wait", map[string]any{"job_ids": []string{id}})
		call("r1", "read_file", map[string]string{"path": "note.txt"})
	case task == "START" && !answered["b1"]:
		call("b1", "bash", map[string]any{"command": "echo boom-output; exit 3", "run_in_background": true})
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "noted"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// A background job that fails after it started is a failed read end to end:
// the live result carries Err, the recorded message carries ToolFailure, the
// model still reads the job's output under the error line, and the call
// batched beside it still runs.
func TestEffectReadingAFailedBackgroundJobIsAFailedCall(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	kind := uniqueKind("boot-failed-job")
	rec := &failedJobProvider{answers: map[string]string{}}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeUserConfigAt(t, home, "[sandbox]\nbash = \"off\"\n")
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	writeFile(t, dir, "note.txt", "note-body")
	approveWorkspaceAt(t, home, dir)

	var mu sync.Mutex
	liveErr := map[string]string{}
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ToolResult {
			mu.Lock()
			liveErr[e.Tool.ID] = e.Tool.Err
			mu.Unlock()
		}
	})
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	ctrl.SetSessionPath(filepath.Join(dir, ".reasonix", "sessions", "failed-job.jsonl"))
	if err := ctrl.Run(context.Background(), "START a background job"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := ctrl.Run(context.Background(), "CHECK the background job"); err != nil {
		t.Fatalf("check: %v", err)
	}

	rec.mu.Lock()
	waited, read := rec.answers["w1"], rec.answers["r1"]
	rec.mu.Unlock()
	if !strings.Contains(waited, "error: background job failed: bash-") || !strings.Contains(waited, "boom-output") {
		t.Fatalf("wait reached the model as %q, want the failure line with the job's output under it", waited)
	}
	if !strings.Contains(read, "note-body") {
		t.Fatalf("the read batched beside the failed wait answered %q, want it run", read)
	}
	mu.Lock()
	waitErr, readErr := liveErr["w1"], liveErr["r1"]
	mu.Unlock()
	if !strings.Contains(waitErr, "background job failed") || readErr != "" {
		t.Fatalf("live Err: wait %q, read %q; want only the wait failed", waitErr, readErr)
	}

	loaded, err := sessionstore.LoadSession(ctrl.SessionPath())
	if err != nil || loaded == nil {
		t.Fatalf("load %s: %v", ctrl.SessionPath(), err)
	}
	failure := map[string]bool{}
	for _, m := range loaded.Snapshot() {
		if m.Role == provider.RoleTool {
			failure[m.ToolCallID] = m.ToolFailure != nil
		}
	}
	if !failure["w1"] || failure["r1"] || failure["b1"] {
		t.Fatalf("recorded ToolFailure by call = %v, want only w1", failure)
	}
}
