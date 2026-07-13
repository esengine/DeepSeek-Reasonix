package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/planmode"
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

const (
	TokenModeFull     = "full"
	TokenModeEconomy  = "economy"
	TokenModeDelivery = "delivery"
)

const tokenEconomyPrompt = `Token economy mode is on. Keep the default tool surface lean. Optional sources are hidden behind connect_tool_source; enable skills, read_only_skill, MCP servers, LSP, web_fetch, install_source, task, or read_only_task only when the current request actually needs them.`

const tokenDeliveryPrompt = `<delivery-profile>
Prioritize a verified, complete result over minimizing model calls or tokens.
For action requests: establish acceptance criteria; reproduce bugs when practical;
inspect the relevant code and project rules; fix the root cause; run focused
verification; review the resulting diff and adjacent behavior; and continue until
the request is complete or a genuine blocker remains. Do not claim success without
evidence. State any unverified result or assumption explicitly.
</delivery-profile>`

var tokenEconomyCoreBuiltins = []string{
	"bash",
	"bash_output",
	"code_index",
	"complete_step",
	"edit_file",
	"glob",
	"grep",
	"kill_shell",
	"ls",
	"move_file",
	"multi_edit",
	"read_file",
	"todo_write",
	"wait",
	"write_file",
}

func NormalizeTokenMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case TokenModeEconomy, "eco", "save", "saving", "low", "lite", "minimal":
		return TokenModeEconomy
	case TokenModeDelivery, "deliver", "quality", "performance":
		return TokenModeDelivery
	default:
		return TokenModeFull
	}
}

func tokenEconomyBuiltins(configured []string) []string {
	if len(configured) == 0 {
		return append([]string(nil), tokenEconomyCoreBuiltins...)
	}
	core := map[string]bool{}
	for _, name := range tokenEconomyCoreBuiltins {
		core[name] = true
	}
	out := make([]string, 0, len(configured))
	seen := map[string]bool{}
	for _, name := range configured {
		name = strings.TrimSpace(name)
		if name == "" || !core[name] || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

type toolSourceConnector struct {
	mu sync.Mutex

	skills        func(context.Context) (string, error)
	readOnlySkill func(context.Context) (string, error)
	task          func(context.Context) (string, error)
	readOnlyTask  func(context.Context) (string, error)
	install       func(context.Context) (string, error)
	webFetch      func(context.Context) (string, error)
	lsp           func(context.Context) (string, error)
	mcp           func(context.Context, string) (string, error)
	mcpNames      []string

	planModeAllowedTools     []string
	planModeTrustedMCPServer map[string]bool
}

func (*toolSourceConnector) Name() string { return "connect_tool_source" }

func (*toolSourceConnector) Description() string {
	return "Token economy mode only: enable an optional tool source when the task needs it. Sources: skills, read_only_skill, mcp, lsp, web_fetch, install_source, task, read_only_task. For mcp, pass the configured server name; omit name to list servers. Newly enabled tools are available on the next model request."
}

func (*toolSourceConnector) ReadOnly() bool { return true }

func (*toolSourceConnector) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"source":{"type":"string","description":"Tool source to enable: skills, read_only_skill, mcp, lsp, web_fetch, install_source, task, or read_only_task."},
			"name":{"type":"string","description":"For source=mcp, the configured server name. Omit to list configured MCP servers without connecting them."}
		},
		"required":["source"]
	}`)
}

func (t *toolSourceConnector) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Source string `json:"source"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	source := normalizeToolSource(p.Source)
	if source == "" {
		return "", fmt.Errorf("unknown tool source %q; available: %s", p.Source, strings.Join(t.availableSources(), ", "))
	}
	name := strings.TrimSpace(p.Name)

	out, mcpConnect, err := t.executeLocked(ctx, source, name, p.Source)
	if mcpConnect == nil {
		return out, err
	}
	// Connecting an MCP server spawns its subprocess and blocks until the
	// handshake finishes (seconds, or until ctx expires), so it runs outside
	// t.mu: concurrent connect_tool_source calls for fast sources must not
	// queue behind it. No re-locking is needed afterwards: the callback itself
	// merges the server's tools into the registry (which has its own lock),
	// and Execute keeps no per-server state. Concurrent connects racing on the
	// same server are deduplicated inside the callback via the plugin host
	// (ErrServerAlreadyConnected / ErrSpawningInFlight fall back to the
	// already-connected server's tools), so the loser still idempotently
	// reports the enabled tools instead of failing.
	return mcpConnect(ctx, name)
}

// executeLocked dispatches a connect_tool_source call under t.mu. Fast sources
// (registry-only mutations) run to completion while the lock is held. For an
// MCP connect with a server name it performs only the quick pre-checks
// (plan-mode gate, callback availability) and returns the connect callback as
// mcpConnect; the caller invokes it after releasing t.mu. When mcpConnect is
// nil, out/err are the final result.
func (t *toolSourceConnector) executeLocked(ctx context.Context, source, name, rawSource string) (out string, mcpConnect func(context.Context, string) (string, error), err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if blocked, msg := t.planModeSourceBlocked(ctx, source, name); blocked {
		return msg, nil, nil
	}

	switch source {
	case "skills":
		out, err = runSourceInstaller(ctx, "skills", t.skills)
	case "read_only_skill":
		out, err = runSourceInstaller(ctx, "read_only_skill", t.readOnlySkill)
	case "task":
		out, err = runSourceInstaller(ctx, "task", t.task)
	case "read_only_task":
		out, err = runSourceInstaller(ctx, "read_only_task", t.readOnlyTask)
	case "install_source":
		out, err = runSourceInstaller(ctx, "install_source", t.install)
	case "web_fetch":
		out, err = runSourceInstaller(ctx, "web_fetch", t.webFetch)
	case "lsp":
		out, err = runSourceInstaller(ctx, "lsp", t.lsp)
	case "mcp":
		if name == "" {
			if len(t.mcpNames) == 0 {
				return "No configured MCP servers are available in this session.", nil, nil
			}
			names := append([]string(nil), t.mcpNames...)
			sort.Strings(names)
			return "Configured MCP servers: " + strings.Join(names, ", ") + ". Call connect_tool_source again with source=\"mcp\" and name set to connect one server.", nil, nil
		}
		if t.mcp == nil {
			return "", nil, fmt.Errorf("MCP source is unavailable in this session")
		}
		return "", t.mcp, nil
	default:
		return "", nil, fmt.Errorf("unknown tool source %q", rawSource)
	}
	return out, nil, err
}

func (t *toolSourceConnector) planModeSourceBlocked(ctx context.Context, source, name string) (bool, string) {
	if !agent.PlanModeFromContext(ctx) {
		return false, ""
	}
	if source == "mcp" {
		if name == "" || planModeAllowsMCPServer(t.planModeAllowedTools, name) || t.planModeTrustedMCPServer[name] {
			return false, ""
		}
		return true, fmt.Sprintf("blocked: MCP source %q is not available in plan mode until at least one concrete tool is trusted. Connect it outside plan mode, choose always allow from the read-only trust prompt, pre-seed trusted_read_only_tools, or declare a concrete %q tool in plan_mode_allowed_tools. Keep exploring with read-only tools, then write your plan for approval before using this MCP server.", name, plugin.ToolPrefix(name))
	}
	// Sources are read-only iff they expose only read-only research surfaces; the
	// moderate plan-mode gate then trusts that ReadOnly flag (step 6), while any
	// other source stays non-read-only and is fail-closed by the policy.
	readOnlySource := source == "web_fetch" || source == "lsp" || source == "read_only_task" || source == "read_only_skill"
	decision := planmode.Policy{}.Decide(planmode.Call{Name: source, ReadOnly: readOnlySource})
	return decision.Blocked, decision.Message
}

func planModeAllowsMCPServer(allowedTools []string, server string) bool {
	prefix := plugin.ToolPrefix(server)
	for _, name := range allowedTools {
		name = strings.TrimSpace(name)
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			return true
		}
	}
	return false
}

func normalizeToolSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "skill", "skills":
		return "skills"
	case "read_only_skill", "readonly_skill", "read-only-skill", "read_only_skills", "readonly_skills", "read-only-skills":
		return "read_only_skill"
	case "mcp", "plugin", "plugins", "server", "servers":
		return "mcp"
	case "lsp", "language_server", "language-servers":
		return "lsp"
	case "web", "web_fetch", "webfetch", "fetch":
		return "web_fetch"
	case "install", "install_source", "installer":
		return "install_source"
	case "read_only_task", "readonly_task", "read-only-task", "read_only_subagent", "readonly_subagent", "read-only-subagent", "research_task", "research-subagent":
		return "read_only_task"
	case "task", "subagent", "subagents":
		return "task"
	default:
		return ""
	}
}

func (t *toolSourceConnector) availableSources() []string {
	var out []string
	if t.skills != nil {
		out = append(out, "skills")
	}
	if t.readOnlySkill != nil {
		out = append(out, "read_only_skill")
	}
	if t.mcp != nil || len(t.mcpNames) > 0 {
		out = append(out, "mcp")
	}
	if t.lsp != nil {
		out = append(out, "lsp")
	}
	if t.webFetch != nil {
		out = append(out, "web_fetch")
	}
	if t.install != nil {
		out = append(out, "install_source")
	}
	if t.task != nil {
		out = append(out, "task")
	}
	if t.readOnlyTask != nil {
		out = append(out, "read_only_task")
	}
	sort.Strings(out)
	return out
}

func runSourceInstaller(ctx context.Context, name string, fn func(context.Context) (string, error)) (string, error) {
	if fn == nil {
		return "", fmt.Errorf("%s source is unavailable in this session", name)
	}
	return fn(ctx)
}

func addTools(reg *tool.Registry, tools []tool.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		reg.Add(t)
		names = append(names, t.Name())
	}
	sort.Strings(names)
	return names
}
