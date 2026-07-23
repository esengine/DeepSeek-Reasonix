package plugin

import (
	"context"
	"fmt"
	"strings"
)

// MCPInstallInput is the high-level install request for market entries, pasted
// commands/URLs/JSON, and explicit imports. Old AddMCPServer/UpdateMCPServer
// remain as compatibility adapters around the same readiness path.
type MCPInstallInput struct {
	RegistryName string            `json:"registryName,omitempty"`
	Definition   string            `json:"definition,omitempty"`
	Name         string            `json:"name,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	// Spec is the already-parsed candidate used by in-process callers that
	// resolved a market entry or argv themselves.
	Spec Spec `json:"-"`
}

// MCPInstallResult is the user-visible outcome of an install/update transaction.
// Only State=="ready" may be phrased as success in CLI/UI copy.
type MCPInstallResult struct {
	Name      string `json:"name"`
	State     string `json:"state"` // ready | action_required | issue
	ToolCount int    `json:"toolCount"`
	Action    string `json:"action"` // none | authenticate | authorize | retry
	Message   string `json:"message"`
}

// InstallAndConnect runs initialize + tools/list for a candidate Spec against
// the shared Host and returns a readiness result. Callers own durable config,
// activation, launcher lock, and schema persistence around this probe so a
// failed readiness check does not leave a half-installed entry.
func ReadyInstallResult(name string, toolCount int) MCPInstallResult {
	return MCPInstallResult{
		Name:      strings.TrimSpace(name),
		State:     "ready",
		ToolCount: toolCount,
		Action:    "none",
		Message:   fmt.Sprintf("%q is available with %d tools", strings.TrimSpace(name), toolCount),
	}
}

// InstallResultForError maps a handshake error to the stable UI/CLI install
// contract. Expected authentication/authorization states are still returned to
// callers as errors so low-level users can decide whether durable config should
// be retained; high-level transaction owners may turn those states into a
// successful RPC response after persistence succeeds.
func InstallResultForError(name string, err error) MCPInstallResult {
	name = strings.TrimSpace(name)
	if err == nil {
		return ReadyInstallResult(name, 0)
	}
	if requiresLaunchApproval(err) {
		return MCPInstallResult{Name: name, State: "action_required", Action: "authorize", Message: err.Error()}
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "auth") || strings.Contains(lower, "oauth") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden") {
		return MCPInstallResult{Name: name, State: "action_required", Action: "authenticate", Message: msg}
	}
	return MCPInstallResult{Name: name, State: "issue", Action: "retry", Message: msg}
}

func (h *Host) InstallAndConnect(ctx context.Context, spec Spec) (MCPInstallResult, error) {
	return h.InstallAndConnectWithLifecycle(ctx, ctx, spec)
}

// InstallAndConnectWithLifecycle separates ownership of the spawned MCP process
// from the readiness-call deadline. Desktop sessions pass their session
// lifecycle as lifeCtx and a short request deadline as callCtx; one-shot CLI
// probes may use the same context for both.
func (h *Host) InstallAndConnectWithLifecycle(lifeCtx, callCtx context.Context, spec Spec) (MCPInstallResult, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return MCPInstallResult{State: "issue", Action: "retry", Message: "MCP server name is required"}, fmt.Errorf("mcp install: name is required")
	}
	if spec.ProcessMode == "" {
		spec.ProcessMode = MCPProcessHost
	}
	// User/host installs are authorized by the install action itself.
	if !spec.RequireLaunchApproval {
		spec.Authorized = true
	}
	tools, err := h.EnsureConnectedWithLifecycle(lifeCtx, callCtx, spec, 0)
	if err != nil {
		return InstallResultForError(name, err), err
	}
	// Persist schema so the next session registers tools without reconnect UX.
	_ = SaveCachedSchema(spec.Name, CachedSchema{
		CacheKey:     SchemaCacheKey(spec),
		Capabilities: map[string]bool{"tools": len(tools) > 0},
		Tools:        cacheableToolsOf(tools),
	})
	return ReadyInstallResult(name, len(tools)), nil
}
