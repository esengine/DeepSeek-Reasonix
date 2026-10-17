package boot

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/hook"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/ext/pluginpkg"
)

func TestCompositionRetainsNoManifestCredentials(t *testing.T) {
	home, root := compositionTestWorkspace(t, "openai")
	writeFile(t, root, ".mcp.json", `{"mcpServers":{"docs":{"command":"server","args":["--token","private-fixture-secret"],"headers":{"Authorization":"private-fixture-secret"}}}}`)
	p := pluginpkg.InstalledPackage{Installed: pluginpkg.InstalledPlugin{Name: "docs", Source: "https://private-fixture-secret@example.test"}}
	p.Package.Root = root
	p.Package.Manifest.MCPServers = map[string]pluginpkg.MCPServer{"docs": {Command: "server", Args: []string{"--token", "private-fixture-secret"}}}
	b := &builder{roots: config.RootsForHome(home), root: root, opts: Options{Composition: &agentcomposition.Binding{}}}
	if err := b.retainExtension(p); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(b.opts.Composition.Resources)
	if strings.Contains(string(raw), "private-fixture-secret") {
		t.Fatal("credentials entered retained package metadata")
	}
	resources, err := resource[retainedExtension](b.opts.Composition, "extension")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range resources[0].Assets.Files {
		if strings.Contains(string(file.Data), "private-fixture-secret") {
			t.Fatal("credentials entered retained package files")
		}
	}
	h := hook.ResolvedHook{}
	h.Command = "curl --token private-fixture-secret https://example.test"
	if err := b.retainHook("user:hook", h); !errors.Is(err, agentcomposition.ErrUnavailable) {
		t.Fatalf("literal hook credentials accepted: %v", err)
	}
}

func TestCompositionCannotReuseRevokedMCPAuthorization(t *testing.T) {
	home, root := compositionTestWorkspace(t, "openai")
	entry := config.PluginEntry{Name: "docs", Command: filepath.Join(root, "server"), Source: config.MCPSourceUserConfig}
	if err := config.DefaultActivationStore().SetServerEnabled(entry, root, config.ActivationGlobal, false); err != nil {
		t.Fatal(err)
	}
	b := &builder{roots: config.RootsForHome(home), root: root, cfg: &config.Config{Plugins: []config.PluginEntry{entry}}, opts: Options{Composition: &agentcomposition.Binding{Resolved: agentcomposition.Builtins()[1]}}}
	b.tools.host = plugin.NewHost()
	defer b.tools.host.Close()
	if err := b.validateCompositionAvailability(); !errors.Is(err, agentcomposition.ErrUnavailable) {
		t.Fatalf("revoked authorization accepted: %v", err)
	}
}
