package control

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/contract/tool"
)

// catalogTool is a tool that names the server it came from, which is all the
// registry needs to bind it — a lazily registered placeholder answers exactly
// this much while its server's process is not running.
type catalogTool struct {
	name   string
	server string
	raw    string
}

func (c catalogTool) Name() string            { return c.name }
func (c catalogTool) Description() string     { return "" }
func (c catalogTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (c catalogTool) MCPServerName() string   { return c.server }
func (c catalogTool) MCPRawToolName() string  { return c.raw }
func (c catalogTool) ReadOnly() bool          { return true }
func (catalogTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

// The question a status row is really asking is whether the model can call this
// server now, and a running process is not that question: a cache-hit server
// stays process-idle with its whole toolset callable. Counting the catalog is
// what tells that apart from a server with nothing registered at all.
func TestCatalogToolsCountsWhatIsCallableRatherThanWhatIsRunning(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(catalogTool{name: "mcp__atlas__find_symbol", server: "atlas", raw: "find_symbol"})
	reg.Add(catalogTool{name: "mcp__atlas__rewrite", server: "atlas", raw: "rewrite"})
	reg.Add(catalogTool{name: "mcp__ledger__post", server: "ledger", raw: "post"})

	c := &Controller{controllerDeps: controllerDeps{mcp: newMcpManager(nil, reg, t.Context(), 0)}}
	got := c.MCPCatalogTools()

	if got["atlas"] != 2 {
		t.Fatalf("atlas = %d tools in the catalog, want 2 with no process running", got["atlas"])
	}
	if got["ledger"] != 1 {
		t.Fatalf("ledger = %d, want 1", got["ledger"])
	}
	if _, ok := got["absent"]; ok {
		t.Fatal("a server that registered nothing appears in the catalog count")
	}
}

// No registry is not an empty registry: a caller that cannot tell them apart
// would report every configured server as having nothing to offer.
func TestCatalogToolsAnswersNothingWithoutARegistry(t *testing.T) {
	c := &Controller{controllerDeps: controllerDeps{mcp: newMcpManager(nil, nil, t.Context(), 0)}}
	if got := c.MCPCatalogTools(); got != nil {
		t.Fatalf("catalog = %v with no registry, want nil", got)
	}
}

func TestDisconnectClearsDisabledMCPPolicy(t *testing.T) {
	reg := tool.NewRegistry()
	reg.ReplaceDisabledMCP("mock", []tool.MCPBinding{{
		Server:       "mock",
		RawName:      "write",
		VisibleName:  "write",
		CallableName: "mcp__mock__write",
		CapabilityID: "mcp-tool:mock/write",
	}})
	m := newMcpManager(nil, reg, context.Background(), 0)

	m.disconnect("mock")
	if reg.DisabledMCP("mcp__mock__write") || reg.DisabledMCP("mcp-tool:mock/write") {
		t.Fatal("disconnect left the server's disabled-tool aliases behind")
	}
}
