package control

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/runtime/agent"
)

// mcpManager owns the session's live tool/plugin surface: the MCP plugin Host
// (live server connections), the tool Registry the executor reads each turn, and
// the session-scoped context a hot-added stdio server binds its subprocess to.
// Like approvalManager it holds the live plumbing behind its own lock, off c.mu —
// the Controller keeps the config-facing orchestration (persisting reasonix.toml
// on add/remove, building specs from entries).
//
// mu guards the lazy host creation and host-pointer reads. The registry is
// internally thread-safe (its own RWMutex) and pluginCtx is write-once, so the
// lock is held only briefly — never across the host's network/subprocess I/O.
// host is either injected at construction (the desktop shared-host path) or
// created lazily on the first connect; once set it never reverts to nil.
type mcpManager struct {
	mu        sync.Mutex
	host      *plugin.Host
	reg       *tool.Registry
	pluginCtx context.Context
	// defaultCallTimeout is what a spec gets when it declares none. Read once
	// while building a spec, so it needs no lock.
	defaultCallTimeout time.Duration
	// sealed, once set, refuses every connection. It is written before the
	// controller is handed to a caller and only read after.
	sealed         error
	promptFailures promptFailureDebt
	// notice tells the model what a catalog refresh took away. It is the
	// executor's turn tail, bound once at construction.
	notice func(string)
}

// seal makes the manager refuse to connect or register any server.
func (m *mcpManager) seal(reason error) { m.sealed = reason }

func newMcpManager(host *plugin.Host, reg *tool.Registry, pluginCtx context.Context, defaultCallTimeout time.Duration, notice func(string)) mcpManager {
	watchToolListChanges(host, reg, pluginCtx, notice)
	return mcpManager{host: host, reg: reg, pluginCtx: pluginCtx, defaultCallTimeout: defaultCallTimeout, notice: notice}
}

// watchToolListChanges keeps this session's registry in step with a server that
// announces a changed tool list. The host may be shared between sessions, so
// each one registers the refreshed catalog into its own registry. notice, when
// set, is how the session tells the model what the refresh took away from it.
func watchToolListChanges(host *plugin.Host, reg *tool.Registry, pluginCtx context.Context, notice func(string)) {
	if host == nil || reg == nil {
		return
	}
	host.SubscribeToolListChanges(pluginCtx, func(spec plugin.Spec, tools []tool.Tool) {
		withdrawn := registerRefreshedMCPTools(reg, spec, tools)
		if len(withdrawn) > 0 && notice != nil {
			notice(withdrawnMCPToolsNotice(spec.Name, withdrawn))
		}
	})
}

// registerRefreshedMCPTools swaps one server's tools for the set it now offers,
// in one registry call, and reports the pinned names the catalog no longer
// backs — those stay as withdrawn slots (see withdrawnProviderTools). The
// prefix is not resumed: a session that suspended this server turned it off,
// and a server announcing new tools is not the user asking for it back.
func registerRefreshedMCPTools(reg *tool.Registry, spec plugin.Spec, tools []tool.Tool) []string {
	prefix := plugin.ToolPrefix(spec.Name)
	held, withdrawn := withdrawnProviderTools(reg, spec.Name, prefix, tools)
	reg.ReplacePrefix(prefix, append(slices.Clone(tools), held...))
	return withdrawn
}

// withdrawnProviderTools are the slots the refresh must not vacate: what this
// session pinned into the provider-visible array at boot and the catalog no
// longer offers. Vacating one rewrites the cache-stable prefix, so the entry
// stays and its call fails with the live catalog's not-found. A slot an earlier
// refresh withdrew is kept but not announced twice.
func withdrawnProviderTools(reg *tool.Registry, server, prefix string, offered []tool.Tool) ([]tool.Tool, []string) {
	live := make(map[string]bool, len(offered))
	for _, t := range offered {
		if t != nil {
			live[t.Name()] = true
		}
	}
	raw := map[string]string{}
	for _, b := range reg.MCPBindings() {
		raw[b.CallableName] = b.RawName
	}
	var held []tool.Tool
	var withdrawn []string
	for _, name := range reg.AllNames() {
		if !strings.HasPrefix(name, prefix) || live[name] || !reg.ProviderSurfacePinned(name) {
			continue
		}
		current, ok := reg.Get(name)
		if !ok || current == nil {
			continue
		}
		if tool.IsWithdrawn(current) {
			held = append(held, current)
			continue
		}
		rawName := raw[name]
		if rawName == "" {
			rawName = strings.TrimPrefix(name, prefix)
		}
		held = append(held, tool.Withdraw(current, fmt.Sprintf("MCP tool %q not found on server %q", rawName, server)))
		withdrawn = append(withdrawn, name)
	}
	return held, withdrawn
}

// withdrawnMCPToolsNotice is what the model is told on the turn tail: the tools
// array still shows these calls and will until the session ends, so a model
// reading the array and nothing else would keep spending turns on them.
func withdrawnMCPToolsNotice(server string, names []string) string {
	return fmt.Sprintf("MCP server %q withdrew %s during this session. The definition stays in your tool list so the cached prefix does not move, but calling it now fails with a not-found; use what the server offers instead (use_capability inspect mcp-server:%s lists it).",
		server, strings.Join(names, ", "), server)
}

// hostRef returns the live plugin host (nil until one is injected or lazily
// created), for the SessionAPI Host() accessor and the nil-safe read wrappers.
func (m *mcpManager) hostRef() *plugin.Host {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.host
}

// connectSpec connects (or attaches to an already-connected) MCP server and
// registers its tools, replacing any prior tools under the same prefix. Returns
// the tool count. The host's network/subprocess I/O runs off mu.
func (m *mcpManager) connectSpec(s plugin.Spec) (int, error) {
	if m.sealed != nil {
		return 0, m.sealed
	}
	host, ctx, reg := m.ensureHost()
	plugin.ApplyDisabledMCPPolicy(reg, s)

	tools, err := host.Add(ctx, s)
	if err != nil {
		if !plugin.IsServerAlreadyConnected(err) {
			return 0, err
		}
		toolsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		tools, err = host.ToolsForSpec(toolsCtx, s)
		if err != nil {
			return 0, err
		}
	}
	if reg != nil {
		reg.ResumePrefix(plugin.ToolPrefix(s.Name))
		reg.RemovePrefix(plugin.ToolPrefix(s.Name))
		for _, t := range tools {
			reg.Add(t)
		}
	}
	return len(tools), nil
}

// registerSpecOnDemand restores one enabled server into this session's tool
// registry without starting a disconnected process. A live shared-host client
// is reused immediately; otherwise cached lazy tools (or one connect stub on a
// cache miss) start the server only when the model makes the first real call.
func (m *mcpManager) registerSpecOnDemand(s plugin.Spec) (int, error) {
	if m.sealed != nil {
		return 0, m.sealed
	}
	host, ctx, reg := m.ensureHost()
	plugin.ApplyDisabledMCPPolicy(reg, s)

	var tools []tool.Tool
	if host.HasClient(s.Name) {
		toolsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var err error
		tools, err = host.ToolsForSpec(toolsCtx, s)
		if err != nil {
			return 0, err
		}
	} else {
		cached, _ := plugin.LoadCachedSchemaForSpec(s)
		tools = plugin.LazyToolset(s, cached, host, reg, ctx, false)
	}
	if reg != nil {
		prefix := plugin.ToolPrefix(s.Name)
		reg.ResumePrefix(prefix)
		reg.RemovePrefix(prefix)
		for _, t := range tools {
			reg.Add(t)
		}
	}
	return len(tools), nil
}

// ensureHost returns the live host, creating it on first use. A host created
// here starts watching for catalog changes before any server connects to it.
func (m *mcpManager) ensureHost() (*plugin.Host, context.Context, *tool.Registry) {
	m.mu.Lock()
	created := m.host == nil
	if created {
		m.host = plugin.NewHost()
	}
	host, ctx, reg := m.host, m.pluginCtx, m.reg
	m.mu.Unlock()
	if created {
		watchToolListChanges(host, reg, ctx, m.notice)
	}
	return host, ctx, reg
}

// disconnect drops a live server and its tools from the registry. Reports whether
// a live server was removed.
func (m *mcpManager) disconnect(name string) bool {
	if reg := m.registry(); reg != nil {
		reg.ClearDisabledMCP(name)
	}
	host := m.hostRef()
	if host == nil {
		return false
	}
	prefix, ok := host.Remove(name)
	if ok {
		if reg := m.registry(); reg != nil {
			reg.RemovePrefix(prefix)
		}
	}
	return ok
}

// removeToolPrefix drops a server's tools from the registry without touching the
// host — the placeholder / not-connected path. Returns the number removed.
func (m *mcpManager) removeToolPrefix(name string) int {
	reg := m.registry()
	if reg == nil {
		return 0
	}
	reg.ClearDisabledMCP(name)
	return reg.RemovePrefix(plugin.ToolPrefix(name))
}

// suspendToolPrefix hides a server's tools from this session's registry while a
// shared host keeps the client alive for sibling sessions.
func (m *mcpManager) suspendToolPrefix(name string) bool {
	reg := m.registry()
	if reg == nil {
		return false
	}
	reg.SuspendPrefix(plugin.ToolPrefix(name))
	return true
}

// registerTool adds a built-in tool to the live registry (e.g. the slash-command
// tool rebuilt by ReloadCommands). No-op when no registry is bound.
func (m *mcpManager) registerTool(t tool.Tool) {
	if reg := m.registry(); reg != nil {
		reg.Add(t)
	}
}

// registry returns the shared tool registry under mu (write-once, but read under
// the lock for consistency with the host pointer).
func (m *mcpManager) registry() *tool.Registry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reg
}

// catalogTools counts, per server, the tools that server has in this session's
// registry. A lazily registered server has its whole cached toolset there while
// its process is not running, which is the difference between a server waiting
// to be called and one that is simply not there. One pass, because a caller
// listing every configured server would otherwise walk the registry per row.
func (m *mcpManager) catalogTools() map[string]int {
	reg := m.registry()
	if reg == nil {
		return nil
	}
	out := map[string]int{}
	for _, b := range reg.MCPBindings() {
		out[b.Server]++
	}
	return out
}

// serverNames lists the live server names (nil when no host is connected).
func (m *mcpManager) serverNames() []string {
	if h := m.hostRef(); h != nil {
		return h.ServerNames()
	}
	return nil
}

// hasServer reports whether a server is live.
func (m *mcpManager) hasServer(name string) bool {
	return slices.Contains(m.serverNames(), name)
}

// prompts lists the live MCP prompts (nil when no host is connected).
func (m *mcpManager) prompts() []plugin.Prompt {
	if h := m.hostRef(); h != nil {
		return h.Prompts()
	}
	return nil
}

// failures lists the recorded MCP startup failures (nil when no host).
func (m *mcpManager) failures() []plugin.Failure {
	if h := m.hostRef(); h != nil {
		return h.Failures()
	}
	return nil
}

// readResource reads an MCP resource. Errors when no host is connected.
func (m *mcpManager) readResource(ctx context.Context, server, uri string) (string, error) {
	h := m.hostRef()
	if h == nil {
		return "", fmt.Errorf("no MCP servers connected")
	}
	return h.ReadResource(ctx, server, uri)
}

// hostFactNotice is how a background change reaches the model: the executor's
// turn tail, which an append leaves the cached prefix alone. A session with no
// executor has nowhere to say it, and says nothing.
func hostFactNotice(executor *agent.Agent) func(string) {
	if executor == nil {
		return nil
	}
	return executor.NoteHostFact
}
