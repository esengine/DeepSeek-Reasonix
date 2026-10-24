package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/session/control"
)

type environmentScriptProvider struct {
	isolationScriptProvider
	discardID     string
	discardResult string
}

func (p *environmentScriptProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if strings.Contains(firstUser(req), "discard retained fixture") {
		ch := make(chan provider.Chunk, 2)
		if hasToolResult(req) {
			p.mu.Lock()
			p.discardResult = lastToolResult(req)
			p.mu.Unlock()
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "result retained"}
		} else {
			ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "discard", Name: "discard_isolated", Arguments: `{"id":"` + p.discardID + `"}`}}
		}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	if strings.Contains(firstUser(req), isolatedTask) {
		return p.isolationScriptProvider.Stream(ctx, req)
	}
	ch := make(chan provider.Chunk, 2)
	if hasToolResult(req) {
		p.mu.Lock()
		p.childSaw = lastToolResult(req)
		p.mu.Unlock()
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "candidate retained"}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "task", Name: "task", Arguments: `{"prompt":"` + isolatedTask + `","isolation":"worktree","tools":["write_file"],"write_paths":["out.txt"],"effort":"high","max_steps":4}`}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectUnifiedEnvironmentRetainsCandidateAndToolBounds(t *testing.T) {
	home, dir := statedBootHome(t), testenv.TempDir(t)
	managed := filepath.Join(config.RootsForHome(home).DeliveryWorktreeDir(), "isolated")
	rec := &environmentScriptProvider{isolationScriptProvider: isolationScriptProvider{dir: dir, managed: managed}}
	kind := "boot-unified-environment"
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
collaboration = "shared"
isolation_copy_paths = ["deps"]
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
supported_efforts = ["high"]
`)
	mirrorToUserConfigAt(t, home, dir)
	writeFile(t, dir, ".gitignore", "deps/\n")
	writeFile(t, dir, "deps/package", "independent input\n")
	gitInDir(t, dir, "init", "-q")
	gitInDir(t, dir, "add", "-A")
	gitInDir(t, dir, "commit", "-q", "-m", "init")
	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := ctrl.Run(t.Context(), "delegate a bounded isolated writer"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "out.txt")); !os.IsNotExist(err) {
		t.Fatalf("candidate wrote into source: %v", err)
	}
	if !isoIDPattern.MatchString(rec.childSaw) {
		t.Fatalf("no retained candidate: %s", rec.childSaw)
	}
	catalog, err := ctrl.IsolationResults(t.Context())
	if err != nil || !catalog.Enabled || len(catalog.Results) != 1 || catalog.Results[0].ID != isoIDPattern.FindString(rec.childSaw) || catalog.Results[0].Verification != "unknown" {
		t.Fatalf("actual Build did not expose the retained candidate with truthful evidence: %+v %v", catalog, err)
	}
	preview, err := ctrl.PreviewIsolatedDiscard(t.Context(), catalog.Results[0].ID)
	if err != nil || preview.Revision == "" || preview.CandidateRoot == dir {
		t.Fatalf("host preview is not bound to the isolated candidate: %+v %v", preview, err)
	}
	requests := rec.requests()
	if len(requests) == 0 {
		t.Fatal("isolated provider never executed")
	}
	for _, req := range requests {
		for _, schema := range req.Tools {
			if schema.Name == "bash" || schema.Name == "edit_file" {
				t.Fatalf("isolated child widened tool grant: %s", schema.Name)
			}
		}
	}
	marks, _ := filepath.Glob(filepath.Join(managed, "*", "entry-v2.json"))
	if len(marks) != 1 {
		t.Fatalf("retained records: %v", marks)
	}
	raw, err := os.ReadFile(marks[0])
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		State       string                       `json:"state"`
		Cand        worktree.Candidate           `json:"cand"`
		Environment *worktree.EnvironmentReceipt `json:"prepared_environment"`
		Evidence    struct {
			Verification string `json:"verification"`
			Receipts     []struct {
				Tool string `json:"tool"`
			} `json:"receipts"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if record.State != "pending" || record.Evidence.Verification != "unknown" || len(record.Evidence.Receipts) == 0 || record.Evidence.Receipts[0].Tool != "write_file" {
		t.Fatalf("candidate record: %s", raw)
	}
	if record.Environment == nil || record.Environment.ContentDigest == "" || record.Environment.Entries != 2 {
		t.Fatal("real boot assembly did not prepare declared dependencies")
	}
	if body, err := os.ReadFile(filepath.Join(record.Cand.WorktreeRoot, "deps/package")); err != nil || string(body) != "independent input\n" {
		t.Fatalf("isolated runtime lacks dependency: %q %v", body, err)
	}
	frozen, err := config.RootsForHome(home).LoadForRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	frozen.Agent.Collaboration, frozen.Agent.WorktreeIsolation = "", true
	rec.discardID = catalog.Results[0].ID
	var refused string
	legacySink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ToolResult {
			refused = e.Tool.RefusalCode
		}
	})
	legacy, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: legacySink, frozenConfig: frozen})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(legacy.Close)
	legacy.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := legacy.Run(t.Context(), "discard retained fixture"); err != nil {
		t.Fatal(err)
	}
	if refused != "isolation.user_action_required" {
		t.Fatalf("legacy Build bypassed managed-result policy: %q / %s", refused, rec.discardResult)
	}
	if _, err := os.Stat(record.Cand.WorktreeRoot); err != nil {
		t.Fatal("legacy runtime removed coordinated result", err)
	}
}
