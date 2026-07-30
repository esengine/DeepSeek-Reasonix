package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/capability"
	"reasonix/internal/plugin"
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
		if _, err := resolver.ResolveCall(context.Background(), json.RawMessage(`{"action":"inspect","capability_id":"mcp-server:figma"}`)); err == nil || !strings.Contains(err.Error(), "outside this subagent's allowed-tools") {
			t.Fatalf("tool-only allowlist must not widen to server inspection: %v", err)
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
	// Direct mcp__* names convert into a capability allowlist; the model never
	// sees mcp__ schemas on the sub-agent surface.
	converted := SubagentToolRegistry(parent, []string{"mcp__figma__search"})
	if _, ok := converted.Get("mcp__figma__search"); ok {
		t.Fatalf("direct MCP tool must not enter subagent registry: %v", converted.Names())
	}
	proxy, ok := converted.Get("use_capability")
	if !ok {
		t.Fatalf("MCP allowlist should install restricted use_capability: %v", converted.Names())
	}
	resolver, ok := proxy.(tool.CallResolver)
	if !ok {
		t.Fatalf("proxy is not a CallResolver: %T", proxy)
	}
	if _, err := resolver.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:figma/search"}`)); err != nil {
		t.Fatalf("converted mcp__ name should allow capability call: %v", err)
	}
	if _, err := resolver.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:other/delete"}`)); err == nil {
		t.Fatal("converted allowlist must reject other MCP capabilities")
	}
}

func TestSubagentToolRegistryDefaultGetsUnrestrictedProxy(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentCapabilityProxy{subagentRegistryTool{name: "use_capability", readOnly: true}})
	parent.Add(subagentMCPTool{
		subagentRegistryTool: subagentRegistryTool{name: "mcp__gh__search", readOnly: true},
		server:               "gh", raw: "search", serverAuthorized: true,
	})
	parent.Add(subagentRegistryTool{name: "read_file", readOnly: true})

	sub := SubagentToolRegistry(parent, nil)
	if _, ok := sub.Get("mcp__gh__search"); ok {
		t.Fatalf("default subagent registry must strip direct MCP: %v", sub.Names())
	}
	if _, ok := sub.Get("use_capability"); !ok {
		t.Fatalf("default subagent registry must include use_capability: %v", sub.Names())
	}
	if _, ok := sub.Get("read_file"); !ok {
		t.Fatal("default subagent registry should keep read_file")
	}
}

func TestReadOnlySubagentToolRegistryKeepsProxyButNotDirectMCP(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentCapabilityProxy{subagentRegistryTool{name: "use_capability", readOnly: true}})
	parent.Add(subagentMCPTool{
		subagentRegistryTool: subagentRegistryTool{name: "mcp__gh__search", readOnly: true},
		server:               "gh", raw: "search", serverAuthorized: true,
	})
	parent.Add(subagentMCPTool{
		subagentRegistryTool: subagentRegistryTool{name: "mcp__gh__write", readOnly: false},
		server:               "gh", raw: "write", serverAuthorized: true,
	})
	parent.Add(subagentRegistryTool{name: "read_file", readOnly: true})

	sub := ReadOnlySubagentToolRegistry(parent, nil)
	if _, ok := sub.Get("mcp__gh__search"); ok {
		t.Fatalf("read-only registry must not expose direct MCP: %v", sub.Names())
	}
	if _, ok := sub.Get("use_capability"); !ok {
		t.Fatalf("read-only registry must keep use_capability for discovery: %v", sub.Names())
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
	parent.Add(subagentCapabilityProxy{subagentRegistryTool{name: "use_capability", readOnly: true}})
	parent.Add(subagentRegistryTool{name: "read_file", readOnly: true})
	parent.Add(subagentMCPTool{
		subagentRegistryTool: subagentRegistryTool{name: "mcp__srv__read", readOnly: true},
		server:               "srv",
		raw:                  "read",
		serverAuthorized:     true,
	})

	sub := ReadOnlySubagentToolRegistry(parent, nil)
	if _, ok := sub.Get("mcp__srv__read"); ok {
		t.Fatalf("read-only subagent registry must not expose direct MCP schemas; got %v", sub.Names())
	}
	if _, ok := sub.Get("use_capability"); !ok {
		t.Fatalf("read-only subagent registry should expose use_capability for MCP readers; got %v", sub.Names())
	}
	if _, ok := sub.Get("read_file"); !ok {
		t.Fatalf("a trusted read-only tool should remain; got %v", sub.Names())
	}
}

func TestCustomProfileAllowlistRestrictsMCPTools(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(subagentCapabilityProxy{subagentRegistryTool{name: "use_capability", readOnly: true}})
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
	if _, ok := general.Get("use_capability"); ok {
		t.Fatalf("built-in-only allowlist should not install MCP proxy; got %v", general.Names())
	}
	for _, name := range []string{"mcp__chrome__list_pages", "mcp__chrome__new_page", "mcp__other__secret"} {
		if _, ok := general.Get(name); ok {
			t.Fatalf("custom profile should exclude direct MCP %q; got %v", name, general.Names())
		}
	}

	explicit := SubagentToolRegistry(parent, []string{"mcp__chrome__*"})
	if _, ok := explicit.Get("mcp__chrome__list_pages"); ok {
		t.Fatalf("explicit MCP wildcard must not expose direct schemas: %v", explicit.Names())
	}
	proxy, ok := explicit.Get("use_capability")
	if !ok {
		t.Fatalf("explicit MCP wildcard should install restricted proxy; got %v", explicit.Names())
	}
	resolver, ok := proxy.(tool.CallResolver)
	if !ok {
		t.Fatalf("proxy is not CallResolver: %T", proxy)
	}
	if _, err := resolver.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:chrome/list_pages"}`)); err != nil {
		t.Fatalf("wildcard should allow chrome/list_pages: %v", err)
	}
	if _, err := resolver.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:chrome/new_page"}`)); err != nil {
		t.Fatalf("wildcard should allow chrome/new_page on writer-capable subagent: %v", err)
	}
	if _, err := resolver.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:other/secret"}`)); err == nil {
		t.Fatal("wildcard must reject other server capabilities")
	}

	ro := ReadOnlySubagentToolRegistry(parent, []string{"read_file"})
	if _, ok := ro.Get("use_capability"); ok {
		t.Fatalf("read-only built-in-only profile should not install MCP proxy; got %v", ro.Names())
	}

	explicitRO := ReadOnlySubagentToolRegistry(parent, []string{"mcp__chrome__*"})
	if _, ok := explicitRO.Get("mcp__chrome__list_pages"); ok {
		t.Fatalf("read-only MCP wildcard must not expose direct schemas: %v", explicitRO.Names())
	}
	roProxy, ok := explicitRO.Get("use_capability")
	if !ok {
		t.Fatalf("read-only MCP wildcard should install restricted proxy; got %v", explicitRO.Names())
	}
	roResolver, ok := roProxy.(tool.CallResolver)
	if !ok {
		t.Fatalf("read-only proxy is not CallResolver: %T", roProxy)
	}
	// Registry allowlist conversion includes both chrome tools; execution-time
	// ReadOnlyExecution still blocks the writer. The schema surface stays proxy-only.
	if _, err := roResolver.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:chrome/list_pages"}`)); err != nil {
		t.Fatalf("read-only wildcard should allow reader capability resolve: %v", err)
	}
}

func TestMCPToolAvailabilityAcrossGeneralAndReadOnlySubagents(t *testing.T) {
	// Direct mcp__* schemas never enter child registries; MCP is only via
	// use_capability. Presence of the proxy (with parent proxy available) is the
	// zero-config surface for both general and strict read-only children.
	parent := tool.NewRegistry()
	parent.Add(subagentCapabilityProxy{subagentRegistryTool{name: "use_capability", readOnly: true}})
	parent.Add(subagentMCPTool{
		subagentRegistryTool: subagentRegistryTool{name: "mcp__srv__tool", readOnly: true},
		server:               "srv", raw: "tool", serverAuthorized: true,
	})

	general := SubagentToolRegistry(parent, nil)
	if _, ok := general.Get("mcp__srv__tool"); ok {
		t.Fatalf("general subagent must not expose direct MCP: %v", general.Names())
	}
	if _, ok := general.Get("use_capability"); !ok {
		t.Fatalf("general subagent must expose use_capability: %v", general.Names())
	}
	ro := ReadOnlySubagentToolRegistry(parent, nil)
	if _, ok := ro.Get("mcp__srv__tool"); ok {
		t.Fatalf("read-only subagent must not expose direct MCP: %v", ro.Names())
	}
	if _, ok := ro.Get("use_capability"); !ok {
		t.Fatalf("read-only subagent must expose use_capability: %v", ro.Names())
	}
	// FilterReadOnlyRegistry (guardian and similar) still surfaces authorized
	// read-only MCP tools; PlannerToolRegistry strips them for proxy-only.
	if _, ok := FilterReadOnlyRegistry(parent).Get("mcp__srv__tool"); !ok {
		t.Fatalf("FilterReadOnlyRegistry should keep authorized read-only MCP for non-planner surfaces; got %v", FilterReadOnlyRegistry(parent).Names())
	}
	if _, ok := PlannerToolRegistry(parent).Get("mcp__srv__tool"); ok {
		t.Fatalf("PlannerToolRegistry must strip direct MCP: %v", PlannerToolRegistry(parent).Names())
	}
	if _, ok := PlannerToolRegistry(parent).Get("use_capability"); !ok {
		t.Fatalf("PlannerToolRegistry must keep use_capability: %v", PlannerToolRegistry(parent).Names())
	}
}

func TestRestrictedCapabilityProxyDescriptionIsStable(t *testing.T) {
	parent := tool.NewRegistry()
	// Real UseCapabilityTool so description bytes match production.
	proxy := NewUseCapabilityTool(context.Background(), nil, []plugin.Spec{
		{Name: "alpha", Authorized: true},
		{Name: "beta", Authorized: true},
	}, parent, nil, nil, nil)
	parent.Add(proxy)
	parent.Add(subagentMCPTool{
		subagentRegistryTool: subagentRegistryTool{name: "mcp__alpha__search", readOnly: true},
		server:               "alpha", raw: "search", serverAuthorized: true,
	})

	before := SubagentToolRegistry(parent, []string{"mcp__alpha__*"})
	beforeProxy, ok := before.Get("use_capability")
	if !ok {
		t.Fatal("restricted proxy missing")
	}
	beforeDesc := beforeProxy.Description()
	beforeSchema := string(beforeProxy.Schema())

	// Install another MCP tool that expands the same wildcard — description and
	// schema must not change (provider-visible prefix stability).
	parent.Add(subagentMCPTool{
		subagentRegistryTool: subagentRegistryTool{name: "mcp__alpha__list", readOnly: true},
		server:               "alpha", raw: "list", serverAuthorized: true,
	})
	after := SubagentToolRegistry(parent, []string{"mcp__alpha__*"})
	afterProxy, ok := after.Get("use_capability")
	if !ok {
		t.Fatal("restricted proxy missing after MCP install")
	}
	if afterProxy.Description() != beforeDesc {
		t.Fatalf("description changed after MCP install\nbefore=%q\nafter=%q", beforeDesc, afterProxy.Description())
	}
	if string(afterProxy.Schema()) != beforeSchema {
		t.Fatalf("schema changed after MCP install")
	}
	if afterProxy.Name() != "use_capability" || beforeProxy.Name() != "use_capability" {
		t.Fatal("proxy name must stay use_capability")
	}
}

func TestRestrictedCapabilityProxyListFiltersServers(t *testing.T) {
	host := plugin.NewHost()
	defer host.Close()
	proxy := NewUseCapabilityTool(context.Background(), host, []plugin.Spec{
		{Name: "alpha", Authorized: true},
		{Name: "beta", Authorized: true},
		{Name: "secret-db", Authorized: true},
	}, tool.NewRegistry(), nil, nil, nil)
	parent := tool.NewRegistry()
	parent.Add(proxy)
	parent.Add(subagentMCPTool{
		subagentRegistryTool: subagentRegistryTool{name: "mcp__alpha__search", readOnly: true},
		server:               "alpha", raw: "search", serverAuthorized: true,
	})

	sub := SubagentToolRegistry(parent, []string{"mcp__alpha__search"})
	tl, ok := sub.Get("use_capability")
	if !ok {
		t.Fatal("missing restricted proxy")
	}
	resolver, ok := tl.(tool.CallResolver)
	if !ok {
		t.Fatalf("not CallResolver: %T", tl)
	}
	rc, err := resolver.ResolveCall(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rc.Result, `"name": "alpha"`) {
		t.Fatalf("list should include allowlisted server alpha:\n%s", rc.Result)
	}
	if strings.Contains(rc.Result, "secret-db") || strings.Contains(rc.Result, `"name": "beta"`) {
		t.Fatalf("list leaked servers outside allowlist:\n%s", rc.Result)
	}
}

func TestMalformedCapabilityAllowlistDoesNotInstallProxy(t *testing.T) {
	parent := tool.NewRegistry()
	parent.Add(NewUseCapabilityTool(context.Background(), nil, []plugin.Spec{
		{Name: "alpha", Authorized: true},
		{Name: "secret-db", Authorized: true},
	}, parent, nil, nil, nil))
	parent.Add(subagentRegistryTool{name: "read_file", readOnly: true})

	// Incomplete IDs must not create a restricted proxy that fail-opens list.
	for _, allow := range [][]string{
		{"mcp-server:"},
		{"mcp-tool:"},
		{"mcp-tool:onlyserver"},
		{"mcp-server:/bad"},
	} {
		sub := SubagentToolRegistry(parent, allow)
		if _, ok := sub.Get("use_capability"); ok {
			t.Fatalf("malformed allowlist %v must not install use_capability; got %v", allow, sub.Names())
		}
	}
}

func TestFilterCapabilityListResultFailClosed(t *testing.T) {
	// Empty server set must not return the raw full inventory.
	full := `{"servers":[{"name":"secret-db","capability_id":"mcp-server:secret-db","status":"configured","authorized":true,"connected":false}],"note":"all"}`
	out := filterCapabilityListResult(full, nil)
	if strings.Contains(out, "secret-db") {
		t.Fatalf("empty servers must fail closed:\n%s", out)
	}
	if !strings.Contains(out, `"servers": []`) && !strings.Contains(out, `"servers":[]`) {
		t.Fatalf("expected empty servers array:\n%s", out)
	}

	// Malformed JSON must not pass through raw text that might contain names.
	leaky := `not-json but mentions secret-db and production`
	out = filterCapabilityListResult(leaky, map[string]bool{"alpha": true})
	if strings.Contains(out, "secret-db") || strings.Contains(out, "not-json") {
		t.Fatalf("malformed payload must fail closed:\n%s", out)
	}
	if !strings.Contains(out, `"servers"`) {
		t.Fatalf("fail-closed payload should still be JSON list shape:\n%s", out)
	}
}

func TestRestrictedListWithEmptyServersMapFailClosed(t *testing.T) {
	// Direct unit path: restricted proxy with empty servers still filters list.
	inner := NewUseCapabilityTool(context.Background(), nil, []plugin.Spec{
		{Name: "secret-db", Authorized: true},
	}, tool.NewRegistry(), nil, nil, nil)
	proxy := &restrictedCapabilityProxy{
		Tool:     inner,
		resolver: inner,
		allowed:  map[string]bool{"mcp-tool:incomplete": true}, // invalid shape should never happen after validation
		servers:  map[string]bool{},
	}
	rc, err := proxy.ResolveCall(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rc.Result, "secret-db") {
		t.Fatalf("empty servers map must not leak inventory:\n%s", rc.Result)
	}
}

func TestPlannerToolRegistryClonesUseCapability(t *testing.T) {
	parent := tool.NewRegistry()
	ledger := capability.NewLedger()
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, parent, ledger, nil, nil)
	parent.Add(proxy)
	parent.Add(subagentRegistryTool{name: "read_file", readOnly: true})

	planner := PlannerToolRegistry(parent)
	got, ok := planner.Get("use_capability")
	if !ok {
		t.Fatal("planner missing use_capability")
	}
	uc, ok := got.(*UseCapabilityTool)
	if !ok {
		t.Fatalf("planner proxy type = %T, want *UseCapabilityTool", got)
	}
	if uc == proxy {
		t.Fatal("planner must not share the executor UseCapabilityTool pointer")
	}
	if uc.ledger == ledger {
		t.Fatal("planner frontend must not share the executor capability ledger")
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
