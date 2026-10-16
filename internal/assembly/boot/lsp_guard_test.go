package boot

import (
	"testing"

	"reasonix/internal/contract/config"
)

// A phone has no language server and no way to get one, so the tools must stay
// out of the schema rather than be offered and refuse on the first call — the
// same reasoning that keeps the browser tools off Android.
func TestLSPWithoutServerOnAndroid(t *testing.T) {
	// The shipped defaults, which is what an untouched config has.
	if !lspWithoutServer("android", config.LSPConfig{Enabled: true}) {
		t.Fatal("android with the default servers should have no LSP tools")
	}
	// A host that names its own command is taken at its word: the manager
	// decides the rest at first query, exactly as it does for a named browser.
	named := config.LSPConfig{Enabled: true, Servers: map[string]config.LSPServer{
		"go": {Command: "/data/local/tmp/gopls"},
	}}
	if lspWithoutServer("android", named) {
		t.Fatal("a server named in the config should keep the LSP tools on android")
	}
	// Every other platform keeps the existing behavior: the tools are bound and
	// a missing server is discovered when one is first asked for.
	for _, goos := range []string{"linux", "darwin", "windows"} {
		if lspWithoutServer(goos, config.LSPConfig{Enabled: true}) {
			t.Fatalf("%s should keep its LSP tools", goos)
		}
	}
}

// The check is on the platform, not on the machine running the test.
func TestLSPWithoutServerIgnoresThisHost(t *testing.T) {
	if lspWithoutServer("windows", config.LSPConfig{Enabled: true}) {
		t.Fatal("this host should still bind the LSP tools")
	}
}
