package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reasonix/internal/runtime/usecap"
	"strings"

	"reasonix/internal/contract/tool"
)

// read_skill is deliberately not listed: it renders playbook text inline and
// cannot recurse, so depth-capped sub-agents keep it and can still read
// playbooks even when they can no longer delegate.
var subagentRecursiveTools = []string{
	"task",
	"read_only_task",
	"run_skill",
	"read_only_skill",
	"explore",
	"locate",
	"research",
	"review",
	"security_review",
}

var subagentAlwaysHiddenTools = []string{
	"parallel_tasks",
	"fleet",
	"read_subagent_result",
	"list_subagents",
	"install_skill",
	"install_source",
	"apply_isolated",
	"discard_isolated",
}

var subagentJobTools = []string{
	"wait",
	"bash_output",
	"kill_shell",
}

var readOnlySubagentWorkflowTools = []string{
	"connect_tool_source",
}

// SubagentMetaTools returns the tool names that spawned agents should not inherit
// from the parent registry unless a future call site deliberately opts into a
// different boundary. They can spawn or author more agent work, so excluding them
// preserves one layer of delegation without adding a spawn-count cap.
// read_skill stays listed here so the guardian and planner surfaces, which
// exclude these names, keep their provider-visible tool sets byte-identical —
// only the sub-agent depth cap deliberately stopped stripping it.
func SubagentMetaTools() []string {
	out := append([]string(nil), subagentRecursiveTools...)
	out = append(out, "read_skill")
	out = append(out, subagentAlwaysHiddenTools...)
	return out
}

// SubagentToolRegistry returns the tool set exposed inside spawned sub-agents:
// the requested whitelist (or every parent tool), minus meta tools that would
// spawn more agent work and job tools whose runtime manager is not injected into
// sub-agents. When bash is present, it is wrapped to advertise and allow only
// foreground execution.
func SubagentToolRegistry(parent *tool.Registry, names []string) *tool.Registry {
	return SubagentToolRegistryForDepth(parent, names, 1, 1)
}

// SubagentToolRegistryForDepth returns the writer-capable tool set for a spawned
// subagent at childDepth. Recursive delegation tools are available only when the
// child still has room to spawn one more subagent.
//
// Direct mcp__* schemas are never exposed: MCP goes only through the fixed
// use_capability proxy so connect/disconnect/tool-list churn cannot change the
// child provider-visible tool prefix. With no explicit allowlist the child gets
// the full proxy (installed/authorized MCP, including tools without
// readOnlyHint). An explicit allowlist converts mcp__* / mcp-tool: names into a
// capability-id allowlist on a restricted proxy.
func SubagentToolRegistryForDepth(parent *tool.Registry, names []string, childDepth, maxDepth int) *tool.Registry {
	return SubagentToolRegistryForDepthWithRuntime(parent, names, childDepth, maxDepth, nil)
}

// SubagentToolRegistryForDepthWithRuntime is SubagentToolRegistryForDepth with
// an optional session MCP runtime used when the parent registry has no
// use_capability (for example Economy or legacy callers) but sub-agents still
// need the proxy.
func SubagentToolRegistryForDepthWithRuntime(parent *tool.Registry, names []string, childDepth, maxDepth int, runtime *usecap.MCPCapabilityRuntime) *tool.Registry {
	exclude := append([]string(nil), subagentAlwaysHiddenTools...)
	if childDepth >= NormalizeMaxSubagentDepth(maxDepth) {
		exclude = append(exclude, subagentRecursiveTools...)
	}
	exclude = append(exclude, subagentJobTools...)
	sub := FilterRegistry(parent, names, exclude...)
	stripDirectMCPTools(sub)
	AttachCompleteSubtaskTool(sub)
	attachSubagentCapabilityProxy(parent, sub, names, runtime)
	if bash, ok := sub.Get("bash"); ok {
		sub.Add(foregroundOnlyBash{inner: bash})
	}
	return sub
}

type foregroundOnlyBash struct {
	inner tool.Tool
}

// FilterRegistry builds a sub-registry from parent: the named whitelist (empty =
// every parent tool), minus any excluded names. Used to scope what a spawned
// sub-agent — a `task` sub-agent or a subagent skill — may call, e.g. excluding
// `task` to bar recursive nesting, or restricting to a skill's allowed-tools.
// Direct MCP tools may be copied here; callers that need a stable MCP surface
// should strip them and attach use_capability via attachSubagentCapabilityProxy.
func FilterRegistry(parent *tool.Registry, names []string, exclude ...string) *tool.Registry {
	sub := tool.NewRegistry()
	if parent == nil {
		return sub
	}
	sub.CopyDisabledMCPFrom(parent)
	ex := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		ex[e] = true
	}
	customAllowlist := len(names) > 0
	src := names
	if !customAllowlist {
		src = parent.Names()
	} else {
		src = ExpandToolPatterns(parent, src)
	}
	for _, name := range src {
		if ex[name] {
			continue
		}
		// MCP never enters through the generic filter when named as capability
		// ids; model-visible mcp__* may still be listed for conversion later.
		if strings.HasPrefix(name, "mcp-tool:") || strings.HasPrefix(name, "mcp-server:") {
			continue
		}
		tl, ok := parent.Get(name)
		if !ok {
			continue
		}
		sub.Add(tl)
	}
	return sub
}

// stripDirectMCPTools removes provider-visible mcp__* tools so sub-agents use
// only the stable use_capability proxy for MCP.
func stripDirectMCPTools(reg *tool.Registry) {
	if reg == nil {
		return
	}
	for _, name := range append([]string(nil), reg.Names()...) {
		if strings.HasPrefix(name, tool.MCPNamePrefix) {
			reg.RemovePrefix(name)
		}
	}
}

// restrictedCapabilityProxy preserves a subagent allowed-tools boundary when
// MCP is available only through use_capability. The pseudo mcp-tool: and
// mcp-server: entries never become provider tools; they select one proxy schema
// whose resolver rejects every capability outside the exact allowlist.
//
// Provider-visible name/description/schema stay identical to the unrestricted
// proxy so allowlist expansion never changes the child cache prefix. Allowlist
// enforcement is host-local (check + filtered list results).
type restrictedCapabilityProxy struct {
	tool.Tool
	resolver tool.CallResolver
	allowed  map[string]bool
	// servers is the set of MCP server names implied by allowed IDs; list
	// results are filtered to this set so profile isolation covers discovery.
	servers map[string]bool
}

// validMCPServerCapabilityID accepts mcp-server:<non-empty-name> only.
func validMCPServerCapabilityID(id string) (server string, ok bool) {
	if !strings.HasPrefix(id, "mcp-server:") {
		return "", false
	}
	server = strings.TrimSpace(strings.TrimPrefix(id, "mcp-server:"))
	// Reject empty and path-like fragments that are not bare server names.
	return server, server != "" && !strings.Contains(server, "/")
}

// validMCPToolCapabilityID accepts mcp-tool:<server>/<tool> with both parts non-empty.
func validMCPToolCapabilityID(id string) (server, raw string, ok bool) {
	if !strings.HasPrefix(id, "mcp-tool:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(id, "mcp-tool:")
	server, raw, cut := strings.Cut(rest, "/")
	server = strings.TrimSpace(server)
	raw = strings.TrimSpace(raw)
	return server, raw, cut && server != "" && raw != ""
}

func serversFromCapabilityAllowlist(allowed map[string]bool) map[string]bool {
	servers := map[string]bool{}
	for id := range allowed {
		id = strings.TrimSpace(id)
		if server, ok := validMCPServerCapabilityID(id); ok {
			servers[server] = true
			continue
		}
		if server, _, ok := validMCPToolCapabilityID(id); ok {
			servers[server] = true
		}
	}
	return servers
}

// attachSubagentCapabilityProxy installs a per-agent use_capability frontend.
// Any parent-copied proxy is replaced so children never share Executor ledger
// state. No allowlist → full proxy. Explicit allowlist with MCP names →
// restricted proxy. Explicit "use_capability" → full proxy. Explicit allowlist
// without MCP entries → no proxy.
func attachSubagentCapabilityProxy(parent, sub *tool.Registry, names []string, runtime *usecap.MCPCapabilityRuntime) {
	if sub == nil {
		return
	}
	// Drop any provider-copied use_capability so we always install an isolated
	// frontend (shared Host/runtime, independent ledger/audit).
	if _, ok := sub.Get("use_capability"); ok {
		sub.RemovePrefix("use_capability")
	}
	frontend := newSubagentCapabilityFrontend(parent, runtime)
	if frontend == nil {
		return
	}
	if len(names) == 0 || allowlistRequestsUnrestrictedProxy(names) {
		sub.Add(frontend)
		return
	}
	allowed := mcpCapabilityAllowlist(parent, names)
	if len(allowed) == 0 {
		// Custom allowlist with no valid MCP entries: do not expose the proxy.
		return
	}
	servers := serversFromCapabilityAllowlist(allowed)
	if len(servers) == 0 {
		// Incomplete capability IDs produced an empty server set: fail closed
		// rather than installing a restricted proxy that would list everything.
		return
	}
	resolver, ok := frontend.(tool.CallResolver)
	if !ok {
		return
	}
	sub.Add(&restrictedCapabilityProxy{
		Tool:     frontend,
		resolver: resolver,
		allowed:  allowed,
		servers:  servers,
	})
}

func newSubagentCapabilityFrontend(parent *tool.Registry, runtime *usecap.MCPCapabilityRuntime) tool.Tool {
	if runtime != nil {
		return runtime.NewFrontend(nil, nil)
	}
	if parent == nil {
		return nil
	}
	inner, ok := parent.Get("use_capability")
	if !ok {
		return nil
	}
	if uc, ok := inner.(*usecap.UseCapabilityTool); ok {
		return uc.CloneForAgent(nil, nil)
	}
	return inner
}

// mcpCapabilityAllowlist converts profile/call tool names into capability IDs
// for the restricted use_capability proxy. Accepts complete mcp-tool:<s>/<t>,
// mcp-server:<s>, model-visible mcp__* names, and wildcards expanded against
// the parent. Incomplete prefixes such as "mcp-server:" or "mcp-tool:foo" are
// rejected so they cannot install a restricted proxy with an empty server set.
func mcpCapabilityAllowlist(parent *tool.Registry, names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	expanded := names
	if parent != nil {
		expanded = ExpandToolPatterns(parent, names)
	}
	allowed := map[string]bool{}
	for _, name := range expanded {
		name = strings.TrimSpace(name)
		switch {
		case name == "use_capability":
			// Explicit proxy grant is handled as a full frontend by the caller
			// when this is the only MCP-related entry; leave empty here so a
			// bare use_capability allowlist entry still installs unrestricted.
			continue
		case strings.HasPrefix(name, "mcp-server:"):
			if server, ok := validMCPServerCapabilityID(name); ok {
				allowed["mcp-server:"+server] = true
			}
		case strings.HasPrefix(name, "mcp-tool:"):
			if server, raw, ok := validMCPToolCapabilityID(name); ok {
				allowed["mcp-tool:"+server+"/"+raw] = true
			}
		default:
			if parent != nil {
				if tl, ok := parent.Get(name); ok {
					if m, ok := tl.(tool.MCPMetadata); ok {
						server := strings.TrimSpace(m.MCPServerName())
						raw := strings.TrimSpace(m.MCPRawToolName())
						if server != "" && raw != "" {
							allowed["mcp-tool:"+server+"/"+raw] = true
							continue
						}
					}
				}
			}
			if server, raw, ok := tool.SplitMCPName(name); ok {
				allowed["mcp-tool:"+server+"/"+raw] = true
			}
		}
	}
	return allowed
}

func allowlistRequestsUnrestrictedProxy(names []string) bool {
	for _, name := range names {
		if strings.TrimSpace(name) == "use_capability" {
			return true
		}
	}
	return false
}

// ReadOnlySubagentToolRegistry returns the tool set exposed to read-only
// sub-agents: read-only research tools plus a bash wrapper that enforces the
// permission-layer read-only command policy at execution time. Workflow/meta tools are
// excluded even when their Tool.ReadOnly contract is true.
func ReadOnlySubagentToolRegistry(parent *tool.Registry, names []string) *tool.Registry {
	return ReadOnlySubagentToolRegistryForDepth(parent, names, 1, 1)
}

// ReadOnlySubagentToolRegistryForDepth returns the tool set exposed to read-only
// subagents. It permits only read-only delegation tools while another depth
// layer is available. Direct mcp__* schemas are never exposed; MCP goes only
// through use_capability. Dynamic execution still requires authorized server +
// readOnlyHint + non-destructive (enforced by ReadOnlyExecution), so strict
// agents share the stable proxy schema and connection reuse without permission
// relaxation.
//
// Custom profile/call allowlists remain authoritative and convert MCP names
// into a capability-id allowlist on a restricted proxy.
func ReadOnlySubagentToolRegistryForDepth(parent *tool.Registry, names []string, childDepth, maxDepth int) *tool.Registry {
	return ReadOnlySubagentToolRegistryForDepthWithRuntime(parent, names, childDepth, maxDepth, nil)
}

// ReadOnlySubagentToolRegistryForDepthWithRuntime is the read-only registry
// builder with an optional session MCP runtime for proxy injection.
func ReadOnlySubagentToolRegistryForDepthWithRuntime(parent *tool.Registry, names []string, childDepth, maxDepth int, runtime *usecap.MCPCapabilityRuntime) *tool.Registry {
	exclude := append([]string(nil), subagentAlwaysHiddenTools...)
	if childDepth >= NormalizeMaxSubagentDepth(maxDepth) {
		exclude = append(exclude, subagentRecursiveTools...)
	} else {
		exclude = append(exclude, "task", "run_skill", "explore", "locate", "research", "review", "security_review")
	}
	exclude = append(exclude, subagentJobTools...)
	exclude = append(exclude, plannerNonResearchTools...)
	exclude = append(exclude, readOnlySubagentWorkflowTools...)
	ex := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		ex[e] = true
	}
	sub := tool.NewRegistry()
	if parent == nil {
		return sub
	}
	sub.CopyDisabledMCPFrom(parent)
	src := names
	if len(src) == 0 {
		src = parent.Names()
	} else {
		src = ExpandToolPatterns(parent, src)
	}
	for _, name := range src {
		if ex[name] {
			continue
		}
		if strings.HasPrefix(name, "mcp-tool:") || strings.HasPrefix(name, "mcp-server:") {
			continue
		}
		tl, ok := parent.Get(name)
		if !ok {
			continue
		}
		if name == "bash" {
			sub.Add(readOnlyBash{inner: tl})
			continue
		}
		// Direct MCP never enters the strict registry — use_capability only.
		if isInstalledMCPTool(tl) || strings.HasPrefix(name, tool.MCPNamePrefix) {
			continue
		}
		if !tl.ReadOnly() {
			continue
		}
		sub.Add(tl)
	}
	attachSubagentCapabilityProxy(parent, sub, names, runtime)
	return sub
}

// ExpandToolPatterns resolves explicit wildcard allowlist entries from imported
// agent profiles against the current registry. Expansion is deterministic and
// session-local, so optional MCP tools only enter a child after connection.
func ExpandToolPatterns(parent *tool.Registry, names []string) []string {
	if parent == nil {
		return nil
	}
	available := parent.Names()
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if !strings.ContainsAny(name, "*?[") {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
			continue
		}
		for _, candidate := range available {
			matched, err := filepath.Match(name, candidate)
			if err == nil && matched && !seen[candidate] {
				seen[candidate] = true
				out = append(out, candidate)
			}
		}
	}
	return out
}

// FilterReadOnlyRegistry builds a sub-registry containing only tools whose
// ReadOnly contract is true, minus explicit exclusions. MCP tools must
// additionally come from an authorized server and must not carry
// destructiveHint.
func FilterReadOnlyRegistry(parent *tool.Registry, exclude ...string) *tool.Registry {
	ex := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		ex[e] = true
	}
	sub := tool.NewRegistry()
	if parent == nil {
		return sub
	}
	sub.CopyDisabledMCPFrom(parent)
	for _, name := range parent.Names() {
		if ex[name] {
			continue
		}
		tl, ok := parent.Get(name)
		if !ok || !tl.ReadOnly() {
			continue
		}
		if isInstalledMCPTool(tl) && (!tool.IsMCPServerAuthorized(tl) || tool.HasMCPDestructiveHint(tl)) {
			continue
		}
		sub.Add(tl)
	}
	return sub
}

func (b foregroundOnlyBash) Name() string { return "bash" }

func (b foregroundOnlyBash) Description() string {
	desc := strings.TrimSpace(b.inner.Description())
	if desc == "" {
		desc = "Execute a command in the shell and return combined stdout/stderr."
	}
	desc = strings.Replace(desc, "Execute a command in the shell", "Execute a foreground command in the shell", 1)
	return desc + " Background execution is unavailable inside subagents."
}

func (b foregroundOnlyBash) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		RunInBackground bool `json:"run_in_background"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.RunInBackground {
		return "", tool.Blocked("blocked: background bash is unavailable in subagents; run a foreground command or ask the parent agent to start a background job")
	}
	return b.inner.Execute(ctx, args)
}

func (b foregroundOnlyBash) ReadOnly() bool { return b.inner.ReadOnly() }

// Description is fixed: never embed dynamic capability IDs (they change with
// MCP install/tool-list and would break the stable provider tool prefix).
func (t *restrictedCapabilityProxy) Description() string {
	return t.Tool.Description()
}

func (t *restrictedCapabilityProxy) check(args json.RawMessage) error {
	var p struct {
		Action       string `json:"action"`
		CapabilityID string `json:"capability_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return fmt.Errorf("invalid args: %w", err)
	}
	action := strings.ToLower(strings.TrimSpace(p.Action))
	if usecap.IsIDlessCatalogAction(action) {
		return nil
	}
	id := strings.TrimSpace(p.CapabilityID)
	if id == "" {
		return fmt.Errorf("capability_id is required")
	}
	if !t.allowed[id] {
		return fmt.Errorf("capability %q is outside this subagent's allowed-tools", id)
	}
	return nil
}

func (t *restrictedCapabilityProxy) ResolveCall(ctx context.Context, args json.RawMessage) (tool.ResolvedCall, error) {
	if err := t.check(args); err != nil {
		return tool.ResolvedCall{}, err
	}
	rc, err := t.resolver.ResolveCall(ctx, args)
	if err != nil {
		return rc, err
	}
	var p struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(args, &p)
	action := strings.ToLower(strings.TrimSpace(p.Action))
	if action == usecap.ActionList && rc.SkipExecute {
		rc.Result = usecap.FilterCapabilityListResult(rc.Result, t.servers)
	} else if action == usecap.ActionSearch && rc.SkipExecute {
		rc.Result = usecap.FilterCapabilitySearchResult(rc.Result, t.allowed)
	}
	return rc, nil
}

func (t *restrictedCapabilityProxy) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if err := t.check(args); err != nil {
		return "", err
	}
	out, err := t.Tool.Execute(ctx, args)
	if err != nil {
		return out, err
	}
	var p struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(args, &p)
	action := strings.ToLower(strings.TrimSpace(p.Action))
	switch action {
	case usecap.ActionList:
		return usecap.FilterCapabilityListResult(out, t.servers), nil
	case usecap.ActionSearch:
		return usecap.FilterCapabilitySearchResult(out, t.allowed), nil
	}
	return out, nil
}

func (foregroundOnlyBash) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute in the foreground"}},"required":["command"]}`)
}
