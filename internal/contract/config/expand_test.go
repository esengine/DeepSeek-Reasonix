package config

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestExpandVars(t *testing.T) {
	t.Setenv("REASONIX_TEST_TOKEN", "sk-123")
	t.Setenv("REASONIX_TEST_EMPTY", "")

	cases := []struct{ in, want string }{
		{"Bearer ${REASONIX_TEST_TOKEN}", "Bearer sk-123"},
		{"${REASONIX_TEST_MISSING}", ""},                                   // unset, no default → empty
		{"${REASONIX_TEST_MISSING:-fallback}", "fallback"},                 // unset → default
		{"${REASONIX_TEST_EMPTY:-fallback}", "fallback"},                   // set-but-empty → default
		{"${REASONIX_TEST_TOKEN:-fallback}", "sk-123"},                     // set → value, default ignored
		{"no vars here", "no vars here"},                                   // untouched
		{"a${REASONIX_TEST_TOKEN}b${REASONIX_TEST_MISSING}c", "ask-123bc"}, // multiple refs
	}
	for _, c := range cases {
		if got := ExpandVars(c.in); got != c.want {
			t.Errorf("ExpandVars(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExpandedPlugin(t *testing.T) {
	t.Setenv("REASONIX_TEST_KEY", "secret")
	e := PluginEntry{
		Name:    "x",
		Type:    "http",
		URL:     "https://api/${REASONIX_TEST_MISSING:-v1}",
		Args:    []string{"--token", "${REASONIX_TEST_KEY}"},
		Env:     map[string]string{"K": "${REASONIX_TEST_KEY}"},
		Headers: map[string]string{"Authorization": "Bearer ${REASONIX_TEST_KEY}"},
	}
	out := e.ExpandedPlugin()
	if out.URL != "https://api/v1" {
		t.Errorf("URL = %q", out.URL)
	}
	if out.Args[1] != "secret" {
		t.Errorf("Args = %v", out.Args)
	}
	if out.Env["K"] != "secret" || out.Headers["Authorization"] != "Bearer secret" {
		t.Errorf("env/headers not expanded: %v %v", out.Env, out.Headers)
	}
	// The original entry must be untouched (we returned a copy).
	if e.Headers["Authorization"] != "Bearer ${REASONIX_TEST_KEY}" {
		t.Error("ExpandedPlugin mutated the original entry")
	}
}

func TestForbidReadRootsForRootResolvesRelativePathsAndScopedEnv(t *testing.T) {
	root := testenv.TempDir(t)
	cfg := Default()
	cfg.setExpansionEnv(map[string]string{"REASONIX_TEST_SECRET_DIR": "from-dotenv"})
	cfg.Sandbox.ForbidRead = []string{
		"relative-secret",
		"${REASONIX_TEST_SECRET_DIR}",
		filepath.Join(root, "absolute-secret"),
	}

	got := cfg.ForbidReadRootsForRoot(root)
	want := []string{
		filepath.Join(root, "relative-secret"),
		filepath.Join(root, "from-dotenv"),
		filepath.Join(root, "absolute-secret"),
	}
	if len(got) != len(want) {
		t.Fatalf("ForbidReadRootsForRoot returned %d roots, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("root %d = %q, want %q (all roots: %v)", i, got[i], want[i], got)
		}
	}
}

func TestWriteRootsForRootExpandsMavenAllowWrite(t *testing.T) {
	home := testenv.TempDir(t)
	project := testenv.TempDir(t)
	t.Setenv("HOME", home)

	cfg := Default()
	cfg.Sandbox.AllowWrite = []string{
		"${HOME}/.m2",
		"${HOME}/.m2/repository",
	}

	got := cfg.WriteRootsForRoot(project)
	// WriteRootsForRoot expands variables but leaves configured separators intact;
	// the writer confiner normalizes roots later.
	want := []string{
		project,
		home + "/.m2",
		home + "/.m2/repository",
	}
	if len(got) != len(want) {
		t.Fatalf("WriteRootsForRoot() returned %d roots, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("root %d = %q, want %q (all roots: %v)", i, got[i], want[i], got)
		}
	}
}

func TestLoadForRootExpandsWorkspaceRootInPluginEntries(t *testing.T) {
	_, userConfig, _ := legacyHome(t)
	t.Setenv("CLAUDE_PROJECT_DIR", "/inherited/from/a/parent/hook")
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfig, []byte(`
[[plugins]]
name = "idea"
type = "http"
url = "http://127.0.0.1:64342/stream"
headers = { IJ_MCP_SERVER_PROJECT_PATH = "${REASONIX_WORKSPACE_ROOT}" }
`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, root := range []string{testenv.TempDir(t), testenv.TempDir(t)} {
		if err := os.WriteFile(filepath.Join(root, mcpJSONFile), []byte(`{
  "mcpServers": {
    "local": { "command": "server", "args": ["--project", "${CLAUDE_PROJECT_DIR}"] }
  }
}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".env"), []byte("CLAUDE_PROJECT_DIR=/from/dotenv\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		want, err := filepath.Abs(root)
		if err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadForRoot(root)
		if err != nil {
			t.Fatal(err)
		}
		idea, ok := pluginEntryByName(cfg.Plugins, "idea")
		if !ok {
			t.Fatalf("global plugin missing: %+v", cfg.Plugins)
		}
		if got := idea.ExpandedPluginForRoot(root).Headers["IJ_MCP_SERVER_PROJECT_PATH"]; got != want {
			t.Fatalf("header = %q, want workspace root %q", got, want)
		}
		local, ok := pluginEntryByName(cfg.Plugins, "local")
		if !ok {
			t.Fatalf(".mcp.json plugin missing: %+v", cfg.Plugins)
		}
		if got := local.ExpandedPluginForRoot(root).Args[1]; got != want {
			t.Fatalf("arg = %q, want workspace root %q", got, want)
		}
	}
}

func TestWorkspaceRootVarsStayOutOfConfigPathExpansion(t *testing.T) {
	t.Setenv("REASONIX_WORKSPACE_ROOT", "")
	root := testenv.TempDir(t)
	cfg := Default()
	cfg.Sandbox.ForbidRead = []string{"${REASONIX_WORKSPACE_ROOT}/secret"}
	got := cfg.ForbidReadRootsForRoot(root)
	if len(got) != 1 || got[0] == filepath.Join(root, "secret") {
		t.Fatalf("ForbidReadRootsForRoot = %v, want the variable left to the environment", got)
	}
}
