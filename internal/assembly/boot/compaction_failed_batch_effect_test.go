package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

const failedBatchReads = 6

// failedBatchProvider works through several rounds, each one assistant turn that
// reads a handful of files and runs a command that fails, then writes a digest
// when asked to summarize.
type failedBatchProvider struct {
	mu     sync.Mutex
	reqs   []provider.Request
	rounds int
}

func (p *failedBatchProvider) Name() string { return "boot-failed-batch" }

func (p *failedBatchProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	summary := len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting the earlier part")
	round := p.rounds
	if !summary {
		p.rounds++
	}
	p.mu.Unlock()
	ch := make(chan provider.Chunk, failedBatchReads+3)
	call := func(id, name string, args any) {
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}}
	}
	switch {
	case summary:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "## Goal\n- read the packages"}
	case round < 5:
		for i := range failedBatchReads {
			call(fmt.Sprintf("r%d-%d", round, i), "read_file", map[string]string{"path": fmt.Sprintf("src%d.txt", i)})
		}
		call(fmt.Sprintf("t%d", round), "bash", map[string]any{"command": "echo FAILED-CHECK; exit 1"})
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *failedBatchProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

// One failing command in a parallel batch keeps that command, not the reads it
// ran beside. Through the real assembly the summary is billed once and installed,
// the failure still reaches the model, and no request carries a call without its
// result.
func TestEffectFailingCallDoesNotPinItsParallelBatch(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	kind := uniqueKind("boot-failed-batch")
	rec := &failedBatchProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeUserConfig(t, "[sandbox]\nbash = \"off\"\n")
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"
compact_ratio = 0.8
recent_keep = 2

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
context_window = 32000
`)
	for i := range failedBatchReads {
		writeFile(t, dir, fmt.Sprintf("src%d.txt", i), fmt.Sprintf("SIBLING-%d\n", i)+strings.Repeat("source line with detail.\n", 160))
	}
	approveWorkspace(t, dir)

	var mu sync.Mutex
	var blocked []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ContextMaintenanceEvent && e.Maintenance != nil && e.Maintenance.Status == "blocked" {
			mu.Lock()
			blocked = append(blocked, e.Maintenance.Code)
			mu.Unlock()
		}
	})
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	ctrl.SetSessionPath(filepath.Join(dir, ".reasonix", "sessions", "failed-batch.jsonl"))
	if err := ctrl.Run(context.Background(), "read every file and run the check"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	got := append([]string(nil), blocked...)
	mu.Unlock()
	if len(got) > 0 {
		t.Fatalf("a failing call in a batch blocked the fold: %v", got)
	}
	reqs := rec.requests()
	installed := -1
	for i, req := range reqs {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "<compaction-summary>") {
				installed = i
			}
		}
	}
	if installed < 0 {
		t.Fatalf("no request carried a digest; the fold was never installed (%d requests)", len(reqs))
	}
	msgs := reqs[installed].Messages
	answered := map[string]bool{}
	for _, m := range msgs {
		if m.Role == provider.RoleTool {
			answered[m.ToolCallID] = true
		}
	}
	failureSeen := false
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if !answered[tc.ID] {
				t.Errorf("call %s is issued with no result in the post-fold request", tc.ID)
			}
		}
		if m.Role == provider.RoleTool && strings.Contains(m.Content, "FAILED-CHECK") {
			failureSeen = true
		}
	}
	if !failureSeen {
		t.Errorf("the failed command's output did not survive the fold\n%s", messageDigest(msgs))
	}
	foldedAway := 0
	for _, m := range msgs {
		if m.Role == provider.RoleTool && strings.Contains(m.Content, "SIBLING-") {
			foldedAway++
		}
	}
	if foldedAway >= 5*failedBatchReads {
		t.Errorf("every read beside a failure was still pinned verbatim (%d results)", foldedAway)
	}
}
