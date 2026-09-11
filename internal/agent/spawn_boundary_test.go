package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// knownDirectChildRunners are the entry points that still construct a child
// outside TaskTool.RunProfileSpec. Each one re-resolves tools, depth, and
// permissions on its own, which is exactly how a boundary added in one place
// gets missed in another. The list may shrink; adding to it needs a reason in
// the pull request.
var knownDirectChildRunners = map[string]string{
	"internal/agent/task.go":   "defines the runners and is the unified path itself",
	"internal/boot/boot.go":    "skill try + run_skill runners",
	"internal/cli/review.go":   "reasonix review",
	"desktop/subagents_app.go": "desktop profile preview",
}

// A new fork must be a deliberate, reviewed choice rather than something that
// appears because one more caller found the low-level runner convenient.
func TestChildConstructionForksStayEnumerated(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == ".claude") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(body)
		if !strings.Contains(text, "RunSubAgentWithSession(") && !strings.Contains(text, "RunReadOnlySubAgentWithSession(") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if _, known := knownDirectChildRunners[rel]; !known {
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Fatalf("these files construct a sub-agent outside the unified runner: %s\n\nCompile the call into a ProfileExecSpec and run it through TaskTool.RunProfileSpec so it inherits tool scoping, depth caps, write claims, scheduler slots, and the completion contract. If a direct runner is genuinely required, add the file to knownDirectChildRunners with a reason.",
			strings.Join(found, ", "))
	}
}

// Converging read_only_task onto the unified runner must not quietly give it
// durable side effects: its contract is that the call leaves nothing behind.
func TestReadOnlyTaskStaysEphemeralOnTheUnifiedRunner(t *testing.T) {
	root := t.TempDir()
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "research done"}, {Type: provider.ChunkDone}},
	}}
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(NewSubagentScheduler(4, 4))

	ctx := withCallContext(context.Background(), "call-1", event.Discard, nil, false)
	ctx = WithParentSession(ctx, filepath.Join(root, "parent.jsonl"))
	out, err := NewReadOnlyTaskTool(task).Execute(ctx, []byte(`{"prompt":"inspect the parser"}`))
	if err != nil {
		t.Fatalf("read_only_task: %v", err)
	}
	if !strings.Contains(out, "research done") {
		t.Fatalf("answer = %q", out)
	}
	if strings.Contains(out, "Subagent reference") {
		t.Fatalf("read_only_task must not persist a transcript even under a parent session:\n%s", out)
	}
}
