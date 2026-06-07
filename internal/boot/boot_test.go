package boot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"

	// Blank import registers the provider kind the same way cmd/reasonix's main
	// does; importing builtin above registers the built-in tools.
	_ "reasonix/internal/provider/openai"
)

// TestBuildFoldsProjectMemoryIntoSystemPrompt is the end-to-end proof of the
// cache-first wiring: a project REASONIX.md is discovered at boot and folded
// into the session's system message (the cached prefix), and the `remember`
// tool is registered. It builds a real Controller from a throwaway project dir.
func TestBuildFoldsProjectMemoryIntoSystemPrompt(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE SYSTEM PROMPT"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`)
	writeFile(t, dir, "REASONIX.md", "Project rule: always run go vet before committing.")

	ctrl, err := Build(context.Background(), Options{}) // RequireKey false: no network/key needed
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	// The system message is the cached prefix; it must contain both the base
	// prompt and the discovered memory.
	sys := systemMessage(ctrl.History())
	if !strings.Contains(sys, "BASE SYSTEM PROMPT") {
		t.Fatalf("base prompt missing from system message:\n%s", sys)
	}
	if !strings.Contains(sys, "always run go vet before committing") {
		t.Fatalf("project REASONIX.md not folded into system message:\n%s", sys)
	}
	// Base must come first so it stays a valid cache prefix when memory changes.
	if strings.Index(sys, "BASE SYSTEM PROMPT") > strings.Index(sys, "always run go vet") {
		t.Fatalf("memory should follow the base prompt, not precede it:\n%s", sys)
	}

	if mem := ctrl.Memory(); mem == nil || len(mem.Docs) == 0 {
		t.Fatal("controller memory set is empty after discovering REASONIX.md")
	}
}

func TestNewProviderAppliesConfiguredDefaultEffort(t *testing.T) {
	var gotReq map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	p, err := NewProvider(&config.ProviderEntry{
		Name:             "custom",
		Kind:             "openai",
		BaseURL:          srv.URL,
		Model:            "m",
		SupportedEfforts: []string{"low", "medium", "high"},
		DefaultEffort:    "MEDIUM",
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	if got := gotReq["reasoning_effort"]; got != "medium" {
		t.Fatalf("reasoning_effort = %#v, want medium from default_effort", got)
	}
}

// TestBuildDiscoversSkills proves the skill wiring end-to-end: a project skill
// is discovered at boot, surfaced via Controller.Skills(), and its name folds
// into the cache-stable system prompt's "# Skills" index alongside a built-in.
func TestBuildDiscoversSkills(t *testing.T) {
	dir := robustTempDir(t)
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`)
	writeFile(t, dir, ".reasonix/skills/projskill.md", "---\ndescription: a project skill\n---\nplaybook")

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	var hasProj, hasBuiltin bool
	for _, s := range ctrl.Skills() {
		switch s.Name {
		case "projskill":
			hasProj = true
		case "explore":
			hasBuiltin = true
		}
	}
	if !hasProj || !hasBuiltin {
		t.Fatalf("Skills() should include the project skill and a built-in; got %v", ctrl.Skills())
	}

	sys := systemMessage(ctrl.History())
	if !strings.Contains(sys, "# Skills") {
		t.Fatalf("skills index missing from system prompt:\n%s", sys)
	}
	if !strings.Contains(sys, "projskill") || !strings.Contains(sys, "explore") {
		t.Fatalf("skill names missing from index:\n%s", sys)
	}
}

func TestAddBuiltinsWithWorkspaceRootKeepsSessionTools(t *testing.T) {
	reg := tool.NewRegistry()
	var stderr bytes.Buffer
	addBuiltins(reg, nil, []string{robustTempDir(t)}, sandbox.Spec{}, 120*time.Second, builtin.SearchSpec{}, &stderr, robustTempDir(t))
	for _, name := range []string{
		"todo_write",
		"complete_step",
		"bash_output",
		"kill_shell",
		"wait",
		"notebook_edit",
	} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("workspace builtins missing %q; got %v", name, reg.Names())
		}
	}
}

func TestBuildOmitsDisabledSkillsFromPromptAndRuntimeList(t *testing.T) {
	dir := robustTempDir(t)
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[skills]
disabled_skills = ["projskill", "review"]

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`)
	writeFile(t, dir, ".reasonix/skills/projskill.md", "---\ndescription: a project skill\n---\nplaybook")

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	for _, s := range ctrl.Skills() {
		if s.Name == "projskill" || s.Name == "review" {
			t.Fatalf("disabled skill %q should not be executable: %v", s.Name, ctrl.Skills())
		}
	}
	var allHasProj bool
	for _, s := range ctrl.AllSkills() {
		if s.Name == "projskill" {
			allHasProj = true
		}
	}
	if !allHasProj {
		t.Fatalf("AllSkills should include disabled skills for management: %v", ctrl.AllSkills())
	}
	sys := systemMessage(ctrl.History())
	if strings.Contains(sys, "projskill") || strings.Contains(sys, "- review ") {
		t.Fatalf("disabled skill names should be omitted from system prompt:\n%s", sys)
	}
}

func TestBuildOmitsExcludedSkillRootsFromPromptAndRuntimeList(t *testing.T) {
	dir := robustTempDir(t)
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Chdir(dir)
	excluded := filepath.Join(home, ".agents", "skills")
	writeFile(t, home, ".reasonix/skills/keep.md", "---\ndescription: keep\n---\nplaybook")
	writeFile(t, home, ".agents/skills/noisy.md", "---\ndescription: noisy\n---\nplaybook")
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[skills]
excluded_paths = [%q]

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`, excluded))

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	for _, s := range ctrl.Skills() {
		if s.Name == "noisy" {
			t.Fatalf("excluded skill should not be executable: %v", ctrl.Skills())
		}
	}
	sys := systemMessage(ctrl.History())
	if strings.Contains(sys, "noisy") {
		t.Fatalf("excluded skill name should be omitted from system prompt:\n%s", sys)
	}
	if !strings.Contains(sys, "keep") {
		t.Fatalf("non-excluded skill should remain in system prompt:\n%s", sys)
	}
}

// TestBuildWithoutMemoryLeavesPromptUnchanged is the inverse invariant: with no
// memory files, the system prompt is exactly the configured base — the cache
// prefix is untouched by the memory feature.
func TestBuildWithoutMemoryLeavesPromptUnchanged(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "JUST THE BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`)

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	// The built-in skills always append a "# Skills" index to the prefix; this
	// test is about memory, so strip that and assert the remaining base is exactly
	// the configured prompt — i.e. no *project/ancestor* memory leaked in. (A
	// user-global REASONIX.md in the real config dir could append; the test
	// environment has none, so the base stands alone.)
	base := sys
	if i := strings.Index(sys, "\n\n# Skills"); i >= 0 {
		base = sys[:i]
	}
	// The language policy is always appended at boot; strip it so this assertion
	// is purely about whether project/ancestor memory leaked into the base.
	base = stripLanguagePolicy(base)
	if base != "JUST THE BASE" {
		t.Fatalf("expected untouched base prompt, got:\n%s", sys)
	}
}

func TestBuildLanguagePolicyIsAppended(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`)

	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	if !strings.Contains(sys, config.LanguagePolicy) {
		t.Fatalf("language policy missing from system prompt:\n%s", sys)
	}
}

func systemMessage(msgs []provider.Message) string {
	for _, m := range msgs {
		if m.Role == provider.RoleSystem {
			return m.Content
		}
	}
	return ""
}

func stripLanguagePolicy(s string) string {
	s = strings.TrimSpace(s)
	for _, policy := range []string{
		config.LanguagePolicy,
	} {
		s = strings.TrimSpace(strings.TrimSuffix(s, policy))
	}
	return s
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := writeFileRaw(dir, name, body); err != nil {
		t.Fatal(err)
	}
}

func TestRememberPermissionRuleUsesWorkspaceRoot(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	cwd := robustTempDir(t)
	workspace := robustTempDir(t)
	t.Chdir(cwd)
	writeFile(t, cwd, "reasonix.toml", `
[permissions]
allow = ["bash(cwd*)"]
`)
	writeFile(t, workspace, "reasonix.toml", `
[permissions]
allow = ["bash(workspace*)"]
`)

	const rule = "bash=go test ./..."
	rememberPermissionRule(workspace, rule)

	cwdCfg := config.LoadForEdit(filepath.Join(cwd, "reasonix.toml"))
	if hasPermissionRule(cwdCfg.Permissions.Allow, rule) {
		t.Fatalf("remembered rule was written to cwd config: %v", cwdCfg.Permissions.Allow)
	}
	workspaceCfg := config.LoadForEdit(filepath.Join(workspace, "reasonix.toml"))
	if !hasPermissionRule(workspaceCfg.Permissions.Allow, rule) {
		t.Fatalf("remembered rule missing from workspace config: %v", workspaceCfg.Permissions.Allow)
	}
}

func TestRememberPermissionRuleEmptyRootUsesSourcePath(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	cwd := robustTempDir(t)
	t.Chdir(cwd)
	userConfig := config.UserConfigPath()
	writeFile(t, filepath.Dir(userConfig), filepath.Base(userConfig), `
[permissions]
allow = ["bash(user*)"]
`)

	const rule = "bash=go env"
	rememberPermissionRule("", rule)

	userCfg := config.LoadForEdit(userConfig)
	if !hasPermissionRule(userCfg.Permissions.Allow, rule) {
		t.Fatalf("empty root should remember into SourcePath config: %v", userCfg.Permissions.Allow)
	}
	if _, err := os.Stat(filepath.Join(cwd, "reasonix.toml")); !os.IsNotExist(err) {
		t.Fatalf("empty root should not create cwd config when SourcePath exists, err=%v", err)
	}
}

func hasPermissionRule(rules []string, want string) bool {
	for _, rule := range rules {
		if rule == want {
			return true
		}
	}
	return false
}

// TestBuildMigratesLegacyConfigEndToEnd drives the real boot path: a v0.x
// ~/.reasonix/config.json with no v1+ config present must be imported during
// Build — config written, key pinned into the env, and the user told via a notice.
func TestBuildMigratesLegacyConfigEndToEnd(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)                               // os.UserHomeDir on Windows
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config")) // os.UserConfigDir on Linux
	t.Setenv("AppData", filepath.Join(home, "AppData"))         // os.UserConfigDir on Windows
	t.Setenv("DEEPSEEK_API_KEY", "")                            // track for cleanup; migration os.Setenv's it live

	proj := robustTempDir(t)
	t.Chdir(proj)
	// codegraph off keeps Build offline; it merges over the migrated user config
	// without dropping the migrated plugins.
	writeFile(t, proj, "reasonix.toml", "[codegraph]\nenabled = false\n")
	writeFile(t, filepath.Join(home, ".reasonix"), "config.json",
		`{"apiKey":"sk-e2e","lang":"zh","mcpServers":{"fs":{"command":"npx","args":["-y","server-fs"]}}}`)
	writeFile(t, filepath.Join(home, ".reasonix", "sessions"), "chat-1.events.jsonl",
		`{"type":"user.message","id":1,"ts":"t","turn":0,"text":"hello from v0.x"}`+"\n"+
			`{"type":"model.final","id":2,"ts":"t","turn":0,"content":"hi","toolCalls":[],"usage":{},"costUsd":0}`+"\n")

	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})

	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	migrated := false
	for _, n := range notices {
		if strings.Contains(n, "migrated your previous configuration") {
			migrated = true
		}
	}
	if !migrated {
		t.Fatalf("no migration notice emitted; got %v", notices)
	}

	dest := config.UserConfigPath()
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("v2 config not written to %s: %v", dest, err)
	}
	if !strings.Contains(string(data), `name    = "fs"`) || !strings.Contains(string(data), `language      = "zh"`) {
		t.Errorf("migrated config missing plugin/lang:\n%s", data)
	}

	if got := os.Getenv("DEEPSEEK_API_KEY"); got != "sk-e2e" {
		t.Errorf("DEEPSEEK_API_KEY not pinned into env after migration: %q", got)
	}

	if data, err := os.ReadFile(config.UserCredentialsPath()); err != nil || !strings.Contains(string(data), "DEEPSEEK_API_KEY=sk-e2e") {
		t.Errorf("credentials store missing migrated key: %q (err %v)", data, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".env")); !os.IsNotExist(err) {
		t.Errorf("migration must not write the user's ~/.env, stat err=%v", err)
	}

	sessionImported := false
	for _, n := range notices {
		if strings.Contains(n, "imported") && strings.Contains(n, "past session") {
			sessionImported = true
		}
	}
	if !sessionImported {
		t.Errorf("no session-import notice emitted; got %v", notices)
	}
	migratedSession := filepath.Join(config.SessionDir(), "chat-1.jsonl")
	if _, err := os.Stat(migratedSession); err != nil {
		t.Errorf("legacy session not imported to %s: %v", migratedSession, err)
	}
}

func TestBuildMigratesLegacySessionsFromConfigSessionDir(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg-config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	proj := robustTempDir(t)
	writeFile(t, proj, "reasonix.toml", "[codegraph]\nenabled = false\n")

	legacyDir := config.SessionDir()
	writeFile(t, legacyDir, "custom-root.events.jsonl",
		`{"type":"user.message","id":1,"ts":"t","turn":0,"text":"hello from redirected config root"}`+"\n"+
			`{"type":"model.final","id":2,"ts":"t","turn":0,"content":"hi from redirected root","toolCalls":[],"usage":{},"costUsd":0}`+"\n")

	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})

	// Pass the project root via WorkspaceRoot instead of t.Chdir: changing the
	// process cwd into a t.TempDir makes Windows refuse to remove that dir during
	// test cleanup (the cwd counts as "in use"), which is the only thing this test
	// failed on. WorkspaceRoot loads the same config without touching the cwd.
	ctrl, err := Build(context.Background(), Options{Sink: sink, WorkspaceRoot: proj})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sessionPath := filepath.Join(config.SessionDir(), "custom-root.jsonl")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("legacy config-root session not imported to %s: %v", sessionPath, err)
	}
	if !strings.Contains(string(data), "hello from redirected config root") {
		t.Fatalf("migrated session missing legacy content:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(config.SessionDir(), ".legacy-imported.v0-events-config")); err != nil {
		t.Fatalf("config-root legacy import marker missing: %v", err)
	}
	sessionImported := false
	for _, n := range notices {
		if strings.Contains(n, "imported") && strings.Contains(n, "past session") && strings.Contains(n, legacyDir) {
			sessionImported = true
		}
	}
	if !sessionImported {
		t.Errorf("no config-root session-import notice emitted; got %v", notices)
	}
}

// isolateConfigHome redirects os.UserConfigDir() (and the cache subtree under
// it) at a per-test temp dir by overriding the env vars Go's stdlib reads —
// HOME on darwin, XDG_CONFIG_HOME on linux. Without this, Build's plugin path
// would persist startup stats and cached schemas into the developer's real
// ~/Library/Application Support tree and bleed state across tests. Mirrors the
// withTempCache helper in internal/plugin/stats_test.go.
func isolateConfigHome(t *testing.T) string {
	t.Helper()
	dir := robustTempDir(t)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// TestPartitionByTier pins the bucket assignment contract that the rest of
// boot.go's plugin orchestration depends on: each tier string maps to its own
// slice, the original order inside a tier is preserved (so /mcp status and
// stats land deterministically), an empty/missing tier defaults to background
// (connects after session start without blocking chat), and unknown non-empty
// values fall back to lazy so a typo never forces unwanted background connects.
func TestPartitionByTier(t *testing.T) {
	entries := []config.PluginEntry{
		{Name: "e1", Tier: "eager"},
		{Name: "l1", Tier: "lazy"},
		{Name: "b1", Tier: "background"},
		{Name: "default", Tier: ""}, // empty defaults to background
	}

	eager, lazy, bg := partitionByTier(entries)

	if len(eager) != 1 || eager[0].Name != "e1" {
		t.Fatalf("eager bucket = %+v, want [e1]", eager)
	}
	if len(bg) != 2 || bg[0].Name != "b1" || bg[1].Name != "default" {
		t.Fatalf("background bucket = %+v, want [b1, default] preserving input order", bg)
	}
	if len(lazy) != 1 || lazy[0].Name != "l1" {
		t.Fatalf("lazy bucket = %+v, want [l1]", lazy)
	}
}

func TestBuildMigratesLegacyEagerTierToBackground(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"

[[plugins]]
name = "legacy-eager"
command = "reasonix-missing-legacy-eager-mcp"
tier = "eager"
`)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	failures := waitForMCPFailure(t, ctrl.Host(), "legacy-eager", 2*time.Second)
	if len(failures) != 1 || failures[0].Name != "legacy-eager" {
		t.Fatalf("failures = %+v, want background startup failure for migrated legacy eager plugin", failures)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "reasonix.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\ntier") {
		t.Fatalf("legacy eager tier should be removed during load:\n%s", raw)
	}
}

func TestBuildMigratesLegacyLazyTierToBackground(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"

[[plugins]]
name = "legacy-lazy"
command = "reasonix-missing-legacy-lazy-mcp"
tier = "lazy"
`)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	failures := waitForMCPFailure(t, ctrl.Host(), "legacy-lazy", 2*time.Second)
	if len(failures) != 1 || failures[0].Name != "legacy-lazy" {
		t.Fatalf("failures = %+v, want background startup failure for migrated legacy lazy plugin", failures)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "reasonix.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\ntier") {
		t.Fatalf("legacy lazy tier should be removed during load:\n%s", raw)
	}
}

func TestBuildColdCodegraphStartsInBackground(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	launcher := writeCodegraphHelper(t, dir)
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"

[codegraph]
enabled = true
path = %q
tier = "background"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`, launcher))

	var notices []event.Event
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	if got := ctrl.Host().Failures(); len(got) != 0 {
		t.Fatalf("Host.Failures() = %+v, want empty for cold built-in codegraph background startup", got)
	}
	codegraphDir := filepath.Join(dir, ".codegraph")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(codegraphDir); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("cold codegraph init did not create .codegraph/: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	foundNotice := false
	for _, n := range notices {
		if strings.Contains(n.Text, "preparing code-intelligence tools in the background") {
			foundNotice = true
			break
		}
	}
	if !foundNotice {
		t.Fatalf("missing background warmup notice; got %+v", notices)
	}
}

func TestBuildMigratesLegacyEagerBeforeStatsDemotion(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	// Three samples above 2*budget — the rule in stats.go's Recommend triggers
	// when the trailing window is entirely over the threshold. Use 30s so even
	// future budget bumps stay below the threshold.
	for i := 0; i < 3; i++ {
		if err := plugin.RecordStartup("slowserver", 30*time.Second); err != nil {
			t.Fatalf("RecordStartup #%d: %v", i, err)
		}
	}

	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"

[[plugins]]
name = "slowserver"
command = "reasonix-missing-slow-mcp-binary"
tier = "eager"
`)

	var notices []event.Event
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	failures := waitForMCPFailure(t, ctrl.Host(), "slowserver", 2*time.Second)
	if len(failures) != 1 || failures[0].Name != "slowserver" {
		t.Fatalf("Host.Failures() = %+v, want background startup failure for migrated plugin", failures)
	}

	foundDemoteNotice := false
	for _, n := range notices {
		if strings.Contains(n.Text, "demoting to lazy") {
			foundDemoteNotice = true
			break
		}
	}
	if foundDemoteNotice {
		t.Fatalf("legacy tier should be migrated before demotion logic; got notices %+v", notices)
	}
}

func waitForMCPFailure(t *testing.T, h *plugin.Host, name string, timeout time.Duration) []plugin.Failure {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		failures := h.Failures()
		for _, f := range failures {
			if f.Name == name {
				return failures
			}
		}
		if time.Now().After(deadline) {
			return failures
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestHelperProcess is invoked as a subprocess by TestBuildEagerStartsAtBoot
// and TestBuildLazyDoesNotConnectAtBoot. It mirrors the minimal MCP stdio
// server in internal/plugin/plugin_test.go so the boot package can drive an
// end-to-end handshake without depending on the plugin package's test helper
// (Go's testing framework only re-invokes the binary of the test package
// currently running). The helper gates on GO_WANT_HELPER_PROCESS=1 so a
// normal `go test ./internal/boot/...` does not trip it.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var req struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue // notification: no response
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "mock", "version": "0"},
				"capabilities":    map[string]any{},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "echo",
				"description": "Echo back the message.",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"msg": map[string]any{"type": "string"}},
					"required":   []string{"msg"},
				},
			}}}
		}

		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}
		b, _ := json.Marshal(resp)
		os.Stdout.Write(append(b, '\n'))
	}
}

func writeCodegraphHelper(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "codegraph-helper")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	src := filepath.Join(dir, "codegraph-helper.go")
	if err := os.WriteFile(src, []byte(`package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "init" {
		_ = os.MkdirAll(filepath.Join(os.Args[2], ".codegraph"), 0o755)
		return
	}

	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var req struct {
			ID     *int            `+"`json:\"id\"`"+`
			Method string          `+"`json:\"method\"`"+`
			Params json.RawMessage `+"`json:\"params\"`"+`
		}
		if err := json.Unmarshal(line, &req); err != nil || req.ID == nil {
			continue
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "codegraph", "version": "0"},
				"capabilities":    map[string]any{},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "search",
				"description": "Search symbols.",
				"inputSchema": map[string]any{"type": "object"},
			}}}
		}

		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}
		b, _ := json.Marshal(resp)
		_, _ = os.Stdout.Write(append(b, '\n'))
	}
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", path, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build codegraph helper: %v\n%s", err, out)
	}
	return path
}
