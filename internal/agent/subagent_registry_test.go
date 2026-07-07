package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

type subagentRegistryTool struct {
	name     string
	schema   string
	readOnly bool
	result   string
}

func (t subagentRegistryTool) Name() string { return t.name }
func (t subagentRegistryTool) Description() string {
	return "Execute a command in the shell and return combined stdout/stderr."
}
func (t subagentRegistryTool) Schema() json.RawMessage {
	if t.schema != "" {
		return json.RawMessage(t.schema)
	}
	return json.RawMessage(`{"type":"object"}`)
}
func (t subagentRegistryTool) ReadOnly() bool { return t.readOnly }
func (t subagentRegistryTool) Execute(context.Context, json.RawMessage) (string, error) {
	return t.result, nil
}

func TestSubagentToolRegistryFiltersUnavailableToolsAndWrapsBash(t *testing.T) {
	parent := tool.NewRegistry()
	for _, name := range []string{
		"task",
		"read_only_task",
		"parallel_tasks",
		"run_skill",
		"read_only_skill",
		"read_skill",
		"install_skill",
		"install_source",
		"explore",
		"research",
		"review",
		"security_review",
		"wait",
		"bash_output",
		"kill_shell",
	} {
		parent.Add(subagentRegistryTool{name: name})
	}
	parent.Add(subagentRegistryTool{name: "read_file", readOnly: true})
	parent.Add(subagentRegistryTool{
		name:   "bash",
		schema: `{"type":"object","properties":{"command":{"type":"string"},"run_in_background":{"type":"boolean"}},"required":["command"]}`,
		result: "foreground ok",
	})

	sub := SubagentToolRegistry(parent, nil)
	for _, hidden := range []string{
		"task",
		"read_only_task",
		"parallel_tasks",
		"run_skill",
		"read_only_skill",
		"read_skill",
		"install_skill",
		"install_source",
		"explore",
		"research",
		"review",
		"security_review",
		"wait",
		"bash_output",
		"kill_shell",
	} {
		if _, ok := sub.Get(hidden); ok {
			t.Fatalf("subagent registry should hide %q; got %v", hidden, sub.Names())
		}
	}
	if _, ok := sub.Get("read_file"); !ok {
		t.Fatalf("subagent registry should keep read_file; got %v", sub.Names())
	}
	bash, ok := sub.Get("bash")
	if !ok {
		t.Fatalf("subagent registry should keep foreground bash; got %v", sub.Names())
	}
	if bash.ReadOnly() {
		t.Fatal("foreground-only bash must remain a writer")
	}
	if strings.Contains(string(bash.Schema()), "run_in_background") {
		t.Fatalf("subagent bash schema should not advertise run_in_background: %s", bash.Schema())
	}
	out, err := bash.Execute(context.Background(), json.RawMessage(`{"command":"printf ok"}`))
	if err != nil || out != "foreground ok" {
		t.Fatalf("foreground bash delegated to inner tool = %q, %v; want foreground ok, nil", out, err)
	}
	if _, err := bash.Execute(context.Background(), json.RawMessage(`{"command":"sleep 1","run_in_background":true}`)); err == nil || !strings.Contains(err.Error(), "background bash is unavailable in subagents") {
		t.Fatalf("background bash should return a subagent-specific error, got %v", err)
	}
}

func TestReadOnlySubagentToolRegistryKeepsOnlyResearchToolsAndSafeBash(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentRegistryTool{name: "task"})
	parent.Add(subagentRegistryTool{name: "read_only_task"})
	parent.Add(subagentRegistryTool{name: "read_only_skill", readOnly: true})
	parent.Add(subagentRegistryTool{name: "write_file"})
	parent.Add(subagentRegistryTool{name: "remember"})
	parent.Add(subagentRegistryTool{name: "todo_write", readOnly: true})
	parent.Add(subagentRegistryTool{name: "complete_step", readOnly: true})
	parent.Add(subagentRegistryTool{name: "connect_tool_source", readOnly: true})
	parent.Add(subagentRegistryTool{name: "read_file", readOnly: true})
	parent.Add(subagentRegistryTool{
		name:   "bash",
		schema: `{"type":"object","properties":{"command":{"type":"string"},"run_in_background":{"type":"boolean"}},"required":["command"]}`,
		result: "safe bash ok",
	})

	sub := ReadOnlySubagentToolRegistry(parent, nil)
	for _, hidden := range []string{"task", "read_only_task", "read_only_skill", "write_file", "remember", "todo_write", "complete_step", "connect_tool_source"} {
		if _, ok := sub.Get(hidden); ok {
			t.Fatalf("read-only subagent registry should hide %q; got %v", hidden, sub.Names())
		}
	}
	if _, ok := sub.Get("read_file"); !ok {
		t.Fatalf("read-only subagent registry should keep read_file; got %v", sub.Names())
	}
	bash, ok := sub.Get("bash")
	if !ok {
		t.Fatalf("read-only subagent registry should keep safe bash; got %v", sub.Names())
	}
	if !bash.ReadOnly() {
		t.Fatal("read-only subagent bash wrapper must report ReadOnly")
	}
	if strings.Contains(string(bash.Schema()), "run_in_background") {
		t.Fatalf("read-only subagent bash schema should not advertise run_in_background: %s", bash.Schema())
	}
	out, err := bash.Execute(context.Background(), json.RawMessage(`{"command":"git status"}`))
	if err != nil || out != "safe bash ok" {
		t.Fatalf("safe bash delegated to inner tool = %q, %v; want safe bash ok, nil", out, err)
	}
	out, err = bash.Execute(context.Background(), json.RawMessage(`{"command":"git status 2>/dev/null"}`))
	if err != nil || out != "safe bash ok" {
		t.Fatalf("safe redirected bash delegated to inner tool = %q, %v; want safe bash ok, nil", out, err)
	}
	out, err = bash.Execute(context.Background(), json.RawMessage(`{"command":"rm -rf tmp"}`))
	if err != nil || !strings.HasPrefix(out, "blocked:") {
		t.Fatalf("unsafe bash should be blocked as tool output, got %q, %v", out, err)
	}
}

func TestReadOnlySubagentToolRegistryAllowsOnlyReadOnlyDelegationBeforeDepthLimit(t *testing.T) {
	parent := tool.NewRegistry()
	for _, name := range []string{"task", "run_skill", "explore", "read_only_task", "read_only_skill", "read_skill", "write_file"} {
		parent.Add(subagentRegistryTool{name: name, readOnly: strings.HasPrefix(name, "read_only") || name == "read_skill"})
	}
	parent.Add(subagentRegistryTool{name: "read_file", readOnly: true})

	firstLayer := ReadOnlySubagentToolRegistryForDepth(parent, nil, 1, 2)
	for _, want := range []string{"read_file", "read_only_task", "read_only_skill", "read_skill"} {
		if _, ok := firstLayer.Get(want); !ok {
			t.Fatalf("first-layer read-only registry should expose %q; got %v", want, firstLayer.Names())
		}
	}
	for _, hidden := range []string{"task", "run_skill", "explore", "write_file"} {
		if _, ok := firstLayer.Get(hidden); ok {
			t.Fatalf("first-layer read-only registry should hide %q; got %v", hidden, firstLayer.Names())
		}
	}

	secondLayer := ReadOnlySubagentToolRegistryForDepth(parent, nil, 2, 2)
	for _, hidden := range []string{"task", "run_skill", "read_only_task", "read_only_skill", "read_skill", "explore", "write_file"} {
		if _, ok := secondLayer.Get(hidden); ok {
			t.Fatalf("depth-limited read-only registry should hide %q; got %v", hidden, secondLayer.Names())
		}
	}
}

// TestReadOnlySubagentToolRegistryExcludesUntrustedReadOnly proves an MCP tool
// whose ReadOnly()==true comes from an untrusted server readOnlyHint is excluded
// from a read-only research sub-agent, even though its ReadOnly contract is true.
func TestReadOnlySubagentToolRegistryExcludesUntrustedReadOnly(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentRegistryTool{name: "read_file", readOnly: true})
	parent.Add(untrustedReadOnlyTool{fakeTool{name: "mcp__srv__read", readOnly: true}})

	sub := ReadOnlySubagentToolRegistry(parent, nil)
	if _, ok := sub.Get("mcp__srv__read"); ok {
		t.Fatalf("read-only subagent registry must exclude an untrusted readOnlyHint MCP tool; got %v", sub.Names())
	}
	if _, ok := sub.Get("read_file"); !ok {
		t.Fatalf("a trusted read-only tool should remain; got %v", sub.Names())
	}
}

func TestTaskToolBuildSubRegUsesSubagentToolRegistry(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentRegistryTool{name: "task"})
	parent.Add(subagentRegistryTool{name: "read_only_task"})
	parent.Add(subagentRegistryTool{name: "read_only_skill", readOnly: true})
	parent.Add(subagentRegistryTool{name: "parallel_tasks"})
	parent.Add(subagentRegistryTool{name: "wait"})
	parent.Add(subagentRegistryTool{
		name:   "bash",
		schema: `{"type":"object","properties":{"command":{"type":"string"},"run_in_background":{"type":"boolean"}}}`,
	})
	task := (&TaskTool{parentReg: parent}).WithMaxSubagentDepth(2)

	firstLayer := task.buildSubReg(nil, 1)
	for _, exposed := range []string{"task", "read_only_task", "read_only_skill"} {
		if _, ok := firstLayer.Get(exposed); !ok {
			t.Fatalf("first-layer subagent registry should expose %q; got %v", exposed, firstLayer.Names())
		}
	}
	for _, hidden := range []string{"parallel_tasks", "wait"} {
		if _, ok := firstLayer.Get(hidden); ok {
			t.Fatalf("first-layer subagent registry should hide %q; got %v", hidden, firstLayer.Names())
		}
	}

	sub := task.buildSubReg(nil, 2)
	for _, hidden := range []string{"task", "read_only_task", "read_only_skill", "parallel_tasks", "wait"} {
		if _, ok := sub.Get(hidden); ok {
			t.Fatalf("depth-limited subagent registry should hide %q; got %v", hidden, sub.Names())
		}
	}
	bash, ok := sub.Get("bash")
	if !ok {
		t.Fatalf("task subagent registry should keep bash; got %v", sub.Names())
	}
	if strings.Contains(string(bash.Schema()), "run_in_background") {
		t.Fatalf("task subagent bash schema should be foreground-only: %s", bash.Schema())
	}
}

func TestTaskToolDescribesSubagentToolBoundary(t *testing.T) {
	task := &TaskTool{}
	for label, text := range map[string]string{
		"description": task.Description(),
		"schema":      string(task.Schema()),
	} {
		for _, want := range []string{"wait", "bash_output", "kill_shell", "foreground-only"} {
			if !strings.Contains(text, want) {
				t.Fatalf("task %s should mention %q in subagent tool boundary: %s", label, want, text)
			}
		}
	}
}
