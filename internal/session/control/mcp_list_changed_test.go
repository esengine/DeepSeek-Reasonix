package control

import (
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
)

func refreshedTool(name, raw string) catalogTool {
	return catalogTool{name: name, server: "atlas", raw: raw}
}

// A refresh replaces what the server offers rather than adding to it: a tool it
// dropped has to leave the registry, or the model keeps being offered a call
// that can only fail.
func TestRefreshedMCPToolsReplaceTheServerPrefix(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(refreshedTool("mcp__atlas__gone", "gone"))
	reg.Add(refreshedTool("mcp__atlas__kept", "kept"))
	reg.Add(catalogTool{name: "mcp__ledger__post", server: "ledger", raw: "post"})

	registerRefreshedMCPTools(reg, plugin.Spec{Name: "atlas"}, []tool.Tool{
		refreshedTool("mcp__atlas__kept", "kept"),
		refreshedTool("mcp__atlas__added", "added"),
	})

	for name, want := range map[string]bool{
		"mcp__atlas__gone": false, "mcp__atlas__kept": true,
		"mcp__atlas__added": true, "mcp__ledger__post": true,
	} {
		if _, ok := reg.Get(name); ok != want {
			t.Errorf("%s present = %v, want %v", name, ok, want)
		}
	}
}

// A session that suspended a server turned it off. A server announcing new
// tools is not the user asking for it back, so the refresh must not be what
// puts its prefix on screen again.
func TestRefreshedMCPToolsLeaveASuspendedServerOff(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(refreshedTool("mcp__atlas__old", "old"))
	reg.SuspendPrefix(plugin.ToolPrefix("atlas"))

	registerRefreshedMCPTools(reg, plugin.Spec{Name: "atlas"}, []tool.Tool{
		refreshedTool("mcp__atlas__added", "added"),
	})

	if _, ok := reg.Get("mcp__atlas__added"); ok {
		t.Fatal("a refresh brought back a server this session had turned off")
	}
}

// The provider-visible tools array is fixed for the session: a pinned server
// that drops a tool must not take its schema out of the array, or every cached
// token behind it is spent again on the next request.
func TestRefreshedMCPToolsKeepAPinnedSchemaSlot(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(refreshedTool("mcp__atlas__gone", "gone"))
	reg.Add(refreshedTool("mcp__atlas__kept", "kept"))
	reg.SetProviderVisibleTools([]string{"mcp__atlas__gone", "mcp__atlas__kept"})
	before := reg.Schemas()

	withdrawn := registerRefreshedMCPTools(reg, plugin.Spec{Name: "atlas"}, []tool.Tool{
		refreshedTool("mcp__atlas__kept", "kept"),
		refreshedTool("mcp__atlas__added", "added"),
	})

	if diff := schemaNames(reg.Schemas()); !slices.Equal(diff, schemaNames(before)) {
		t.Fatalf("the provider-visible tools array moved with the catalog: %v, want %v", diff, schemaNames(before))
	}
	if !slices.Equal(withdrawn, []string{"mcp__atlas__gone"}) {
		t.Fatalf("withdrawn = %v, want the dropped pinned tool", withdrawn)
	}
	gone, ok := reg.Get("mcp__atlas__gone")
	if !ok || !tool.IsWithdrawn(gone) {
		t.Fatalf("the dropped tool is %v (withdrawn=%v), want a withdrawn schema slot", ok, tool.IsWithdrawn(gone))
	}
	if _, err := gone.Execute(t.Context(), nil); err == nil || !strings.Contains(err.Error(), `MCP tool "gone" not found on server "atlas"`) {
		t.Fatalf("calling the withdrawn tool returned %v, want the typed not-found", err)
	}
	// What arrived is registered but unpinned: it is reachable through
	// use_capability, which is the turn tail, not the cached prefix.
	if _, ok := reg.Get("mcp__atlas__added"); !ok {
		t.Fatal("the tool the server added never reached the registry")
	}
	if reg.ProviderSurfacePinned("mcp__atlas__added") {
		t.Fatal("a tool that arrived mid-session was pinned into the provider schema")
	}
}

// A tool the model was never shown has no slot to hold: dropping it is just a
// removal, and keeping it would offer a dead call to every later turn.
func TestRefreshedMCPToolsDropAnUnpinnedTool(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(refreshedTool("mcp__atlas__gone", "gone"))
	reg.SetProviderVisibleTools([]string{"use_capability"})

	if withdrawn := registerRefreshedMCPTools(reg, plugin.Spec{Name: "atlas"}, nil); len(withdrawn) != 0 {
		t.Fatalf("withdrawn = %v, want nothing announced for a tool the model never saw", withdrawn)
	}
	if _, ok := reg.Get("mcp__atlas__gone"); ok {
		t.Fatal("an unpinned tool the server dropped stayed in the registry")
	}
}

// A second refresh must not announce the same withdrawal again: the slot is
// already held, and the model was already told once.
func TestRefreshedMCPToolsAnnounceAWithdrawalOnce(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(refreshedTool("mcp__atlas__gone", "gone"))
	reg.SetProviderVisibleTools([]string{"mcp__atlas__gone"})

	first := registerRefreshedMCPTools(reg, plugin.Spec{Name: "atlas"}, nil)
	second := registerRefreshedMCPTools(reg, plugin.Spec{Name: "atlas"}, nil)
	if len(first) != 1 || len(second) != 0 {
		t.Fatalf("announced %v then %v, want the withdrawal stated once", first, second)
	}
	if _, ok := reg.Get("mcp__atlas__gone"); !ok {
		t.Fatal("the withdrawn schema slot was vacated by the second refresh")
	}
}

// A tool that comes back is a live tool again, not a slot: the model's calls
// have to reach the server rather than the message about its absence.
func TestRefreshedMCPToolsRestoreAToolThatReturns(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(refreshedTool("mcp__atlas__gone", "gone"))
	reg.SetProviderVisibleTools([]string{"mcp__atlas__gone"})

	registerRefreshedMCPTools(reg, plugin.Spec{Name: "atlas"}, nil)
	registerRefreshedMCPTools(reg, plugin.Spec{Name: "atlas"}, []tool.Tool{refreshedTool("mcp__atlas__gone", "gone")})

	back, ok := reg.Get("mcp__atlas__gone")
	if !ok || tool.IsWithdrawn(back) {
		t.Fatalf("the returning tool is %v (withdrawn=%v), want the server's tool back", ok, tool.IsWithdrawn(back))
	}
}

func schemaNames(schemas []provider.ToolSchema) []string {
	out := make([]string, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s.Name+"|"+s.Description+"|"+string(s.Parameters))
	}
	return out
}
