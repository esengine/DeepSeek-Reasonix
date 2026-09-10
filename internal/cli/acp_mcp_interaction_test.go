package cli

import (
	"reasonix/internal/acp"
	"reasonix/internal/plugin"
	"testing"
)

func TestACPMCPProfileFollowsClientInteractionCapability(t *testing.T) {
	factory := &acpFactory{}
	for _, enabled := range []bool{false, true} {
		opts, err := factory.sessionBootOptions(acp.SessionParams{Cwd: t.TempDir(), MCPInteractions: enabled})
		if err != nil {
			t.Fatal(err)
		}
		want := plugin.HostProfileCore
		if enabled {
			want = plugin.HostProfileInteractive
		}
		if opts.MCPHostProfile != want {
			t.Fatalf("enabled=%v profile=%v, want %v", enabled, opts.MCPHostProfile, want)
		}
	}
}
