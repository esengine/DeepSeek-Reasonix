package config

import (
	"path/filepath"
	"testing"
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
		Dir:     "${REASONIX_TEST_KEY}/sub",
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
	if out.Dir != "secret/sub" {
		t.Errorf("Dir = %q, want %q", out.Dir, "secret/sub")
	}
	// The original entry must be untouched (we returned a copy).
	if e.Headers["Authorization"] != "Bearer ${REASONIX_TEST_KEY}" {
		t.Error("ExpandedPlugin mutated the original entry")
	}
	if e.Dir != "${REASONIX_TEST_KEY}/sub" {
		t.Error("ExpandedPlugin mutated the original Dir")
	}
}

func TestExpandedPluginForWorkspace(t *testing.T) {
	e := PluginEntry{
		Name: "server",
		Args: []string{"--root", "${WORKSPACE_ROOT}"},
		Dir:  "${REASONIX_WORKSPACE}/sub",
		Env:  map[string]string{"CWD": "${WORKSPACE_ROOT}"},
	}
	out := e.ExpandedPluginForWorkspace("/work")
	if out.Args[1] != "/work" {
		t.Errorf("Args[1] = %q, want /work", out.Args[1])
	}
	if out.Dir != "/work/sub" {
		t.Errorf("Dir = %q, want /work/sub", out.Dir)
	}
	if out.Env["CWD"] != "/work" {
		t.Errorf("Env[CWD] = %q, want /work", out.Env["CWD"])
	}
}

func TestExpandedPluginForWorkspacePrecedence(t *testing.T) {
	t.Setenv("WORKSPACE_ROOT", "/env-root")
	e := PluginEntry{
		Name: "server",
		Args: []string{"--root", "${WORKSPACE_ROOT}"},
	}
	// Workspace root wins over the env var.
	out := e.ExpandedPluginForWorkspace("/session-root")
	if out.Args[1] != "/session-root" {
		t.Errorf("Args[1] = %q, want /session-root (session root should beat env)", out.Args[1])
	}
}

func TestExpandedPluginForWorkspaceEmptyRoot(t *testing.T) {
	t.Setenv("REASONIX_TEST_KEY", "env-val")
	e := PluginEntry{
		Name: "server",
		Args: []string{"${WORKSPACE_ROOT}", "${REASONIX_TEST_KEY}"},
		Dir:  "${WORKSPACE_ROOT:-fallback}",
	}
	// Empty root: WORKSPACE_ROOT falls back to env (unset → ""), :-default kicks in.
	out := e.ExpandedPluginForWorkspace("")
	if out.Args[0] != "" {
		t.Errorf("Args[0] = %q, want \"\" (empty root)", out.Args[0])
	}
	if out.Args[1] != "env-val" {
		t.Errorf("Args[1] = %q, want env-val (env fallback)", out.Args[1])
	}
	if out.Dir != "fallback" {
		t.Errorf("Dir = %q, want fallback (:-default)", out.Dir)
	}
}

func TestForbidReadRootsForRootResolvesRelativePathsAndScopedEnv(t *testing.T) {
	root := t.TempDir()
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
	home := t.TempDir()
	project := t.TempDir()
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
