package plugin

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func TestCompositionMCPContractBlocksRefreshCallsAndReconnectDrift(t *testing.T) {
	for _, follow := range []bool{false, true} {
		t.Run(fmt.Sprintf("follow=%v", follow), func(t *testing.T) {
			compositionMCPContractScenario(t, follow)
		})
	}
}

func compositionMCPContractScenario(t *testing.T, follow bool) {
	t.Helper()
	server, srv := newListChangedServer(t, true, "alpha")
	h, _ := connectListChanged(t, srv, Spec{Authorized: true, StateDir: filepath.Join(t.TempDir(), "state", "live")})
	spec := h.lookupClient("live").spec
	tools, err := h.ToolsFor(t.Context(), "live")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := h.PinCompositionTools("live", "")
	if err != nil || len(hash) != 64 {
		t.Fatalf("pin=%s %v", hash, err)
	}
	if _, err := h.PinCompositionTools("live", hash); err != nil {
		t.Fatal(err)
	}
	if _, err := h.PinCompositionTools("live", "different"); err == nil {
		t.Fatal("mismatched contract accepted")
	}
	if _, err := h.PinCompositionTools("missing", ""); err == nil {
		t.Fatal("missing server accepted")
	}
	if h.CompositionContractError() != nil {
		t.Fatal("valid contract blocked")
	}
	if err := SetToolPinsFollow(spec, follow); err != nil {
		t.Fatal(err)
	}
	if _, err := tools[0].Execute(t.Context(), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	before := server.calls.Load()
	server.announce(0, "alpha", "beta")
	if _, _, err := h.lookupClient("live").refreshToolCatalog(t.Context()); err == nil {
		t.Fatal("catalog drift published")
	}
	if h.CompositionContractError() == nil {
		t.Fatal("contract drift not reported")
	}
	if _, err := tools[0].Execute(t.Context(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("stale adapter executed after drift")
	}
	if server.calls.Load() != before {
		t.Fatal("blocked call reached server")
	}
	h.Remove("live")
	if _, err := h.EnsureConnected(t.Context(), spec); err == nil {
		t.Fatal("reconnect bypassed binding")
	}
}
