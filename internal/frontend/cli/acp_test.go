package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/frontend/acp"

	"reasonix/internal/base/testenv"
	_ "reasonix/internal/tools/builtin"
)

const acpTestProviderKind = "acp-test-provider"

func init() {
	provider.Register(acpTestProviderKind, func(cfg provider.Config) (provider.Provider, error) {
		return &acpTestProvider{cfg: cfg}, nil
	})
}

func TestACPBuiltinToolsKeepSessionLevelBuiltins(t *testing.T) {
	dir := testenv.TempDir(t)
	tools := toolMap(acpBuiltinTools(&config.Config{}, dir, []string{dir}))
	for _, name := range []string{
		"todo_write",
		"complete_step",
		"bash_output",
		"kill_shell",
		"wait",
		"move_file",
		"notebook_edit",
	} {
		if tools[name] == nil {
			t.Fatalf("ACP workspace tools missing %q; got %v", name, toolNames(tools))
		}
	}
}

func TestACPInitializesWithoutAPIKey(t *testing.T) {
	isolateCLIConfigHome(t)
	t.Setenv("DEEPSEEK_API_KEY", "")
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.WriteString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}` + "\n")
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = r.Close()
	})

	out := captureStdout(t, func() {
		if rc := Run([]string{"--acp"}, "test-version"); rc != 0 {
			t.Fatalf("Run --acp initialize rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, `"protocolVersion":1`) || !strings.Contains(out, `"name":"reasonix"`) {
		t.Fatalf("initialize output = %s", out)
	}
}

func TestACPRejectsInvalidSupervisorFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--planner=maybe"},
		{"--sandbox-network=maybe"},
		{"--sandbox-bash=maybe"},
	} {
		if rc := acpCommand(args, "test-version"); rc != 2 {
			t.Fatalf("acpCommand(%v) rc = %d, want 2", args, rc)
		}
	}
}

func TestACPSupervisorRuntimeStateUsesHardOverrides(t *testing.T) {
	isolateCLIConfigHome(t)
	project := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
[agent]
planner_model = "configured-planner"

[sandbox]
network = true
bash = "off"

allow_write = ["../outside"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	approveWorkspace(t, project)
	off := false
	factory := &acpFactory{
		plannerOff: true, networkOverride: &off, bashOverride: "enforce", workspaceOnly: true,
	}
	state, err := factory.SessionRuntimeState(context.Background(), acp.SessionRuntimeStateParams{
		Cwd: project, Model: "configured-planner", RuntimeProfile: "balanced",
	})
	if err != nil {
		t.Fatalf("SessionRuntimeState: %v", err)
	}
	if state.PlannerMode != "off" || state.Sandbox.Mode != "enforce" || state.Sandbox.NetworkEnabled {
		t.Fatalf("runtime overrides = %+v", state)
	}
	if len(state.Sandbox.WriteRoots) != 1 || state.Sandbox.WriteRoots[0] != project || state.Sandbox.WorkspaceRoot != project {
		t.Fatalf("workspace confinement = %+v", state.Sandbox)
	}
}

func TestACPSupervisorRuntimeStateDegradesWhenSandboxIsUnavailable(t *testing.T) {
	isolateCLIConfigHome(t)
	project := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte("[sandbox]\nbash = \"enforce\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	approveWorkspace(t, project)
	unavailable := func() bool { return false }
	params := acp.SessionRuntimeStateParams{Cwd: project, RuntimeProfile: "balanced"}

	state, err := (&acpFactory{
		bashOverride: "enforce", sandboxAvailable: unavailable,
	}).SessionRuntimeState(context.Background(), params)
	if err != nil {
		t.Fatalf("default ACP startup must report unavailable sandbox instead of failing: %v", err)
	}
	if state.Sandbox.Mode != "enforce" || state.Sandbox.Available {
		t.Fatalf("degraded sandbox state = %+v", state.Sandbox)
	}

	_, err = (&acpFactory{
		bashOverride: "enforce", requireSandbox: true, sandboxAvailable: unavailable,
	}).SessionRuntimeState(context.Background(), params)
	if err == nil || !strings.Contains(err.Error(), "sandbox unavailable") {
		t.Fatalf("explicit enforce must fail closed, got %v", err)
	}
}

func TestEffectiveACPPlannerModeMatchesSelectedRuntime(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.PlannerModel = "planner/planner-model"
	cfg.Providers = []config.ProviderEntry{
		{Name: "executor", Kind: "openai", Model: "executor-model"},
		{Name: "planner", Kind: "openai", Model: "planner-model"},
	}
	if got := effectiveACPPlannerMode(cfg, false, "executor/executor-model", "balanced"); got != "on" {
		t.Fatalf("balanced split-model planner mode = %q, want on", got)
	}
	// "economy" resolves to balanced now, so it plans like one.
	if got := effectiveACPPlannerMode(cfg, false, "executor/executor-model", "economy"); got != "on" {
		t.Fatalf("economy planner mode = %q, want on", got)
	}
	if got := effectiveACPPlannerMode(cfg, false, "planner/planner-model", "balanced"); got != "off" {
		t.Fatalf("same-model planner mode = %q, want off", got)
	}
}

func TestACPFactoryLoadsSessionCwdProjectConfig(t *testing.T) {
	home := isolateCLIConfigHome(t)
	if _, err := config.SetCredential("REASONIX_TEST_KEY", "test-key"); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
	project := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
default_model = "local"

[[providers]]
name = "local"
kind = "acp-test-provider"
base_url = "http://example.invalid"
model = "fake-model"
api_key_env = "REASONIX_TEST_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	approveWorkspace(t, project)
	cmdDir := filepath.Join(project, ".reasonix", "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "acp-only.md"), []byte("ACP project command"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}

	ctrl, err := (&acpFactory{}).NewSession(context.Background(), acp.SessionParams{Cwd: project, Sink: event.Discard})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer ctrl.Close()

	for _, cmd := range ctrl.Commands() {
		if cmd.Name == "acp-only" {
			return
		}
	}
	t.Fatalf("ACP session did not load project command from cwd; commands=%v", ctrl.Commands())
}

func TestACPFactoryClearsEffortOverrideForUnsupportedModel(t *testing.T) {
	isolateCLIConfigHome(t)
	if _, err := config.SetCredential("REASONIX_TEST_KEY", "test-key"); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
	project := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
default_model = "reasoner/reasoning-model"

[[providers]]
name = "reasoner"
kind = "acp-test-provider"
base_url = "http://example.invalid"
model = "reasoning-model"
api_key_env = "REASONIX_TEST_KEY"
supported_efforts = ["low", "high"]

[[providers]]
name = "plain"
kind = "acp-test-provider"
base_url = "http://example.invalid"
model = "plain-model"
api_key_env = "REASONIX_TEST_KEY"
effort = "high"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	approveWorkspace(t, project)

	high := "high"
	state, err := (&acpFactory{}).SessionConfigState(context.Background(), acp.SessionConfigStateParams{
		Cwd:            project,
		Model:          "reasoner/reasoning-model",
		EffortOverride: &high,
	})
	if err != nil {
		t.Fatalf("reasoning SessionConfigState: %v", err)
	}
	if state.EffortOverride == nil || *state.EffortOverride != "high" {
		t.Fatalf("reasoning effort override = %v, want high", state.EffortOverride)
	}

	state, err = (&acpFactory{}).SessionConfigState(context.Background(), acp.SessionConfigStateParams{
		Cwd:            project,
		Model:          "plain/plain-model",
		EffortOverride: &high,
	})
	if err != nil {
		t.Fatalf("plain SessionConfigState: %v", err)
	}
	if _, ok := findACPConfigOption(state.ConfigOptions, "effort"); ok {
		t.Fatalf("plain model should not advertise effort option: %+v", state.ConfigOptions)
	}
	if state.EffortOverride == nil || *state.EffortOverride != "" {
		t.Fatalf("plain effort override = %v, want explicit empty override", state.EffortOverride)
	}
}

func TestACPFactoryAdvertisesAndNormalizesRuntimeProfiles(t *testing.T) {
	isolateCLIConfigHome(t)
	if _, err := config.SetCredential("REASONIX_TEST_KEY", "test-key"); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
	project := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
default_model = "local"

[[providers]]
name = "local"
kind = "acp-test-provider"
base_url = "http://example.invalid"
model = "fake-model"
api_key_env = "REASONIX_TEST_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	approveWorkspace(t, project)

	state, err := (&acpFactory{profile: "full"}).SessionConfigState(context.Background(), acp.SessionConfigStateParams{
		Cwd:            project,
		RuntimeProfile: "delivery",
	})
	if err != nil {
		t.Fatalf("SessionConfigState: %v", err)
	}
	work, ok := findACPConfigOption(state.ConfigOptions, "work_mode")
	if !ok || work.CurrentValue != "delivery" || state.RuntimeProfile != "delivery" || len(work.Options) != 3 {
		t.Fatalf("work mode state = %+v / %q, want delivery with 3 options", work, state.RuntimeProfile)
	}

	state, err = (&acpFactory{profile: "full"}).SessionConfigState(context.Background(), acp.SessionConfigStateParams{Cwd: project})
	if err != nil {
		t.Fatalf("default SessionConfigState: %v", err)
	}
	work, _ = findACPConfigOption(state.ConfigOptions, "work_mode")
	if work.CurrentValue != "balanced" || state.RuntimeProfile != "balanced" {
		t.Fatalf("legacy full profile = %+v / %q, want balanced", work, state.RuntimeProfile)
	}
}

func findACPConfigOption(options []acp.SessionConfigOption, id string) (acp.SessionConfigOption, bool) {
	for _, opt := range options {
		if opt.ID == id {
			return opt, true
		}
	}
	return acp.SessionConfigOption{}, false
}

func toolMap(tools []tool.Tool) map[string]tool.Tool {
	out := make(map[string]tool.Tool, len(tools))
	for _, t := range tools {
		out[t.Name()] = t
	}
	return out
}

func toolNames(tools map[string]tool.Tool) []string {
	out := make([]string, 0, len(tools))
	for name := range tools {
		out = append(out, name)
	}
	return out
}

type acpTestProvider struct {
	cfg provider.Config
}

func (p *acpTestProvider) Name() string { return p.cfg.Name }

func (p *acpTestProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestACPEffortForcedThinkingDescription(t *testing.T) {
	prev := i18n.CurrentLanguage()
	t.Cleanup(func() { i18n.DetectLanguage(prev) })
	cap := config.EffortCapability{Supported: true, Levels: []string{"auto", "minimal", "high"}}
	for _, lang := range []string{"en", "zh", "zh-TW"} {
		i18n.DetectLanguage(lang)
		for _, model := range []string{"glm-5.3", "glm-5.2"} {
			entry := &config.ProviderEntry{Kind: "openai", BaseURL: "https://api.z.ai/api/paas/v4", Model: model}
			for _, option := range acpEffortOptions(cap, entry) {
				want := ""
				if model == "glm-5.3" && option.Value == "minimal" {
					want = i18n.M.ArgEffortForcedOn
				}
				if option.Description != want {
					t.Errorf("%s/%s/%s: description = %q, want %q", lang, model, option.Value, option.Description, want)
				}
			}
		}
	}
}
