package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/acp"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
	"reasonix/internal/tool"

	_ "reasonix/internal/tool/builtin"
)

const acpTestProviderKind = "acp-test-provider"

func init() {
	provider.Register(acpTestProviderKind, func(cfg provider.Config) (provider.Provider, error) {
		return &acpTestProvider{cfg: cfg}, nil
	})
}

func TestACPBuiltinToolsKeepSessionLevelBuiltins(t *testing.T) {
	dir := t.TempDir()
	tools := toolMap(acpBuiltinTools(&config.Config{}, dir, []string{dir}))
	for _, name := range []string{
		"todo_write",
		"complete_step",
		"bash_output",
		"kill_shell",
		"wait",
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

func TestACPFactoryLoadsSessionCwdProjectConfig(t *testing.T) {
	home := isolateCLIConfigHome(t)
	t.Setenv("REASONIX_TEST_KEY", "test-key")
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
default_model = "local"

[codegraph]
enabled = false

[[providers]]
name = "local"
kind = "acp-test-provider"
base_url = "http://example.invalid"
model = "fake-model"
api_key_env = "REASONIX_TEST_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}
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

func TestACPTaskProfileDefaults(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.SubagentModel = "default-model"
	cfg.Agent.SubagentEffort = "high"
	cfg.Agent.SubagentModels = map[string]string{"task": "task-model"}
	cfg.Agent.SubagentEfforts = map[string]string{"task": "max"}

	model, effort := acpTaskProfileDefaults(cfg)
	if model != "task-model" || effort != "max" {
		t.Fatalf("task profile defaults = %q/%q, want task-model/max", model, effort)
	}

	cfg.Agent.SubagentModels = nil
	cfg.Agent.SubagentEfforts = nil
	model, effort = acpTaskProfileDefaults(cfg)
	if model != "default-model" || effort != "high" {
		t.Fatalf("fallback task profile defaults = %q/%q, want default-model/high", model, effort)
	}
}

func TestACPSubagentProviderResolverHonorsProfile(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = []config.ProviderEntry{
		{
			Name:             "parent",
			Kind:             acpTestProviderKind,
			Model:            "parent-model",
			ContextWindow:    111,
			SupportedEfforts: []string{"low", "high"},
		},
		{
			Name:             "sub",
			Kind:             acpTestProviderKind,
			Models:           []string{"sub-model"},
			Default:          "sub-model",
			ContextWindow:    222,
			SupportedEfforts: []string{"low", "high"},
		},
	}
	parent, ok := cfg.ResolveModel("parent")
	if !ok {
		t.Fatal("parent model did not resolve")
	}

	resolve := newACPSubagentProviderResolver(cfg, parent, netclient.ProxySpec{})
	prov, _, ctxWin, err := resolve("sub/sub-model", "HIGH")
	if err != nil {
		t.Fatalf("resolve sub profile: %v", err)
	}
	got := prov.(*acpTestProvider).cfg
	if got.Model != "sub-model" || got.Extra["effort"] != "high" || ctxWin != 222 {
		t.Fatalf("resolved profile = model:%q effort:%v ctx:%d, want sub-model/high/222", got.Model, got.Extra["effort"], ctxWin)
	}

	prov, _, ctxWin, err = resolve("", "low")
	if err != nil {
		t.Fatalf("resolve effort-only profile: %v", err)
	}
	got = prov.(*acpTestProvider).cfg
	if got.Model != "parent-model" || got.Extra["effort"] != "low" || ctxWin != 111 {
		t.Fatalf("effort-only profile = model:%q effort:%v ctx:%d, want parent-model/low/111", got.Model, got.Extra["effort"], ctxWin)
	}
}

func TestACPSubagentProviderResolverRejectsInvalidEffort(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = []config.ProviderEntry{{
		Name:             "parent",
		Kind:             acpTestProviderKind,
		Model:            "parent-model",
		SupportedEfforts: []string{"low", "high"},
	}}
	parent, ok := cfg.ResolveModel("parent")
	if !ok {
		t.Fatal("parent model did not resolve")
	}

	resolve := newACPSubagentProviderResolver(cfg, parent, netclient.ProxySpec{})
	if _, _, _, err := resolve("", "max"); err == nil {
		t.Fatal("invalid effort should fail before ACP task falls back to the parent profile")
	}
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
