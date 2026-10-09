package boot

import (
	"slices"
	"testing"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

// glmCatalogTOML is a real Zhipu host, so the depth contract applies, with two
// models that sit on opposite sides of the "can thinking be switched off"
// line. Neither declares supported_efforts.
const glmCatalogTOML = `
default_model = "glm/glm-5.3"

[codegraph]
enabled = false

[[providers]]
name = "glm"
kind = "openai"
base_url = "https://open.bigmodel.cn/api/paas/v4"
api_key = "test-key"
models = ["glm-5.3", "glm-5.2"]
`

func TestCatalogCarriesTheGlmDepthLadderWithoutADeclaredList(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", glmCatalogTOML)
	approveWorkspace(t, dir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	byRef := map[string]provider.Descriptor{}
	for _, d := range NewLocalProviderResolver(cfg, netclient.ProxySpec{}).Catalog() {
		byRef[d.Ref] = d
	}

	cases := []struct {
		ref     string
		efforts []string
		def     string
		forces  bool
	}{
		// GLM-5.3 always reasons, so its cheapest level is low, not off.
		{"glm/glm-5.3", []string{"auto", "low", "high", "max"}, "max", true},
		// GLM-5.2 keeps none/minimal as a real off switch.
		{"glm/glm-5.2", []string{"auto", "none", "minimal", "low", "medium", "high", "xhigh", "max"}, "max", false},
	}
	for _, tc := range cases {
		t.Run(tc.ref, func(t *testing.T) {
			d, ok := byRef[tc.ref]
			if !ok {
				t.Fatalf("%s is missing from the catalog", tc.ref)
			}
			if !d.Reasoning {
				t.Error("Reasoning = false, so the picker hides the ladder")
			}
			if !slices.Equal(d.Efforts, tc.efforts) {
				t.Errorf("Efforts = %v, want %v", d.Efforts, tc.efforts)
			}
			if d.DefaultEffort != tc.def {
				t.Errorf("DefaultEffort = %q, want %q", d.DefaultEffort, tc.def)
			}
			if d.ForcesThinking != tc.forces {
				t.Errorf("ForcesThinking = %v, want %v", d.ForcesThinking, tc.forces)
			}
		})
	}
}
