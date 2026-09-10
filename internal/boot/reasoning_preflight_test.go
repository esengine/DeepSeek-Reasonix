package boot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestRoleReasoningPreflightKeepsPlannerEffortIndependent(t *testing.T) {
	for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"} {
		cfg := config.Default()
		cfg.DefaultModel, cfg.Agent.PlannerModel = "go/"+model, "go/"+model
		cfg.Providers = []config.ProviderEntry{{Name: "go", Kind: "anthropic", BaseURL: "https://opencode.ai/zen/go", RequestURL: "https://opencode.ai/zen/go/v1/messages", Model: model, Thinking: "enabled", Effort: "max"}}
		disabled := "disabled"
		if err := preflightRoleReasoning(cfg, Options{EffortOverride: &disabled}, nil, false); err != nil {
			t.Fatal(err)
		}
		if cfg.Providers[0].Effort != "max" {
			t.Fatal("preflight mutated planner effort")
		}
		cfg.Agent.PlannerModel = "bad/" + model
		cfg.Providers = append(cfg.Providers, config.ProviderEntry{Name: "bad", Kind: "anthropic", BaseURL: "https://custom.example", Model: model, Thinking: "enabled", Effort: "max"})
		err := preflightRoleReasoning(cfg, Options{EffortOverride: &disabled}, nil, false)
		var role *RoleReasoningError
		var unsupported *provider.UnsupportedReasoningEffort
		if !errors.As(err, &role) || !errors.As(err, &unsupported) {
			t.Fatalf("missing typed role/adapter errors: %v", err)
		}
		if role.Role != "planner" || role.Effort != "max" || role.Source != "providers.bad.effort" || role.Kind != "anthropic" || !strings.Contains(err.Error(), "enabled disabled") {
			t.Fatalf("%+v", role)
		}
	}
}

func TestBuildRejectsRoleEffortBeforeCreatingSessionResources(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	t.Chdir(root)
	writeFile(t, root, "reasonix.toml", `default_model="exec/deepseek-v4-pro"
[agent]
planner_model="planner/deepseek-v4-pro"
[[providers]]
name="exec"
kind="openai"
base_url="https://opencode.ai/zen/go/v1"
model="deepseek-v4-pro"
[[providers]]
name="planner"
kind="anthropic"
base_url="https://custom.example"
model="deepseek-v4-pro"
thinking="enabled"
effort="max"
`)
	sessionDir := filepath.Join(root, "must-not-exist")
	disabled := "disabled"
	ctrl, err := Build(context.Background(), Options{WorkspaceRoot: root, SessionDir: sessionDir, EffortOverride: &disabled, Sink: event.Discard})
	var role *RoleReasoningError
	if ctrl != nil || !errors.As(err, &role) || role.Role != "planner" {
		t.Fatalf("%v %v", ctrl, err)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("session resources created before preflight: %v", err)
	}
}

func TestBuildRetainsTheSelectedRoleConfigurationSnapshot(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	cfg := config.Default()
	cfg.DefaultModel = "go/deepseek-v4-pro"
	cfg.Agent.PlannerModel = cfg.DefaultModel
	cfg.Providers = []config.ProviderEntry{{Name: "go", Kind: "openai", BaseURL: "https://opencode.ai/zen/go/v1", Model: "deepseek-v4-pro", Effort: "max"}}
	disabled := "disabled"
	if err := ValidateReasoningSnapshot(cfg, Options{EffortOverride: &disabled}); err != nil {
		t.Fatal(err)
	}
	// Another settings transaction wins on disk after selection was validated.
	writeFile(t, root, "reasonix.toml", `default_model="broken/model"
[[providers]]
name="broken"
kind="anthropic"
base_url="https://custom.example"
model="model"
thinking="enabled"
effort="max"
`)
	ctrl, err := Build(context.Background(), Options{WorkspaceRoot: root, ConfigSnapshot: cfg, EffortOverride: &disabled, Sink: event.Discard})
	if err != nil {
		t.Fatalf("assembly reloaded a different role snapshot: %v", err)
	}
	defer ctrl.Close()
	if cfg.Providers[0].Effort != "max" {
		t.Fatal("execution override mutated the planner snapshot")
	}
}

func TestRoleReasoningPreflightKeepsRuntimeSelectedVision(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	t.Setenv("CUSTOM_KEY", "sk-test")
	writeFile(t, root, "reasonix.toml", `default_model="custom/text"
[agent]
vision_model="auto"
[[providers]]
name="custom"
kind="openai"
base_url="https://example.invalid/v1"
api_key_env="CUSTOM_KEY"
models=["text","vision-pro"]
vision_models=["vision-pro"]
`)
	ctrl, err := Build(context.Background(), Options{WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatalf("vision_model=auto must not fail assembly: %v", err)
	}
	ctrl.Close()
	// A removed explicit vision reference keeps the executor usable, exactly as
	// the lazy image-input path did; an unsupported effort on a resolvable
	// vision reference is still rejected before any session exists.
	cfg := config.Default()
	cfg.DefaultModel = "custom/text"
	cfg.Agent.VisionModel = "gone/vision"
	cfg.Providers = []config.ProviderEntry{{Name: "custom", Kind: "openai", BaseURL: "https://example.invalid/v1", Models: []string{"text"}}}
	if err := preflightRoleReasoning(cfg, Options{}, nil, false); err != nil {
		t.Fatalf("removed vision reference blocked assembly: %v", err)
	}
	cfg.Agent.VisionModel = "vision/deepseek-v4-flash-vision-exp"
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{Name: "vision", Kind: "anthropic", BaseURL: "https://custom.example", Model: "deepseek-v4-flash-vision-exp", Thinking: "enabled", Effort: "max"})
	var role *RoleReasoningError
	if err := preflightRoleReasoning(cfg, Options{}, nil, false); !errors.As(err, &role) || role.Role != "vision" {
		t.Fatalf("invalid vision effort not reported: %v", err)
	}
}

func TestBuildFreezesCallerConfigurationSnapshot(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	if _, err := config.SetCredential("SNAPSHOT_KEY", "sk-first"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DefaultModel = "custom/text"
	cfg.Providers = []config.ProviderEntry{{Name: "custom", Kind: "openai", BaseURL: "https://example.invalid/v1", APIKeyEnv: "SNAPSHOT_KEY", Model: "text"}}
	ctrl, err := Build(context.Background(), Options{WorkspaceRoot: root, ConfigSnapshot: cfg, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	// A credential rotation after assembly must not reach the running runtime.
	if _, err := config.SetCredential("SNAPSHOT_KEY", "sk-rotated"); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers[0].APIKey(); got != "sk-first" {
		t.Fatalf("caller snapshot rereads credentials: %q", got)
	}
}
