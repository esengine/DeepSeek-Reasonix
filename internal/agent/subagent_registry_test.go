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

type subagentCapabilityProxy struct {
	subagentRegistryTool
}

type subagentMCPTool struct {
	subagentRegistryTool
	server           string
	raw              string
	destructive      bool
	serverAuthorized bool
}

func (t subagentMCPTool) MCPServerName() string     { return t.server }
func (t subagentMCPTool) MCPRawToolName() string    { return t.raw }
func (t subagentMCPTool) MCPDestructiveHint() bool  { return t.destructive }
func (t subagentMCPTool) MCPServerAuthorized() bool { return t.serverAuthorized }

func (t subagentCapabilityProxy) ResolveCall(_ context.Context, args json.RawMessage) (tool.ResolvedCall, error) {
	var p struct {
		CapabilityID string `json:"capability_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return tool.ResolvedCall{}, err
	}
	return tool.ResolvedCall{DisplayName: t.Name(), CapabilityID: p.CapabilityID, ReadOnly: true, SkipExecute: true, Result: p.CapabilityID}, nil
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
		"fleet",
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
		"fleet",
		"run_skill",
		"read_only_skill",
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
	if _, ok := sub.Get("read_skill"); !ok {
		t.Fatalf("depth-capped subagent registry should keep read_skill (it renders text, it cannot recurse); got %v", sub.Names())
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

func TestSubagentToolRegistryRestrictsCapabilityProxyToAllowedMCPIDs(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentCapabilityProxy{subagentRegistryTool{name: "use_capability", readOnly: true}})
	allowedID := "mcp-tool:figma/search"

	for _, sub := range []*tool.Registry{
		SubagentToolRegistry(parent, []string{allowedID}),
		ReadOnlySubagentToolRegistry(parent, []string{allowedID}),
	} {
		proxy, ok := sub.Get("use_capability")
		if !ok {
			t.Fatalf("restricted capability proxy missing: %v", sub.Names())
		}
		resolver, ok := proxy.(tool.CallResolver)
		if !ok {
			t.Fatalf("restricted proxy does not resolve calls: %T", proxy)
		}
		if _, err := resolver.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:figma/search"}`)); err != nil {
			t.Fatalf("allowed capability was rejected: %v", err)
		}
		if _, err := resolver.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:other/delete"}`)); err == nil || !strings.Contains(err.Error(), "outside this subagent's allowed-tools") {
			t.Fatalf("disallowed capability was not rejected: %v", err)
		}
		if _, err := proxy.Execute(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:other/delete"}`)); err == nil {
			t.Fatal("direct execution bypassed the restricted capability allowlist")
		}
	}

	parent.Add(subagentMCPTool{
		subagentRegistryTool: subagentRegistryTool{name: "mcp__figma__search", readOnly: true},
		server:               "figma",
		raw:                  "search",
		serverAuthorized:     true,
	})
	direct := SubagentToolRegistry(parent, []string{"mcp__figma__search", allowedID})
	if _, ok := direct.Get("mcp__figma__search"); !ok {
		t.Fatalf("direct MCP tool missing: %v", direct.Names())
	}
	if _, ok := direct.Get("use_capability"); ok {
		t.Fatalf("restricted proxy should not duplicate an available direct MCP tool: %v", direct.Names())
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
	out, err = bash.Execute(context.Background(), json.RawMessage(`{"command":"git status","run_in_background":true}`))
	if err != nil || !strings.HasPrefix(out, "blocked:") {
		t.Fatalf("background read-only bash should be blocked as tool output, got %q, %v", out, err)
	}
	out, err = bash.Execute(context.Background(), json.RawMessage(`{"command":"git status","preserve_background_processes":true}`))
	if err != nil || !strings.HasPrefix(out, "blocked:") {
		t.Fatalf("process-preserving read-only bash should be blocked as tool output, got %q, %v", out, err)
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
	for _, hidden := range []string{"task", "run_skill", "read_only_task", "read_only_skill", "explore", "write_file"} {
		if _, ok := secondLayer.Get(hidden); ok {
			t.Fatalf("depth-limited read-only registry should hide %q; got %v", hidden, secondLayer.Names())
		}
	}
	if _, ok := secondLayer.Get("read_skill"); !ok {
		t.Fatalf("depth-limited read-only registry should keep read_skill (it renders text, it cannot recurse); got %v", secondLayer.Names())
	}
}

func TestReadOnlySubagentToolRegistryIncludesMCPReadOnlyHint(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentRegistryTool{name: "read_file", readOnly: true})
	parent.Add(subagentMCPTool{
		subagentRegistryTool: subagentRegistryTool{name: "mcp__srv__read", readOnly: true},
		server:               "srv",
		raw:                  "read",
		serverAuthorized:     true,
	})

	sub := ReadOnlySubagentToolRegistry(parent, nil)
	if _, ok := sub.Get("mcp__srv__read"); !ok {
		t.Fatalf("read-only subagent registry should include an installed MCP read-only tool; got %v", sub.Names())
	}
	if _, ok := sub.Get("read_file"); !ok {
		t.Fatalf("a trusted read-only tool should remain; got %v", sub.Names())
	}
}

func TestCustomProfileAllowlistRestrictsMCPTools(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentRegistryTool{name: "read_file", readOnly: true})
	parent.Add(subagentRegistryTool{name: "write_file"})
	parent.Add(subagentMCPTool{
		subagentRegistryTool: subagentRegistryTool{name: "mcp__chrome__list_pages", readOnly: true},
		server:               "chrome",
		raw:                  "list_pages",
		serverAuthorized:     true,
	})
	parent.Add(subagentMCPTool{
		subagentRegistryTool: subagentRegistryTool{name: "mcp__chrome__new_page"},
		server:               "chrome",
		raw:                  "new_page",
		serverAuthorized:     true,
	})
	parent.Add(subagentMCPTool{
		subagentRegistryTool: subagentRegistryTool{name: "mcp__other__secret"},
		server:               "other",
		raw:                  "secret",
		serverAuthorized:     false,
	})

	// A custom profile boundary is authoritative even for installed MCP tools.
	general := SubagentToolRegistry(parent, []string{"read_file"})
	if _, ok := general.Get("read_file"); !ok {
		t.Fatalf("custom profile should keep allowlisted built-in; got %v", general.Names())
	}
	if _, ok := general.Get("write_file"); ok {
		t.Fatalf("custom profile should not include non-allowlisted writer; got %v", general.Names())
	}
	for _, name := range []string{"mcp__chrome__list_pages", "mcp__chrome__new_page", "mcp__other__secret"} {
		if _, ok := general.Get(name); ok {
			t.Fatalf("custom profile should exclude non-allowlisted MCP %q; got %v", name, general.Names())
		}
	}

	explicit := SubagentToolRegistry(parent, []string{"mcp__chrome__*"})
	for _, name := range []string{"mcp__chrome__list_pages", "mcp__chrome__new_page"} {
		if _, ok := explicit.Get(name); !ok {
			t.Fatalf("explicit MCP wildcard should include %q; got %v", name, explicit.Names())
		}
	}

	ro := ReadOnlySubagentToolRegistry(parent, []string{"read_file"})
	if _, ok := ro.Get("mcp__chrome__list_pages"); ok {
		t.Fatalf("read-only custom profile should exclude non-allowlisted MCP; got %v", ro.Names())
	}
	if _, ok := ro.Get("mcp__chrome__new_page"); ok {
		t.Fatalf("read-only subagent must not inherit writer MCP; got %v", ro.Names())
	}

	explicitRO := ReadOnlySubagentToolRegistry(parent, []string{"mcp__chrome__*"})
	if _, ok := explicitRO.Get("mcp__chrome__list_pages"); !ok {
		t.Fatalf("read-only MCP wildcard should include the authorized reader; got %v", explicitRO.Names())
	}
	if _, ok := explicitRO.Get("mcp__chrome__new_page"); ok {
		t.Fatalf("read-only MCP wildcard must still exclude writers; got %v", explicitRO.Names())
	}
}

func TestMCPToolAvailabilityAcrossGeneralAndReadOnlySubagents(t *testing.T) {
	tests := []struct {
		name               string
		readOnly           bool
		destructive        bool
		authorized         bool
		wantGeneral        bool
		wantStrictReadOnly bool
	}{
		{name: "authorized reader", readOnly: true, authorized: true, wantGeneral: true, wantStrictReadOnly: true},
		{name: "authorized writer", authorized: true, wantGeneral: true},
		{name: "authorized destructive reader", readOnly: true, destructive: true, authorized: true, wantGeneral: true},
		{name: "unauthorized reader", readOnly: true, wantGeneral: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const name = "mcp__srv__tool"
			parent := tool.NewRegistry()
			parent.Add(subagentMCPTool{
				subagentRegistryTool: subagentRegistryTool{name: name, readOnly: tc.readOnly},
				server:               "srv",
				raw:                  "tool",
				destructive:          tc.destructive,
				serverAuthorized:     tc.authorized,
			})

			_, gotGeneral := SubagentToolRegistry(parent, nil).Get(name)
			if gotGeneral != tc.wantGeneral {
				t.Fatalf("general subagent availability = %v, want %v", gotGeneral, tc.wantGeneral)
			}
			_, gotStrictReadOnly := ReadOnlySubagentToolRegistry(parent, nil).Get(name)
			if gotStrictReadOnly != tc.wantStrictReadOnly {
				t.Fatalf("read-only subagent availability = %v, want %v", gotStrictReadOnly, tc.wantStrictReadOnly)
			}
			_, gotPlanner := FilterReadOnlyRegistry(parent).Get(name)
			if gotPlanner != tc.wantStrictReadOnly {
				t.Fatalf("planner availability = %v, want %v", gotPlanner, tc.wantStrictReadOnly)
			}
		})
	}
}

func TestTaskToolBuildSubRegUsesSubagentToolRegistry(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentRegistryTool{name: "task"})
	parent.Add(subagentRegistryTool{name: "read_only_task"})
	parent.Add(subagentRegistryTool{name: "read_only_skill", readOnly: true})
	parent.Add(subagentRegistryTool{name: "parallel_tasks"})
	parent.Add(subagentRegistryTool{name: "fleet"})
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
	for _, hidden := range []string{"parallel_tasks", "fleet", "wait"} {
		if _, ok := firstLayer.Get(hidden); ok {
			t.Fatalf("first-layer subagent registry should hide %q; got %v", hidden, firstLayer.Names())
		}
	}

	sub := task.buildSubReg(nil, 2)
	for _, hidden := range []string{"task", "read_only_task", "read_only_skill", "parallel_tasks", "fleet", "wait"} {
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
