package pluginpkg

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// Configuration sees an enabled package only through this projection, so
// commands and prompts arrive as one list of command dirs and each declared
// server keeps its transport; a disabled package contributes nothing.
func TestConfigPackagesProjectsEnabledPackages(t *testing.T) {
	home := testenv.TempDir(t)
	root := filepath.Join(home, "plugins", "demo")
	writeV2Plugin(t, root, `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "demo",
  "version": "2.1.0",
  "contributes": {
    "commands": ["commands"],
    "prompts": ["prompts"],
    "mcpServers": {"srv": {"type": "stdio", "command": "./srv", "args": ["--serve"], "load": "always"}}
  }
}`)
	writeTestFile(t, filepath.Join(root, "commands", "go.md"), "Go")
	writeTestFile(t, filepath.Join(root, "prompts", "ask.md"), "Ask")
	if err := Upsert(home, InstalledPlugin{Name: "demo", Version: "2.1.0", Root: "plugins/demo", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(home, InstalledPlugin{Name: "off", Root: "plugins/off", Enabled: false}); err != nil {
		t.Fatal(err)
	}

	got := configPackages(home)
	if len(got) != 1 {
		t.Fatalf("packages = %+v, want only the enabled one", got)
	}
	pkg := got[0]
	if pkg.Name != "demo" || pkg.Version != "2.1.0" || pkg.Root == "" {
		t.Fatalf("package = %+v", pkg)
	}
	if len(pkg.CommandDirs) != 2 {
		t.Fatalf("command dirs = %+v, want the commands and the prompts root", pkg.CommandDirs)
	}
	srv, ok := pkg.MCPServers["srv"]
	if !ok || srv.Type != "stdio" || srv.Command == "" || len(srv.Args) != 1 || srv.Args[0] != "--serve" || srv.Load != "always" {
		t.Fatalf("servers = %+v", pkg.MCPServers)
	}
}
