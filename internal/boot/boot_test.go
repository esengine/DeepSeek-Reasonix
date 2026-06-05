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

	// Blank imports register the provider kind and built-in tools the same way
	// cmd/reasonix's main does; without them Build sees an empty provider
	// registry and a bare tool set.
	_ "reasonix/internal/provider/openai"
	_ "reasonix/internal/tool/builtin"
)

// TestBuildFoldsProjectMemoryIntoSystemPrompt is the end-to-end proof of the
// cache-first wiring: a project REASONIX.md is discovered at boot and folded
// into the session's system message (the cached prefix), and the `remember`
// tool is registered. It builds a real Controller from a throwaway project dir.
func TestBuildFoldsProjectMemoryIntoSystemPrompt(t *testing.T) {
	dir := t.TempDir()
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
	dir := t.TempDir()
	home := t.TempDir()
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

func TestBuildOmitsDisabledSkillsFromPromptAndRuntimeList(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
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

func TestBuildRecordsMCPStartupFailure(t *testing.T) {
	dir := t.TempDir()
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
name = "missing"
command = "reasonix-missing-mcp-binary"
tier = "eager"
`)
	var notices []event.Event
	ctrl, err := Build(context.Background(), Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build should not fail when an MCP server is unavailable: %v", err)
	}
	defer ctrl.Close()
	failures := ctrl.Host().Failures()
	if len(failures) != 1 || failures[0].Name != "missing" {
		t.Fatalf("failures = %+v, want missing", failures)
	}
	foundNotice := false
	for _, n := range notices {
		if strings.Contains(n.Text, "failed to start") {
			foundNotice = true
			break
		}
	}
	if !foundNotice {
		t.Fatalf("missing startup warning notice: %+v", notices)
	}
}

// TestBuildWithoutMemoryLeavesPromptUnchanged is the inverse invariant: with no
// memory files, the system prompt is exactly the configured base — the cache
// prefix is untouched by the memory feature.
func TestBuildWithoutMemoryLeavesPromptUnchanged(t *testing.T) {
	dir := t.TempDir()
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
	dir := t.TempDir()
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	cwd := t.TempDir()
	workspace := t.TempDir()
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	cwd := t.TempDir()
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)                               // os.UserHomeDir on Windows
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config")) // os.UserConfigDir on Linux
	t.Setenv("AppData", filepath.Join(home, "AppData"))         // os.UserConfigDir on Windows
	t.Setenv("DEEPSEEK_API_KEY", "")                            // track for cleanup; migration os.Setenv's it live

	proj := t.TempDir()
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

// isolateConfigHome redirects os.UserConfigDir() (and the cache subtree under
// it) at a per-test temp dir by overriding the env vars Go's stdlib reads —
// HOME on darwin, XDG_CONFIG_HOME on linux. Without this, Build's plugin path
// would persist startup stats and cached schemas into the developer's real
// ~/Library/Application Support tree and bleed state across tests. Mirrors the
// withTempCache helper in internal/plugin/stats_test.go.
func isolateConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// TestPartitionByTier pins the bucket assignment contract that the rest of
// boot.go's plugin orchestration depends on: each tier string maps to its own
// slice, the original order inside a tier is preserved (so /mcp status and
// stats land deterministically), and a missing/unknown tier value falls into
// lazy — the project default that keeps adding a plugin from ever slowing the
// next launch.
func TestPartitionByTier(t *testing.T) {
	entries := []config.PluginEntry{
		{Name: "e1", Tier: "eager"},
		{Name: "l1", Tier: "lazy"},
		{Name: "b1", Tier: "background"},
		{Name: "default", Tier: ""}, // empty must default to lazy
	}

	eager, lazy, bg := partitionByTier(entries)

	if len(eager) != 1 || eager[0].Name != "e1" {
		t.Fatalf("eager bucket = %+v, want [e1]", eager)
	}
	if len(bg) != 1 || bg[0].Name != "b1" {
		t.Fatalf("background bucket = %+v, want [b1]", bg)
	}
	// Lazy holds both the explicit lazy entry and the default-fallback one, in
	// the order they appeared in the input — proves the empty-tier default
	// flows through partition without reshuffling.
	if len(lazy) != 2 || lazy[0].Name != "l1" || lazy[1].Name != "default" {
		t.Fatalf("lazy bucket = %+v, want [l1, default] preserving input order", lazy)
	}
}

// TestBuildEagerStartsAtBoot proves an eager-tier plugin actually completes
// its handshake on the boot critical path: Host.ServerNames() must include the
// plugin after Build returns. We point the plugin at this test binary running
// as a stdio MCP helper (see TestHelperProcess), so the spawn is real but
// deterministic and hermetic — no external MCP server required on PATH.
func TestBuildEagerStartsAtBoot(t *testing.T) {
	isolateConfigHome(t)
	dir := t.TempDir()
	t.Chdir(dir)

	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
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
name = "eagermock"
command = %q
args = ["-test.run=TestHelperProcess", "--"]
tier = "eager"
env = { GO_WANT_HELPER_PROCESS = "1" }
`, os.Args[0]))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	names := ctrl.Host().ServerNames()
	found := false
	for _, n := range names {
		if n == "eagermock" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("eager plugin missing from Host.ServerNames() = %v — boot did not block on its handshake", names)
	}
	if got := ctrl.Host().Failures(); len(got) != 0 {
		t.Fatalf("Host.Failures() = %+v, want empty for a healthy eager plugin", got)
	}
}

// TestBuildLazyDoesNotConnectAtBoot is the inverse of the eager test: a
// lazy-tier plugin must NOT trigger a subprocess handshake during Build, so
// the boot critical path stays empty even with a slow-to-spawn server in
// config. We assert through Host.ServerNames() (must not list the plugin) and
// Host.Failures() (lazy plugins never appear here — a failure surfaces only on
// first model use, not at boot). The placeholder tool registration itself is
// covered by internal/plugin/lazy_test.go's TestLazy* suite; the Registry has
// no public accessor on Controller, so at this layer we pin the load-bearing
// boot-time invariant — no spawn — rather than re-validating the placeholder.
func TestBuildLazyDoesNotConnectAtBoot(t *testing.T) {
	isolateConfigHome(t)
	dir := t.TempDir()
	t.Chdir(dir)

	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
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
name = "lazymock"
command = %q
args = ["-test.run=TestHelperProcess", "--"]
tier = "lazy"
env = { GO_WANT_HELPER_PROCESS = "1" }
`, os.Args[0]))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctrl, err := Build(ctx, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	for _, n := range ctrl.Host().ServerNames() {
		if n == "lazymock" {
			t.Fatalf("lazy plugin %q appeared in Host.ServerNames() — boot connected it eagerly", n)
		}
	}
	if got := ctrl.Host().Failures(); len(got) != 0 {
		t.Fatalf("Host.Failures() = %+v, want empty — lazy plugins must not even attempt a boot-time spawn", got)
	}
	// The configured plugin is still recognised as a configured-but-disconnected
	// server, the same signal /mcp uses to render its "lazy, will connect on
	// first use" line — proves the entry made it through partition into the
	// lazy bucket rather than being dropped.
	dconn := ctrl.DisconnectedMCPNames()
	foundDisconnected := false
	for _, n := range dconn {
		if n == "lazymock" {
			foundDisconnected = true
			break
		}
	}
	if !foundDisconnected {
		t.Fatalf("DisconnectedMCPNames() = %v, want it to include the lazy plugin (configured but not connected)", dconn)
	}
}

func TestBuildColdCodegraphStartsInBackground(t *testing.T) {
	isolateConfigHome(t)
	dir := t.TempDir()
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

// TestBuildAutoDemoteFromStats proves the Phase 5 telemetry → Phase 4 tier
// bridge: three consecutive over-budget startup samples must demote an
// eager-tier plugin to lazy at the *next* boot, so the user pays for a slow
// MCP server at most a handful of starts. We pre-seed three 30s samples for
// "slowserver" (well above 2 * DefaultStartupBudget = 10s) via the public
// RecordStartup API, then Build a config that declares "slowserver" eager and
// verify (a) boot did NOT block on its handshake — the plugin is absent from
// Host.ServerNames() — and (b) a Notice carrying the demote reason fired so
// the user understands why their explicit "eager" was ignored this session.
func TestBuildAutoDemoteFromStats(t *testing.T) {
	isolateConfigHome(t)
	dir := t.TempDir()
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

	for _, n := range ctrl.Host().ServerNames() {
		if n == "slowserver" {
			t.Fatalf("demoted plugin %q still appeared in Host.ServerNames() — auto-demote did not move it out of the eager bucket", n)
		}
	}
	// Crucially the missing-binary command never ran: a real eager attempt
	// would have surfaced as a Failure with a spawn error. An empty failures
	// list proves boot skipped the spawn entirely, not that it tried and
	// silently swallowed the error.
	if got := ctrl.Host().Failures(); len(got) != 0 {
		t.Fatalf("Host.Failures() = %+v, want empty — demoted plugin should never have been spawned", got)
	}

	foundDemoteNotice := false
	for _, n := range notices {
		if strings.Contains(n.Text, "demoting to lazy") {
			foundDemoteNotice = true
			break
		}
	}
	if !foundDemoteNotice {
		t.Fatalf("no demote notice surfaced; got %+v", notices)
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
