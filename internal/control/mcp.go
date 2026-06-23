package control

import (
	"context"
	"fmt"
	"sync"
	"time"

	"reasonix/internal/plugin"
	"reasonix/internal/tool"
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
}

func newMcpManager(host *plugin.Host, reg *tool.Registry, pluginCtx context.Context) mcpManager {
	return mcpManager{host: host, reg: reg, pluginCtx: pluginCtx}
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
	m.mu.Lock()
	if m.host == nil {
		m.host = plugin.NewHost()
	}
	host, ctx, reg := m.host, m.pluginCtx, m.reg
	m.mu.Unlock()

	tools, err := host.Add(ctx, s)
	if err != nil {
		if !plugin.IsServerAlreadyConnected(err) {
			return 0, err
		}
		toolsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		tools, err = host.ToolsFor(toolsCtx, s.Name)
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

// disconnect drops a live server and its tools from the registry. Reports whether
// a live server was removed.
func (m *mcpManager) disconnect(name string) bool {
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

// serverNames lists the live server names (nil when no host is connected).
func (m *mcpManager) serverNames() []string {
	if h := m.hostRef(); h != nil {
		return h.ServerNames()
	}
	return nil
}

// hasServer reports whether a server is live.
func (m *mcpManager) hasServer(name string) bool {
	for _, n := range m.serverNames() {
		if n == name {
			return true
		}
	}
	return false
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
