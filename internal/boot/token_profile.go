package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/tool"
)

const (
	TokenModeFull    = "full"
	TokenModeEconomy = "economy"
)

const tokenEconomyPrompt = `Token economy mode is on. Keep the default tool surface lean. Optional sources are hidden behind connect_tool_source; enable skills, MCP servers, CodeGraph, LSP, web_fetch, install_source, or task only when the current request actually needs them.`

var tokenEconomyCoreBuiltins = []string{
	"bash",
	"bash_output",
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

	skills    func(context.Context) (string, error)
	task      func(context.Context) (string, error)
	install   func(context.Context) (string, error)
	webFetch  func(context.Context) (string, error)
	lsp       func(context.Context) (string, error)
	codegraph func(context.Context) (string, error)
	mcp       func(context.Context, string) (string, error)
	mcpNames  []string
}

func (*toolSourceConnector) Name() string { return "connect_tool_source" }

func (*toolSourceConnector) Description() string {
	return "Token economy mode only: enable an optional tool source when the task needs it. Sources: skills, mcp, codegraph, lsp, web_fetch, install_source, task. For mcp, pass the configured server name; omit name to list servers. Newly enabled tools are available on the next model request."
}

func (*toolSourceConnector) ReadOnly() bool { return true }

func (*toolSourceConnector) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"source":{"type":"string","description":"Tool source to enable: skills, mcp, codegraph, lsp, web_fetch, install_source, or task."},
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

	t.mu.Lock()
	defer t.mu.Unlock()

	switch source {
	case "skills":
		return runSourceInstaller(ctx, "skills", t.skills)
	case "task":
		if agent.PlanModeFromContext(ctx) {
			return "task is unavailable in plan mode because it exposes a writer-capable sub-agent tool.", nil
		}
		return runSourceInstaller(ctx, "task", t.task)
	case "install_source":
		if agent.PlanModeFromContext(ctx) {
			return "install_source is unavailable in plan mode because it can install or remove tools.", nil
		}
		return runSourceInstaller(ctx, "install_source", t.install)
	case "web_fetch":
		return runSourceInstaller(ctx, "web_fetch", t.webFetch)
	case "lsp":
		return runSourceInstaller(ctx, "lsp", t.lsp)
	case "codegraph":
		return runSourceInstaller(ctx, "codegraph", t.codegraph)
	case "mcp":
		name := strings.TrimSpace(p.Name)
		if name == "" {
			if len(t.mcpNames) == 0 {
				return "No configured MCP servers are available in this session.", nil
			}
			names := append([]string(nil), t.mcpNames...)
			sort.Strings(names)
			return "Configured MCP servers: " + strings.Join(names, ", ") + ". Call connect_tool_source again with source=\"mcp\" and name set to connect one server.", nil
		}
		if t.mcp == nil {
			return "", fmt.Errorf("MCP source is unavailable in this session")
		}
		return t.mcp(ctx, name)
	default:
		return "", fmt.Errorf("unknown tool source %q", p.Source)
	}
}

func normalizeToolSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "skill", "skills":
		return "skills"
	case "mcp", "plugin", "plugins", "server", "servers":
		return "mcp"
	case "codegraph", "code_graph", "symbol", "symbols":
		return "codegraph"
	case "lsp", "language_server", "language-servers":
		return "lsp"
	case "web", "web_fetch", "webfetch", "fetch":
		return "web_fetch"
	case "install", "install_source", "installer":
		return "install_source"
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
	if t.mcp != nil || len(t.mcpNames) > 0 {
		out = append(out, "mcp")
	}
	if t.codegraph != nil {
		out = append(out, "codegraph")
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
