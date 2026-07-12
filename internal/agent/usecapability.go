package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"reasonix/internal/capability"
	"reasonix/internal/event"
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

// UseCapabilityTool is the Delivery-only stable proxy for inspecting, calling,
// or declining catalog capabilities. Call never adds dynamic MCP tools to the
// main registry — subsequent calls keep using this stable schema.
type UseCapabilityTool struct {
	mu sync.Mutex

	host *plugin.Host
	// lifeCtx is the session-scoped context that owns on-demand MCP child
	// processes (mirrors lazySpawn.ctx): a proxied server must outlive the tool
	// call that started it and die with the session, not with a resolve-phase
	// timeout. nil falls back to context.Background() for direct/test use.
	lifeCtx context.Context
	// specs are the boot-converted plugin specs (env expansion, workspace
	// overrides, timeouts, trusted read-only tools). The proxy never rebuilds
	// specs from raw config entries — that would fork the conversion logic.
	specs    []plugin.Spec
	registry *tool.Registry // live registry for already-exposed MCP tools
	ledger   *capability.Ledger
	audit    *capability.Audit
	catalog  func() capability.Catalog
	// proxyClients holds on-demand connected clients that must not pollute the
	// provider-visible registry. Tools are looked up via host.ToolsFor.
	connected map[string]bool
	// liveTools snapshots raw tool metadata of proxy-connected servers so the
	// capability catalog keeps concrete mcp-tool entries after a server turns
	// ready — the tools deliberately never enter the provider-visible registry.
	liveTools map[string][]plugin.CachedTool
}

// NewUseCapabilityTool builds the Delivery-only capability proxy. lifeCtx is
// the session-scoped context that owns on-demand MCP subprocess lifetimes;
// specs must be the same boot-converted specs used for auto-started servers.
func NewUseCapabilityTool(lifeCtx context.Context, host *plugin.Host, specs []plugin.Spec, reg *tool.Registry, ledger *capability.Ledger, audit *capability.Audit, catalog func() capability.Catalog) *UseCapabilityTool {
	return &UseCapabilityTool{
		host:      host,
		lifeCtx:   lifeCtx,
		specs:     append([]plugin.Spec(nil), specs...),
		registry:  reg,
		ledger:    ledger,
		audit:     audit,
		catalog:   catalog,
		connected: map[string]bool{},
	}
}

func (*UseCapabilityTool) Name() string { return "use_capability" }

func (*UseCapabilityTool) Description() string {
	return "Delivery profile capability proxy: inspect Skill/MCP metadata, call MCP tools (including auto_start=false servers) without changing the provider tool schema, or decline a prefer capability with a non-empty reason. Skills still use run_skill; this tool only proxies MCP calls."
}

func (*UseCapabilityTool) ReadOnly() bool { return true }

func (*UseCapabilityTool) Schema() json.RawMessage {
	// Stable schema — must not change across turns or when MCP connects.
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"action":{"type":"string","description":"inspect | call | decline"},
			"capability_id":{"type":"string","description":"Capability id such as skill:review, mcp-server:github, or mcp-tool:github/search_issues"},
			"arguments":{"type":"object","description":"Raw MCP tool arguments for action=call"},
			"reason":{"type":"string","description":"Required non-empty reason when action=decline"}
		},
		"required":["action","capability_id"]
	}`)
}

// ResolveCall implements tool.CallResolver so the agent can run permission,
// hooks, and evidence against the real MCP target before execution.
func (t *UseCapabilityTool) ResolveCall(ctx context.Context, args json.RawMessage) (tool.ResolvedCall, error) {
	var p struct {
		Action       string          `json:"action"`
		CapabilityID string          `json:"capability_id"`
		Arguments    json.RawMessage `json:"arguments"`
		Reason       string          `json:"reason"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return tool.ResolvedCall{}, fmt.Errorf("invalid args: %w", err)
	}
	action := strings.ToLower(strings.TrimSpace(p.Action))
	id := strings.TrimSpace(p.CapabilityID)
	if id == "" {
		return tool.ResolvedCall{}, fmt.Errorf("capability_id is required")
	}
	base := tool.ResolvedCall{
		DisplayName:  "use_capability",
		ProxyAction:  action,
		CapabilityID: id,
		Args:         p.Arguments,
	}
	switch action {
	case "inspect":
		out, err := t.inspect(ctx, id)
		if err != nil {
			if t.audit != nil {
				t.audit.RecordMCPProxy(true, false, true)
			}
			return tool.ResolvedCall{}, err
		}
		if t.audit != nil {
			t.audit.RecordMCPProxy(true, false, false)
		}
		base.SkipExecute = true
		base.Result = out
		base.ReadOnly = true
		return base, nil
	case "decline":
		reason := strings.TrimSpace(p.Reason)
		if reason == "" {
			return tool.ResolvedCall{}, fmt.Errorf("reason is required for action=decline")
		}
		// Decline must not skip require.
		if t.ledger != nil {
			if e, ok := t.ledger.Get(id); ok && e.Policy == capability.AutoUseRequire {
				return tool.ResolvedCall{}, fmt.Errorf("cannot decline a require capability %q", id)
			}
			if err := t.ledger.MarkDeclined(id, reason); err != nil {
				return tool.ResolvedCall{}, err
			}
		}
		if t.audit != nil {
			t.audit.RecordDecline()
		}
		base.SkipExecute = true
		base.Result = fmt.Sprintf("declined capability %s: %s", id, reason)
		base.ReadOnly = true
		return base, nil
	case "call":
		return t.resolveCall(ctx, id, p.Arguments, base)
	default:
		return tool.ResolvedCall{}, fmt.Errorf("unknown action %q; use inspect, call, or decline", p.Action)
	}
}

func (t *UseCapabilityTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	resolved, err := t.ResolveCall(ctx, args)
	if err != nil {
		return "", err
	}
	if resolved.SkipExecute {
		if resolved.ProxyAction == "call" && !resolved.Unavailable {
			if t.ledger != nil {
				t.ledger.MarkSucceeded(resolved.CapabilityID)
			}
			if t.audit != nil {
				t.audit.RecordMCPProxy(false, true, false)
			}
		}
		return resolved.Result, nil
	}
	if resolved.Unavailable {
		if t.ledger != nil {
			t.ledger.MarkUnavailable(resolved.CapabilityID, resolved.UnavailableReason)
		}
		return "", fmt.Errorf("capability unavailable: %s", resolved.UnavailableReason)
	}
	if resolved.Target == nil {
		return "", fmt.Errorf("no target tool resolved for %s", resolved.CapabilityID)
	}
	if t.ledger != nil {
		t.ledger.MarkInvoked(resolved.CapabilityID)
	}
	if t.audit != nil {
		t.audit.RecordMCPProxy(false, true, false)
	}
	out, err := resolved.Target.Execute(ctx, resolved.Args)
	if err != nil {
		if t.ledger != nil {
			t.ledger.MarkFailed(resolved.CapabilityID, err.Error())
		}
		if t.audit != nil {
			t.audit.RecordMCPProxy(false, true, true)
		}
		return out, err
	}
	if t.ledger != nil {
		t.ledger.MarkSucceeded(resolved.CapabilityID)
	}
	return out, nil
}

func (t *UseCapabilityTool) inspect(ctx context.Context, id string) (string, error) {
	cat := t.currentCatalog()
	if e, ok := cat.Lookup(id); ok {
		b, _ := json.MarshalIndent(map[string]any{
			"id":          e.ID,
			"kind":        e.Kind,
			"name":        e.Name,
			"description": e.Description,
			"status":      e.Status,
			"read_only":   e.ReadOnly,
			"auto_use":    e.AutoUse,
			"requires":    e.Requires,
			"profiles":    e.Profiles,
			"tool_name":   e.ToolName,
			"auto_start":  e.AutoStart,
		}, "", "  ")
		// For MCP entries, list tools without side effects: live tools when the
		// server is already connected, cached schema otherwise. Inspect runs
		// during call resolution — before permission and hook gates — so it must
		// never start a subprocess or open a network connection.
		if e.Kind == capability.KindMCPServer || e.Kind == capability.KindMCPTool {
			server := e.Source
			if server == "" {
				server = e.ConnectName
			}
			if server != "" {
				if t.host != nil && t.host.HasClient(server) {
					// serverTools refreshes the snapshot too: inspecting a
					// server another tab connected restores tool routing here.
					tools, err := t.serverTools(ctx, server)
					if err != nil {
						return string(b) + "\n\nTool listing failed: " + err.Error(), nil
					}
					return string(b) + "\n\nTools:\n" + inspectToolListJSON(server, tools), nil
				}
				if spec, ok := t.specFor(server); ok {
					if cs, ok := plugin.LoadCachedSchema(server, plugin.SpecFingerprint(spec)); ok && len(cs.Tools) > 0 {
						var list []inspectToolInfo
						for _, ct := range cs.Tools {
							list = append(list, inspectToolInfo{
								ID:          "mcp-tool:" + server + "/" + ct.Name,
								Name:        plugin.ModelToolName(server, ct.Name),
								Description: ct.Description,
								ReadOnly:    ct.ReadOnly,
								Schema:      ct.Schema,
							})
						}
						extra, _ := json.MarshalIndent(list, "", "  ")
						return string(b) + "\n\nTools (from cached schema; server not started):\n" + string(extra), nil
					}
					return string(b) + "\n\nServer not connected and no cached tool schema; call use_capability(action=\"call\", capability_id=\"mcp-server:" + server + "\") to connect (after approval) and list its tools.", nil
				}
			}
		}
		return string(b), nil
	}
	return "", fmt.Errorf("unknown capability_id %q", id)
}

type inspectToolInfo struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	ReadOnly    bool            `json:"read_only"`
	Schema      json.RawMessage `json:"input_schema,omitempty"`
}

// inspectToolListJSON renders a server's live tools as the capability-id
// directory shared by inspect and the first-discovery connect result.
func inspectToolListJSON(server string, tools []tool.Tool) string {
	var list []inspectToolInfo
	for _, tl := range tools {
		raw := ""
		if m, ok := tl.(tool.MCPMetadata); ok {
			raw = m.MCPRawToolName()
		}
		list = append(list, inspectToolInfo{
			ID:          "mcp-tool:" + server + "/" + raw,
			Name:        tl.Name(),
			Description: tl.Description(),
			ReadOnly:    tl.ReadOnly(),
			Schema:      tl.Schema(),
		})
	}
	extra, _ := json.MarshalIndent(list, "", "  ")
	return string(extra)
}

func (t *UseCapabilityTool) resolveCall(ctx context.Context, id string, args json.RawMessage, base tool.ResolvedCall) (tool.ResolvedCall, error) {
	// Server-level call is the first-discovery path for servers with no
	// schema cache: it resolves to a gated connect-and-list target so the
	// model can learn tool names without inspect ever starting a process.
	if server, ok := parseMCPServerCapabilityID(id); ok {
		return t.resolveServerConnect(ctx, server, base)
	}
	server, raw, err := parseMCPCapabilityID(id)
	if err != nil {
		// Skills must use run_skill.
		if strings.HasPrefix(id, "skill:") {
			return tool.ResolvedCall{}, fmt.Errorf("call only proxies MCP tools; use run_skill for %s", id)
		}
		return tool.ResolvedCall{}, err
	}
	// Prefer already-exposed registry tool (auto-started MCP). The model name
	// MUST come from the plugin layer's canonical constructor: it appends a
	// collision hash for sanitised raw names, and permission/hook rules are
	// written against that executed name — a proxy-local normalization would
	// let them silently miss.
	modelName := plugin.ModelToolName(server, raw)
	if t.registry != nil {
		if tl, ok := t.registry.Get(modelName); ok {
			base.TargetName = modelName
			base.Target = tl
			base.ReadOnly = tl.ReadOnly()
			if len(args) == 0 {
				base.Args = json.RawMessage(`{}`)
			} else {
				base.Args = args
			}
			return base, nil
		}
	}
	// Server already connected (auto-started, a previous proxy call, or a
	// sibling tab sharing this host): resolving against live tools is
	// side-effect-free. serverTools also refreshes the catalog snapshot so a
	// cross-tab connect still yields routable mcp-tool entries here.
	if t.host != nil && t.host.HasClient(server) {
		tools, err := t.serverTools(ctx, server)
		if err != nil {
			return t.resolveUnavailable(base, id, modelName, err.Error()), nil
		}
		target := findMCPTool(tools, raw, modelName)
		if target == nil {
			return t.resolveUnavailable(base, id, modelName, fmt.Sprintf("MCP tool %q not found on server %q", raw, server)), nil
		}
		base.Target = target
		base.TargetName = target.Name()
		base.ReadOnly = target.ReadOnly()
		if len(args) == 0 {
			base.Args = json.RawMessage(`{}`)
		} else {
			base.Args = args
		}
		return base, nil
	}
	// Unconnected server: resolution must stay pure — no subprocess, no network.
	// Return a deferred target that connects in Execute, after the permission
	// gate and PreToolUse hooks have approved the real target name/arguments.
	spec, ok := t.specFor(server)
	if !ok {
		return t.resolveUnavailable(base, id, modelName, fmt.Sprintf("MCP server %q is not configured", server)), nil
	}
	lazy := &onDemandMCPTool{proxy: t, spec: spec, server: server, raw: raw, modelName: modelName}
	base.Target = lazy
	base.TargetName = modelName
	// Conservative: an unstarted tool counts as a writer unless the user
	// explicitly trusted it read-only in config (spec-level trust, the same
	// source live remote tools honor).
	base.ReadOnly = lazy.ReadOnly()
	if len(args) == 0 {
		base.Args = json.RawMessage(`{}`)
	} else {
		base.Args = args
	}
	return base, nil
}

// resolveUnavailable fills the host-proven unavailable shape shared by the
// side-effect-free resolution failures (missing config, unknown tool).
func (t *UseCapabilityTool) resolveUnavailable(base tool.ResolvedCall, id, modelName, reason string) tool.ResolvedCall {
	base.Unavailable = true
	base.UnavailableReason = reason
	base.SkipExecute = true
	base.Result = "capability unavailable: " + reason
	base.TargetName = modelName
	base.ReadOnly = false
	if t.ledger != nil {
		t.ledger.MarkUnavailable(id, reason)
	}
	if t.audit != nil {
		t.audit.RecordMCPProxy(false, true, true)
	}
	return base
}

// findMCPTool matches a server's tool list by raw MCP name or by the
// canonical namespaced model-visible name (plugin.ModelToolName).
func findMCPTool(tools []tool.Tool, raw, modelName string) tool.Tool {
	for _, tl := range tools {
		if m, ok := tl.(tool.MCPMetadata); ok && m.MCPRawToolName() == raw {
			return tl
		}
		if tl.Name() == modelName {
			return tl
		}
	}
	return nil
}

// onDemandMCPTool defers MCP server startup to Execute so permission and hook
// gates always run before any subprocess or network side effect. It reports
// itself read-only only under config-level trust; everything else is treated
// as a writer until the live handshake proves otherwise, so plan mode and
// permission prompts see the conservative classification.
type onDemandMCPTool struct {
	proxy     *UseCapabilityTool
	spec      plugin.Spec
	server    string
	raw       string
	modelName string
}

func (o *onDemandMCPTool) Name() string { return o.modelName }

func (o *onDemandMCPTool) Description() string {
	return "on-demand MCP tool " + o.server + "/" + o.raw + " (connects after approval)"
}

func (o *onDemandMCPTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (o *onDemandMCPTool) ReadOnly() bool {
	return o.spec.ReadOnlyToolNames[o.raw] || o.spec.ReadOnlyModelToolNames[o.modelName]
}

// PlanModeUntrustedReadOnly is false because ReadOnly() is true only under an
// explicit user config assertion (trusted_read_only_tools), never a server
// hint — there is no server yet.
func (o *onDemandMCPTool) PlanModeUntrustedReadOnly() bool { return false }

// MCPServerName/MCPRawToolName expose the deferred target for audit and
// plan-mode trust prompts (tool.MCPMetadata).
func (o *onDemandMCPTool) MCPServerName() string  { return o.server }
func (o *onDemandMCPTool) MCPRawToolName() string { return o.raw }

func (o *onDemandMCPTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	tools, err := o.proxy.ensureServerTools(ctx, o.server)
	if err != nil {
		// Audit for the call path is recorded once by the agent loop
		// (noteCapabilityInvocation); only the ledger outcome lands here.
		if o.proxy.ledger != nil {
			o.proxy.ledger.MarkUnavailable("mcp-tool:"+o.server+"/"+o.raw, err.Error())
		}
		return "", err
	}
	target := findMCPTool(tools, o.raw, o.modelName)
	if target == nil {
		msg := fmt.Sprintf("MCP tool %q not found on server %q", o.raw, o.server)
		if o.proxy.ledger != nil {
			o.proxy.ledger.MarkUnavailable("mcp-tool:"+o.server+"/"+o.raw, msg)
		}
		return "", fmt.Errorf("%s", msg)
	}
	return target.Execute(ctx, args)
}

func (t *UseCapabilityTool) ensureServerTools(ctx context.Context, server string) ([]tool.Tool, error) {
	server = strings.TrimSpace(server)
	if server == "" {
		return nil, fmt.Errorf("empty MCP server name")
	}
	if t.host == nil {
		return nil, fmt.Errorf("MCP host unavailable")
	}
	// Reuse shared host if already connected (including auto-started).
	if t.host.HasClient(server) {
		return t.serverTools(ctx, server)
	}
	spec, ok := t.specFor(server)
	if !ok {
		return nil, fmt.Errorf("MCP server %q is not configured", server)
	}
	// On-demand connect: the handshake gets a short budget, but the child
	// process lifetime belongs to the session-scoped lifeCtx — canceling the
	// handshake context after connect must not kill a stdio server the tool
	// call is about to use. Tools stay off the main registry.
	life := t.lifeCtx
	if life == nil {
		life = context.Background()
	}
	handshakeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tools, err := t.host.AddWithLifecycle(life, handshakeCtx, spec)
	if err != nil {
		if plugin.IsServerAlreadyConnected(err) {
			return t.serverTools(ctx, server)
		}
		t.host.RecordFailure(spec, err)
		return nil, fmt.Errorf("connect %q: %w", server, err)
	}
	t.mu.Lock()
	t.connected[server] = true
	t.mu.Unlock()
	// Intentionally do NOT add tools to t.registry — Delivery schema stays stable.
	_ = tools
	return t.serverTools(ctx, server)
}

// serverTools fetches the live tools for a connected server and refreshes the
// proxy's catalog snapshot so mcp-tool entries stay routable once the server
// is StatusReady (its tools are absent from the provider-visible registry).
func (t *UseCapabilityTool) serverTools(ctx context.Context, server string) ([]tool.Tool, error) {
	tools, err := t.host.ToolsFor(ctx, server)
	if err != nil {
		return nil, err
	}
	snap := make([]plugin.CachedTool, 0, len(tools))
	for _, tl := range tools {
		m, ok := tl.(tool.MCPMetadata)
		if !ok || m.MCPRawToolName() == "" {
			continue
		}
		snap = append(snap, plugin.CachedTool{
			Name:        m.MCPRawToolName(),
			Description: tl.Description(),
			Schema:      tl.Schema(),
			ReadOnly:    tl.ReadOnly(),
		})
	}
	t.mu.Lock()
	if t.liveTools == nil {
		t.liveTools = map[string][]plugin.CachedTool{}
	}
	t.liveTools[server] = snap
	t.mu.Unlock()
	return tools, nil
}

// ConnectedProxyTools returns raw tool metadata for servers connected through
// the proxy, keyed by server name. Catalog builders consume it so concrete
// mcp-tool capabilities survive an on-demand connect without ever touching
// the provider-visible registry.
func (t *UseCapabilityTool) ConnectedProxyTools() map[string][]plugin.CachedTool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.liveTools) == 0 {
		return nil
	}
	out := make(map[string][]plugin.CachedTool, len(t.liveTools))
	for k, v := range t.liveTools {
		out[k] = append([]plugin.CachedTool(nil), v...)
	}
	return out
}

// specFor looks up the boot-converted spec for server. The proxy deliberately
// holds []plugin.Spec, not raw config entries: env expansion, workspace
// overrides, call timeouts, and trusted read-only tool names all live in the
// shared conversion and must not be re-derived here.
func (t *UseCapabilityTool) specFor(server string) (plugin.Spec, bool) {
	for _, s := range t.specs {
		if s.Name == server {
			return s, true
		}
	}
	return plugin.Spec{}, false
}

func (t *UseCapabilityTool) currentCatalog() capability.Catalog {
	if t.catalog != nil {
		return t.catalog()
	}
	return capability.Catalog{}
}

// parseMCPServerCapabilityID extracts the server name from an mcp-server id.
func parseMCPServerCapabilityID(id string) (string, bool) {
	if !strings.HasPrefix(id, "mcp-server:") {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimPrefix(id, "mcp-server:"))
	return name, name != ""
}

// resolveServerConnect resolves action=call on an mcp-server id. A connected
// server lists its tools immediately (side-effect free); an unconnected one
// resolves to a deferred connect target that runs only after the permission
// gate and PreToolUse hooks approve it.
func (t *UseCapabilityTool) resolveServerConnect(ctx context.Context, server string, base tool.ResolvedCall) (tool.ResolvedCall, error) {
	id := "mcp-server:" + server
	if t.host != nil && t.host.HasClient(server) {
		out, err := t.listServerTools(ctx, server)
		if err != nil {
			return t.resolveUnavailable(base, id, plugin.ToolPrefix(server), err.Error()), nil
		}
		base.SkipExecute = true
		base.Result = out
		base.ReadOnly = true
		return base, nil
	}
	spec, ok := t.specFor(server)
	if !ok {
		return t.resolveUnavailable(base, id, plugin.ToolPrefix(server), fmt.Sprintf("MCP server %q is not configured", server)), nil
	}
	connect := &onDemandMCPConnect{proxy: t, spec: spec, server: server}
	base.Target = connect
	// A dedicated exact identity names the connect for permission and hook
	// rules. It cannot collide with a real mcp__ tool, and rules do not need to
	// rely on unsupported tool-name glob matching.
	base.TargetName = connect.Name()
	// Connecting spawns a subprocess — never a read-only fast path; plan mode
	// blocks it and Ask-style gates prompt before any process starts.
	base.ReadOnly = false
	base.Args = json.RawMessage(`{}`)
	return base, nil
}

// onDemandMCPConnect is the deferred first-discovery target: it connects the
// server post-approval and returns the live tool directory.
type onDemandMCPConnect struct {
	proxy  *UseCapabilityTool
	spec   plugin.Spec
	server string
}

func (o *onDemandMCPConnect) Name() string { return plugin.MCPConnectPermissionName(o.server) }

func (o *onDemandMCPConnect) Description() string {
	return "connect MCP server " + o.server + " on demand and list its tools"
}

func (o *onDemandMCPConnect) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (o *onDemandMCPConnect) ReadOnly() bool { return false }

func (o *onDemandMCPConnect) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	if _, err := o.proxy.ensureServerTools(ctx, o.server); err != nil {
		if o.proxy.ledger != nil {
			o.proxy.ledger.MarkUnavailable("mcp-server:"+o.server, err.Error())
		}
		return "", err
	}
	return o.proxy.listServerTools(ctx, o.server)
}

// listServerTools renders the live tool directory of a connected server and
// refreshes the proxy snapshot on the way (via serverTools).
func (t *UseCapabilityTool) listServerTools(ctx context.Context, server string) (string, error) {
	tools, err := t.serverTools(ctx, server)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("connected MCP server %q; %d tools:\n%s", server, len(tools), inspectToolListJSON(server, tools)), nil
}

func parseMCPCapabilityID(id string) (server, raw string, err error) {
	id = strings.TrimSpace(id)
	switch {
	case strings.HasPrefix(id, "mcp-tool:"):
		rest := strings.TrimPrefix(id, "mcp-tool:")
		server, raw, ok := strings.Cut(rest, "/")
		if !ok || server == "" || raw == "" {
			return "", "", fmt.Errorf("invalid mcp-tool id %q; want mcp-tool:<server>/<tool>", id)
		}
		return server, raw, nil
	case strings.HasPrefix(id, "mcp-server:"):
		return "", "", fmt.Errorf("%q is a server id; call it directly to connect and list tools, or use mcp-tool:<server>/<tool>", id)
	default:
		return "", "", fmt.Errorf("action=call requires an mcp-tool capability id, got %q", id)
	}
}

// Ensure UseCapabilityTool satisfies the tool contracts used by the agent.
var (
	_ tool.Tool         = (*UseCapabilityTool)(nil)
	_ tool.CallResolver = (*UseCapabilityTool)(nil)
)

// EmitProxyAudit is a helper for frontends: returns a notice describing the
// proxy name and real target for user audit trails.
func EmitProxyAudit(sink event.Sink, resolved tool.ResolvedCall) {
	if sink == nil || resolved.TargetName == "" {
		return
	}
	sink.Emit(event.Event{
		Kind:   event.Notice,
		Level:  event.LevelInfo,
		Text:   fmt.Sprintf("capability proxy: %s → %s", resolved.DisplayName, resolved.TargetName),
		Detail: resolved.CapabilityID,
	})
}
