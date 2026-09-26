package pluginspec

import (
	"net/http"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
)

func TestPluginSpecsMapConfiguredMCPTimeouts(t *testing.T) {
	specs := ForRootWithOptions([]config.PluginEntry{{
		Name:                  "maker",
		Command:               "maker-mcp",
		StartupTimeoutSeconds: 45,
		CallTimeoutSeconds:    600,
		ToolTimeoutSeconds: map[string]int{
			"generate_video": 1800,
			" ":              120,
			"zero":           0,
		},
	}}, "", Options{
		DefaultStartupTimeout: 30 * time.Second,
		DefaultCallTimeout:    300 * time.Second,
	})
	if len(specs) != 1 {
		t.Fatalf("PluginSpecs returned %d specs, want 1", len(specs))
	}
	if specs[0].DefaultCallTimeout != 5*time.Minute {
		t.Fatalf("DefaultCallTimeout = %v, want 5m", specs[0].DefaultCallTimeout)
	}
	if specs[0].DefaultStartupTimeout != 30*time.Second || specs[0].StartupTimeout != 45*time.Second {
		t.Fatalf("startup timeouts = default %v override %v, want 30s/45s", specs[0].DefaultStartupTimeout, specs[0].StartupTimeout)
	}
	if specs[0].CallTimeout != 10*time.Minute {
		t.Fatalf("CallTimeout = %v, want 10m", specs[0].CallTimeout)
	}
	if specs[0].ToolTimeouts["generate_video"] != 30*time.Minute {
		t.Fatalf("generate_video timeout = %v, want 30m", specs[0].ToolTimeouts["generate_video"])
	}
	if _, ok := specs[0].ToolTimeouts["zero"]; ok {
		t.Fatalf("zero tool timeout should be ignored: %+v", specs[0].ToolTimeouts)
	}
	if _, ok := specs[0].ToolTimeouts[""]; ok {
		t.Fatalf("empty tool timeout should be ignored: %+v", specs[0].ToolTimeouts)
	}
}

func TestPluginSpecsMapMCPSourceDefaults(t *testing.T) {
	tests := []struct {
		name           string
		source         config.MCPConfigSource
		wantAuthorized bool
		wantApproval   bool
	}{
		{name: "user config", source: config.MCPSourceUserConfig, wantAuthorized: true},
		{name: "legacy user config", source: config.MCPSourceLegacyUser, wantAuthorized: true},
		{name: "plugin package", source: config.MCPSourcePluginPackage, wantAuthorized: true},
		{name: "project config", source: config.MCPSourceProjectConfig, wantAuthorized: true},
		{name: "project mcp json", source: config.MCPSourceProjectMCPJSON, wantAuthorized: true},
		{name: "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			specs := ForRootWithOptions([]config.PluginEntry{{
				Name:   "server",
				Source: tc.source,
			}}, "/workspace", Options{ConfigSource: "workspace_config"})
			if len(specs) != 1 {
				t.Fatalf("spec count = %d", len(specs))
			}
			if specs[0].Authorized != tc.wantAuthorized || specs[0].RequireLaunchApproval != tc.wantApproval {
				t.Fatalf("source defaults = %+v, want authorized=%v approval=%v", specs[0], tc.wantAuthorized, tc.wantApproval)
			}
			wantSource := string(tc.source)
			if wantSource == "" {
				wantSource = "workspace_config"
			}
			if specs[0].ConfigSource != wantSource {
				t.Fatalf("ConfigSource = %q, want %q", specs[0].ConfigSource, wantSource)
			}
		})
	}
}

func TestPluginSpecsCarryPluginPackageProvenance(t *testing.T) {
	specs := ForRootWithOptions([]config.PluginEntry{{Name: "figma"}}, "/workspace", Options{
		PackageOwners: map[string]string{"figma": "design-plugin"},
	})
	if len(specs) != 1 || specs[0].Package != "design-plugin" {
		t.Fatalf("plugin package provenance = %+v, want design-plugin", specs)
	}
}

func TestApplyDefaultMCPCallTimeoutPreservesConfiguredDefault(t *testing.T) {
	specs := ApplyDefaultCallTimeout([]plugin.Spec{
		{Name: "configured", DefaultCallTimeout: 2 * time.Minute},
		{Name: "empty"},
	}, 5*time.Minute)
	if specs[0].DefaultCallTimeout != 2*time.Minute {
		t.Fatalf("configured DefaultCallTimeout overwritten: %v", specs[0].DefaultCallTimeout)
	}
	if specs[1].DefaultCallTimeout != 5*time.Minute {
		t.Fatalf("empty DefaultCallTimeout = %v, want 5m", specs[1].DefaultCallTimeout)
	}
}

func TestApplyDefaultMCPStartupTimeoutPreservesConfiguredDefault(t *testing.T) {
	specs := ApplyDefaultStartupTimeout([]plugin.Spec{
		{Name: "configured", DefaultStartupTimeout: 20 * time.Second},
		{Name: "empty"},
	}, 30*time.Second)
	if specs[0].DefaultStartupTimeout != 20*time.Second {
		t.Fatalf("configured DefaultStartupTimeout overwritten: %v", specs[0].DefaultStartupTimeout)
	}
	if specs[1].DefaultStartupTimeout != 30*time.Second {
		t.Fatalf("empty DefaultStartupTimeout = %v, want 30s", specs[1].DefaultStartupTimeout)
	}
}

func TestPluginSpecsForRootPinsCodeGraphToWorkspace(t *testing.T) {
	specs := ForRoot([]config.PluginEntry{{Name: "codegraph"}}, "/workspace")
	if len(specs) != 1 {
		t.Fatalf("PluginSpecsForRoot returned %d specs, want 1", len(specs))
	}
	if specs[0].Dir != "/workspace" {
		t.Fatalf("codegraph Dir = %q, want workspace root", specs[0].Dir)
	}
	if specs[0].WorkspaceRoot != "/workspace" {
		t.Fatalf("codegraph WorkspaceRoot = %q, want /workspace", specs[0].WorkspaceRoot)
	}
}

func TestPluginSpecsCarryOAuthHTTPClient(t *testing.T) {
	client := &http.Client{}
	specs := ForRootWithOptions([]config.PluginEntry{{
		Name: "remote", Type: "http", URL: "https://mcp.example.test",
	}}, "", Options{OAuthHTTPClient: client})
	if len(specs) != 1 || specs[0].OAuthHTTPClient != client {
		t.Fatalf("OAuth HTTP client was not carried into plugin.Spec: %+v", specs)
	}
}

func TestPluginSpecsForRootDoesNotPinHTTPCodeGraph(t *testing.T) {
	specs := ForRoot([]config.PluginEntry{{Name: "codegraph", Type: "http", URL: "https://example.com/mcp"}}, "/workspace")
	if len(specs) != 1 {
		t.Fatalf("ForRoot returned %d specs, want 1", len(specs))
	}
	if specs[0].Dir != "" {
		t.Fatalf("http codegraph Dir = %q, want empty", specs[0].Dir)
	}
	if specs[0].WorkspaceRoot != "/workspace" {
		t.Fatalf("http codegraph WorkspaceRoot = %q, want /workspace", specs[0].WorkspaceRoot)
	}
}

func TestPluginSpecsForRootExpandWorkspaceRoot(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "/inherited/from/a/parent/hook")
	root := t.TempDir()
	specs := ForRoot([]config.PluginEntry{{
		Name: "idea", Type: "http", URL: "http://127.0.0.1:64342/stream",
		Headers: map[string]string{"X-Project": "${REASONIX_WORKSPACE_ROOT}", "X-Claude": "${CLAUDE_PROJECT_DIR}"},
	}}, root)
	want := config.WorkspaceRootValue(root)
	if got := specs[0].Headers; got["X-Project"] != want || got["X-Claude"] != want {
		t.Fatalf("headers = %v, want both %q", got, want)
	}
}
