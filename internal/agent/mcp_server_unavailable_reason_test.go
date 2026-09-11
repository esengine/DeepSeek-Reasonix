package agent

import (
	"strings"
	"testing"

	"reasonix/internal/config"
)

// "disabled" and "never registered" have different causes and different fixes.
// Reporting the second as the first sends the user looking for a toggle that
// does not exist — which is exactly what a host-session MCP server used to hit,
// since it connects and lists its tools while the capability runtime has no
// record of it at all.
func TestServerUnavailableReasonSeparatesUnregisteredFromDisabled(t *testing.T) {
	r := runtimeWithServers(config.PluginEntry{Name: "enabled-server"})
	r.servers["disabled-server"] = mcpRuntimeServer{
		entry:   config.PluginEntry{Name: "disabled-server"},
		enabled: false,
	}
	frontend := r.NewFrontend(nil, nil)

	if got := frontend.serverUnavailableReason("disabled-server"); !strings.Contains(got, "is disabled in this session") {
		t.Errorf("registered-but-disabled server: reason = %q, want it to say disabled", got)
	}
	if got := frontend.serverUnavailableReason("ghost-server"); !strings.Contains(got, "is not registered in this session") {
		t.Errorf("unregistered server: reason = %q, want it to say not registered", got)
	}
	if !frontend.serverEnabled("enabled-server") {
		t.Error("enabled-server should dispatch")
	}
	if frontend.serverEnabled("disabled-server") || frontend.serverEnabled("ghost-server") {
		t.Error("neither a disabled nor an unregistered server may dispatch")
	}
}
