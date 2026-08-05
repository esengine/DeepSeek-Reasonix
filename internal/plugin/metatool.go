// run_mcp meta-tool: a single dispatcher that replaces every per-tool
// "mcp__<server>__<tool>" top-level entry.
//
// Motivation. Each MCP tool registered as a top-level entry inflates the
// provider's tools array; with 15+ servers the surface reaches 50-100+ entries
// and dilutes model attention across dozens of schemas. run_mcp collapses that
// surface to a single tool: the model discovers the (server_name, tool_name)
// pairs from Description()'s dynamically-stitched mapping and calls run_mcp to
// dispatch. The provider's tools array stays at builtins + 1 regardless of how
// many MCP servers are configured.
//
// Description contract (locked by meta_capacity_test.go's
// TestMetaToolDescriptionListsServerToolMapping): server_name is quoted (Go %q),
// tool_names are comma-separated, the call format is spelled out, and the two
// arguments are explicitly marked non-swappable — the visual distinction is what
// stops the model from passing a tool_name where server_name belongs.
//
// The mapping is rebuilt on every Description() call from live Host clients
// first, then from on-disk cached schemas for specs whose background handshake
// has not completed yet. Sorting by server_name then tool_name keeps the bytes
// stable across calls when the surface hasn't changed, so the provider prefix
// cache stays warm.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"reasonix/internal/tool"
)

// MetaToolName is the model-visible name of the run_mcp dispatcher.
const MetaToolName = "run_mcp"

// metaToolSchema is the fixed parameter schema for run_mcp. The model passes
// server_name + tool_name + args; the dispatcher forwards args verbatim to the
// named server's tools/call. tool-specific parameters live inside args so the
// top-level schema is independent of which MCP tool is being called.
const metaToolSchema = `{"type":"object","properties":{"server_name":{"type":"string","description":"The MCP server name, exactly as listed in this tool's description."},"tool_name":{"type":"string","description":"The server-local tool name, exactly as listed for server_name in this tool's description."},"args":{"type":"object","description":"Arguments object forwarded to the tool. All tool-specific parameters must be passed inside this args object as a JSON object."}},"required":["server_name","tool_name"]}`

// MetaTool is the single run_mcp dispatcher. It holds the Host (for live
// dispatch) and the configured specs (for cached-schema fallback when a
// background spawn is still in flight). It implements tool.Tool and
// tool.ImageTool; image content from MCP results flows through to vision models
// exactly as it would from a top-level mcp__ tool.
type MetaTool struct {
	host  *Host
	specs []Spec
}

// NewMetaTool constructs a run_mcp dispatcher. specs must be the same slice
// boot used to start the servers so cached-schema fallback can enumerate
// un-spawned servers' tools on the first turn. Hot-added servers (/mcp add)
// appear automatically via Host.ServerNames() once connected.
func NewMetaTool(host *Host, specs []Spec) *MetaTool {
	return &MetaTool{host: host, specs: specs}
}

func (m *MetaTool) Name() string             { return MetaToolName }
func (m *MetaTool) ReadOnly() bool           { return false } // dispatches to writer tools
func (m *MetaTool) Schema() json.RawMessage  { return json.RawMessage(metaToolSchema) }

// Description dynamically stitches the server_name → tool_name mapping the model
// uses to call run_mcp. See the file comment for the contract; the mapping is
// rebuilt every call from live clients plus cached schemas for un-spawned specs.
func (m *MetaTool) Description() string {
	serverTools := m.snapshotServerTools()
	if len(serverTools) == 0 {
		return "Call an MCP tool by server_name and tool_name. No MCP servers are currently available."
	}
	var b strings.Builder
	b.WriteString("Call an MCP tool by server_name and tool_name. Pass server_name and tool_name using the EXACT strings below.\n")
	b.WriteString("server_name -> available tool_name values (server_name is the quoted key; tool_name is one of the comma-separated values after the colon):\n")
	servers := make([]string, 0, len(serverTools))
	for s := range serverTools {
		servers = append(servers, s)
	}
	sort.Strings(servers)
	for _, s := range servers {
		tools := serverTools[s]
		sort.Strings(tools)
		fmt.Fprintf(&b, "  %q: %s\n", s, strings.Join(tools, ", "))
	}
	b.WriteString(`Call with {"server_name": <one of the quoted keys above>, "tool_name": <one of that server's listed tools>, "args": <object>}. Do not swap server_name and tool_name.`)
	return b.String()
}

// snapshotServerTools builds server_name → []raw_tool_name from live clients
// first, then fills in specs not yet connected from their on-disk cached
// schema. A server appears at most once: live data wins when both exist. Raw
// tool names are used (not StripRawPrefix-adjusted visible names) because
// tools/call takes the raw name — the description must list exactly what
// Execute forwards.
func (m *MetaTool) snapshotServerTools() map[string][]string {
	out := make(map[string][]string)

	// 1. Live connected clients. cachedTools() returns the remoteTool adapters
	// populated by listTools; each exposes its raw server-local name via
	// MCPMetadata. When the handshake is still in flight (toolsListed false),
	// cachedTools returns ok=false and we fall through to the cache below.
	if m.host != nil {
		for _, name := range m.host.ServerNames() {
			c := m.host.client(name)
			if c == nil {
				continue
			}
			tools, ok := c.cachedTools()
			if !ok || len(tools) == 0 {
				continue
			}
			names := make([]string, 0, len(tools))
			for _, t := range tools {
				if md, ok := t.(tool.MCPMetadata); ok {
					if raw := md.MCPRawToolName(); raw != "" {
						names = append(names, raw)
					}
				}
			}
			if len(names) > 0 {
				out[name] = names
			}
		}
	}

	// 2. Cached schemas for specs not yet connected (or connected but listTools
	// not finished). This keeps the surface correct on the first turn, before
	// background spawns complete — mirroring the cache-hit placeholder behavior
	// the lazy path has always presented at boot.
	for _, s := range m.specs {
		if _, live := out[s.Name]; live {
			continue
		}
		cs, ok := LoadCachedSchemaForSpec(s)
		if !ok || len(cs.Tools) == 0 {
			continue
		}
		names := make([]string, 0, len(cs.Tools))
		for _, ct := range cs.Tools {
			names = append(names, ct.Name)
		}
		if len(names) > 0 {
			out[s.Name] = names
		}
	}
	return out
}

// Execute dispatches to the named server's tools/call. server_name and
// tool_name must match the description's mapping; tool_name is the raw
// server-local name passed verbatim to MCP. args is forwarded as the arguments
// object.
func (m *MetaTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	text, _, err := m.ExecuteWithImages(ctx, args)
	return text, err
}

// ExecuteWithImages implements tool.ImageTool so MCP image content reaches
// vision models instead of being flattened to a text placeholder.
func (m *MetaTool) ExecuteWithImages(ctx context.Context, args json.RawMessage) (string, []string, error) {
	var p struct {
		ServerName string          `json:"server_name"`
		ToolName   string          `json:"tool_name"`
		Args       json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", nil, fmt.Errorf("run_mcp: invalid args: %w", err)
	}
	if p.ServerName == "" || p.ToolName == "" {
		return "", nil, fmt.Errorf("run_mcp: server_name and tool_name are required (got server_name=%q tool_name=%q)", p.ServerName, p.ToolName)
	}
	if m.host == nil {
		return "", nil, fmt.Errorf("run_mcp: no plugin host available")
	}
	c := m.host.client(p.ServerName)
	if c == nil {
		// Name the servers that ARE connected so the model can self-correct:
		// a typo is recoverable, a not-yet-spawned server is worth a retry.
		available := m.host.ServerNames()
		return "", nil, fmt.Errorf("run_mcp: MCP server %q is not connected (available: %v)", p.ServerName, available)
	}
	var argMap map[string]any
	if len(p.Args) > 0 {
		if err := json.Unmarshal(p.Args, &argMap); err != nil {
			return "", nil, fmt.Errorf("run_mcp: invalid args.args: %w", err)
		}
	}
	res, err := c.call(ctx, "tools/call", map[string]any{
		"name":      p.ToolName,
		"arguments": argMap,
	})
	if err != nil {
		return "", nil, fmt.Errorf("run_mcp: %s.%s: %w", p.ServerName, p.ToolName, err)
	}
	return parseToolResult(res)
}

// KickSpawns starts background handshakes for specs WITHOUT registering any
// placeholder or real tools in the registry. It is the meta-tool-mode
// counterpart of LazyToolset: the Host still needs connected clients for
// MetaTool.Execute to dispatch through, but the registry must stay free of
// mcp__ tools so the provider's tools array stays at builtins + 1.
//
// Constructing lazySpawn directly with removePrefix="" makes trySwap a registry
// no-op (trySwap only touches the registry when removePrefix != ""), so the
// post-spawn swap cannot re-add mcp__ tools back into the registry. The spawn
// still publishes its Client to the Host and writes the schema cache, so
// MetaTool sees the server via Host.client() / Host.ServerNames() once ready.
func KickSpawns(ctx context.Context, host *Host, specs []Spec) {
	for _, s := range specs {
		if host.HasClient(s.Name) {
			continue
		}
		spawnCtx, cancel := context.WithCancel(ctx)
		host.registerDeferredCancel(s.Name, cancel)
		shared := &lazySpawn{
			spec: s,
			host: host,
			ctx:  spawnCtx,
			// reg and removePrefix stay zero: trySwap becomes a registry no-op.
		}
		shared.kick()
	}
}

// MetaToolEnabled reports whether run_mcp meta-tool mode is enabled, checking
// ONLY the REASONIX_MCP_META_TOOL env var. It is the env-only shim kept for
// plugin-internal tests and direct package use; the production boot path uses
// config.MCPMetaToolEnabled() instead, which resolves [tools] meta_tool config
// first and treats this env var as an override. Recognized spellings:
// 1/true/yes/on enables, 0/false/no/off disables, unset/unknown = false.
//
// boot.go no longer calls this — it calls cfg.MCPMetaToolEnabled() so the
// config file is the primary control. This function stays so tests that set
// the env var directly (without loading config) still work, and so
// mcp-surface-dump can flip the mode via env without a config file.
func MetaToolEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("REASONIX_MCP_META_TOOL"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
