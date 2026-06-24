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
		"parallel_tasks",
		"run_skill",
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
		"parallel_tasks",
		"run_skill",
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

func TestTaskToolBuildSubRegUsesSubagentToolRegistry(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentRegistryTool{name: "task"})
	parent.Add(subagentRegistryTool{name: "parallel_tasks"})
	parent.Add(subagentRegistryTool{name: "wait"})
	parent.Add(subagentRegistryTool{
		name:   "bash",
		schema: `{"type":"object","properties":{"command":{"type":"string"},"run_in_background":{"type":"boolean"}}}`,
	})
	task := &TaskTool{parentReg: parent}

	sub := task.buildSubReg(nil)
	for _, hidden := range []string{"task", "parallel_tasks", "wait"} {
		if _, ok := sub.Get(hidden); ok {
			t.Fatalf("task subagent registry should hide %q; got %v", hidden, sub.Names())
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
