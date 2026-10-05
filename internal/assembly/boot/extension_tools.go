package boot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension/sidecar"
	"reasonix/internal/ext/pluginpkg"
)

// ExtensionToolPrefix marks a tool a plugin runtime serves, the way mcp__
// marks one an MCP server does.
const ExtensionToolPrefix = "ext__"

// extensionToolTimeout bounds one call; a tool doing longer work should
// report progress through its own surfaces and return.
const extensionToolTimeout = 5 * time.Minute

// extensionTool is a tool a plugin runtime serves. Like an MCP tool it is not
// in the provider-visible list: the model finds it through the capability
// catalog, so installing or reloading a plugin never moves the cached prefix.
type extensionTool struct {
	name     string
	pluginID string
	decl     pluginpkg.RuntimeTool
	clients  *sidecar.Manager
}

func (t extensionTool) Name() string            { return t.name }
func (t extensionTool) Description() string     { return t.decl.Description }
func (t extensionTool) Schema() json.RawMessage { return t.decl.InputSchema }
func (t extensionTool) ReadOnly() bool          { return t.decl.ReadOnly }

func (t extensionTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	client := t.clients.Client(t.pluginID)
	if client == nil || client.Exited() {
		return "", fmt.Errorf("the %s extension is not running; reload extensions or check it under Settings → Plugins", t.pluginID)
	}
	res, err := client.CallTool(ctx, t.decl.Name, args, extensionToolTimeout)
	if err != nil {
		return "", fmt.Errorf("the %s extension could not run %s: %w", t.pluginID, t.decl.Name, err)
	}
	if res.IsError {
		return "", errors.New(res.Content)
	}
	return res.Content, nil
}

// extensionToolName qualifies a runtime's tool with its plugin, so two plugins
// can each serve a "search" and neither can take a built-in's name.
func extensionToolName(pluginID, toolName string) string {
	return ExtensionToolPrefix + strings.NewReplacer(".", "_").Replace(pluginID) + "__" + toolName
}

// addExtensionTools registers the tools each running runtime declared in its
// manifest and activated at its handshake. A runtime that failed to start
// contributes none, so the catalog never lists a tool nothing can run.
func addExtensionTools(reg *tool.Registry, mgr *sidecar.Manager, warn func(string)) {
	if mgr == nil {
		return
	}
	for _, client := range mgr.Clients() {
		for _, decl := range client.DeclaredTools() {
			if !client.ServesTool(decl.Name) {
				continue
			}
			name := extensionToolName(client.PluginID(), decl.Name)
			if len(name) > 64 {
				warn(fmt.Sprintf("extension %s: tool %s is not offered: %s is over the 64 characters a model tool name may have", client.PluginID(), decl.Name, name))
				continue
			}
			reg.Add(extensionTool{name: name, pluginID: client.PluginID(), decl: decl, clients: mgr})
		}
	}
}
