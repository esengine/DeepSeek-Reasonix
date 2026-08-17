package boot

import (
	"context"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
)

func TestCompressOnUnifiedSurfaceForAllRoleSettings(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	registerBootTokenProfileTestProvider()
	for _, mode := range []string{"", "light", "economy", "balanced", "full", "delivery"} {
		t.Run(firstNonEmpty(mode, "default"), func(t *testing.T) {
			prov := testutil.NewMock("compress-"+mode, testutil.Turn{Text: "done"})
			setBootTokenProfileTestProvider(t, prov)
			ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: mode, AgentPreset: mode})
			if err != nil {
				t.Fatalf("Build(%q): %v", mode, err)
			}
			defer ctrl.Close()
			if err := ctrl.Run(context.Background(), "hi"); err != nil {
				t.Fatalf("Run(%q): %v", mode, err)
			}
			if !requestHasTool(prov.Requests()[0], "compress") {
				// compress is part of the unified core when registered by boot.
				// Some minimal configs may omit it; still must never reappear via connect_tool_source.
				reg := map[string]bool{}
				for _, e := range ctrl.AllToolContractEntries() {
					reg[e.Name] = true
				}
				if !reg["compress"] {
					t.Fatalf("%q registry missing compress", mode)
				}
			}
			if requestHasTool(prov.Requests()[0], "connect_tool_source") {
				t.Fatalf("%q still exposes connect_tool_source", mode)
			}
		})
	}
}
