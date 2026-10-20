package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

const batchScript = `
reads = call_many([{"name": "read_file", "args": {"path": p}} for p in ["a.txt", "b.txt", "a.txt", "b.txt"]])
print("parallel", [r.output.strip() for r in reads])
rs = call_many([
    {"name": "read_file", "args": {"path": "a.txt"}},
    {"name": "read_file", "args": {"path": "b.txt"}},
    {"name": "bash", "args": {"command": "rm -rf a.txt"}},
])
print([r.ok for r in rs], rs[0].output.strip(), rs[1].output.strip())
`

// A batch passes the same gates as single calls: reads run, the deny rule
// still refuses rm, and the answers come back in the order they were asked.
func TestEffectRunScriptBatchKeepsEveryGate(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	writeFile(t, dir, "a.txt", "alpha")
	writeFile(t, dir, "b.txt", "beta")
	rec := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("s1", "run_script", map[string]any{"script": batchScript})
		},
	}}
	kind := "boot-script-" + strings.ToLower(t.Name())
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"
code_mode = true

[permissions]
deny = ["Bash(rm:*)"]

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspaceAt(t, home, dir)
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: &noticeRecorder{}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_ = ctrl.Run(context.Background(), "read both files at once")
	ctrl.Close()
	reqs := agentRequests(rec.requests())
	results := effectToolResults(reqs[len(reqs)-1])
	if len(results) != 1 || !strings.Contains(results[0], `parallel ["alpha", "beta", "alpha", "beta"]`) || !strings.Contains(results[0], "[True, True, False] alpha beta") {
		t.Fatalf("script result = %q", results)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal("the deny rule did not hold inside a batch")
	}
}

const generateThenReadScript = `
gen = try_call("use_capability", action="call", capability_id="mcp-tool:live/job", arguments={})
back = call("read_file", path=gen.result_ref)
print(gen.ok, gen.result_ref.startswith("tool-result:"), "media-1" in back)
`

// A script reaches a retained result through its typed reference, never by
// reading the reference out of the call's text.
func TestEffectRunScriptReadsBackThroughTheTypedResultReference(t *testing.T) {
	rec := &browserScriptProvider{rounds: []func(string) *provider.ToolCall{
		func(string) *provider.ToolCall {
			return browserCall("s1", "run_script", map[string]any{"script": generateThenReadScript})
		},
	}}
	provider.Register("boot-script-generate-read", func(provider.Config) (provider.Provider, error) { return rec, nil })
	home, workspace := mediaTaskWorkspace(t, "boot-script-generate-read", newMediaTaskService(t, true).URL)
	cfg := filepath.Join(workspace, "reasonix.toml")
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(strings.Replace(string(raw), `system_prompt = "BASE"`, "system_prompt = \"BASE\"\ncode_mode = true", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	ctrl, err := Build(t.Context(), Options{Sink: event.Discard, Home: home, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	if err := ctrl.Run(t.Context(), "generate one shot and read its result"); err != nil {
		t.Fatal(err)
	}
	reqs := agentRequests(rec.requests())
	results := effectToolResults(reqs[len(reqs)-1])
	if len(results) != 1 || !strings.Contains(results[0], "True True True") {
		t.Fatalf("script result = %q", results)
	}
}
