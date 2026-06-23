package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestSetDefaultModel(t *testing.T) {
	c := Default()
	if err := c.SetDefaultModel("deepseek-pro"); err != nil {
		t.Fatalf("set valid default: %v", err)
	}
	if c.DefaultModel != "deepseek-pro" {
		t.Errorf("default = %q, want deepseek-pro", c.DefaultModel)
	}
	if err := c.SetDefaultModel("nope"); err == nil {
		t.Error("expected error for unknown provider")
	}
	// "provider/model" form is also accepted: the /model picker stores the
	// full ref so a user can land on a non-default model under the same
	// provider across restarts.
	if err := c.SetDefaultModel("deepseek-pro/deepseek-v4-pro"); err != nil {
		t.Fatalf("set provider/model default: %v", err)
	}
	if c.DefaultModel != "deepseek-pro/deepseek-v4-pro" {
		t.Errorf("default = %q, want deepseek-pro/deepseek-v4-pro", c.DefaultModel)
	}
	if err := c.SetDefaultModel("deepseek-pro/missing"); err == nil {
		t.Error("expected error for unknown model under known provider")
	}
	if err := c.SetDefaultModel(""); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestUIThemeNormalizes(t *testing.T) {
	c := Default()
	for _, tt := range []struct {
		in   string
		want string
	}{
		{"", "auto"},
		{"AUTO", "auto"},
		{"dark", "dark"},
		{" light ", "light"},
		{"unknown", "auto"},
	} {
		c.UI.Theme = tt.in
		if got := c.UITheme(); got != tt.want {
			t.Errorf("UITheme(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestUIThemeStyleNormalizes(t *testing.T) {
	c := Default()
	for _, tt := range []struct {
		in   string
		want string
	}{
		{"", ""},
		{"AURORA", "aurora"},
		{" nocturne ", "nocturne"},
		{" glacier ", "glacier"},
		{"unknown", ""},
	} {
		c.UI.ThemeStyle = tt.in
		if got := c.UIThemeStyle(); got != tt.want {
			t.Errorf("UIThemeStyle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestUICloseBehaviorNormalizes(t *testing.T) {
	c := Default()
	for _, tt := range []struct {
		in   string
		want string
	}{
		{"", "background"},
		{"QUIT", "quit"},
		{"exit", "quit"},
		{" background ", "background"},
		{"hide", "background"},
		{"unknown", "background"},
	} {
		c.UI.CloseBehavior = tt.in
		if got := c.UICloseBehavior(); got != tt.want {
			t.Errorf("UICloseBehavior(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDesktopPreferencesAreSeparateFromCLI(t *testing.T) {
	c := Default()
	c.Language = "zh"
	c.UI.Theme = "light"
	c.UI.ThemeStyle = "glacier"

	if err := c.SetDesktopLanguage("en"); err != nil {
		t.Fatalf("SetDesktopLanguage: %v", err)
	}
	if err := c.SetDesktopAppearance("dark", "graphite"); err != nil {
		t.Fatalf("SetDesktopAppearance: %v", err)
	}
	if err := c.SetDesktopLayoutStyle("workbench"); err != nil {
		t.Fatalf("SetDesktopLayoutStyle: %v", err)
	}
	if err := c.SetDesktopStatusBarStyle("text"); err != nil {
		t.Fatalf("SetDesktopStatusBarStyle: %v", err)
	}
	if err := c.SetDesktopStatusBarItems([]string{"model", "balance", "cache"}); err != nil {
		t.Fatalf("SetDesktopStatusBarItems: %v", err)
	}

	if c.Language != "zh" {
		t.Fatalf("CLI language changed to %q", c.Language)
	}
	if got := c.UITheme(); got != "light" {
		t.Fatalf("CLI theme = %q, want light", got)
	}
	if got := c.UIThemeStyle(); got != "glacier" {
		t.Fatalf("CLI theme style = %q, want glacier", got)
	}
	if got := c.DesktopLanguage(); got != "en" {
		t.Fatalf("desktop language = %q, want en", got)
	}
	if got := c.DesktopTheme(); got != "dark" {
		t.Fatalf("desktop theme = %q, want dark", got)
	}
	if got := c.DesktopThemeStyle(); got != "graphite" {
		t.Fatalf("desktop theme style = %q, want graphite", got)
	}
	if got := c.DesktopLayoutStyle(); got != "workbench" {
		t.Fatalf("desktop layout style = %q, want workbench", got)
	}
	if got := c.DesktopStatusBarStyle(); got != "text" {
		t.Fatalf("desktop status bar style = %q, want text", got)
	}
	if got, want := c.DesktopStatusBarItems(), []string{"model", "balance", "cache"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("desktop status bar items = %v, want %v", got, want)
	}
}

func TestDesktopLayoutStyleNormalizes(t *testing.T) {
	if got := Default().DesktopLayoutStyle(); got != "workbench" {
		t.Fatalf("default desktop layout style = %q, want workbench", got)
	}
	for _, tt := range []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "classic", false},
		{"classic", "classic", false},
		{" workbench ", "workbench", false},
		{"workspace", "workbench", false},
		{"creation", "creation", false},
		{" Creation ", "creation", false},
		{"later", "workbench", true},
	} {
		c := Default()
		if err := c.SetDesktopLayoutStyle(tt.in); (err != nil) != tt.wantErr {
			t.Fatalf("SetDesktopLayoutStyle(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if got := c.DesktopLayoutStyle(); got != tt.want {
			t.Fatalf("DesktopLayoutStyle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}

	c := Default()
	c.Desktop.ThemeStyle = "workbench"
	if got := c.DesktopLayoutStyle(); got != "workbench" {
		t.Fatalf("legacy desktop theme_style=workbench layout = %q, want workbench", got)
	}
	if got := c.DesktopThemeStyle(); got != "" {
		t.Fatalf("legacy desktop theme_style=workbench theme style = %q, want empty", got)
	}
}

func TestDesktopStatusBarStyleNormalizes(t *testing.T) {
	if got := Default().DesktopStatusBarStyle(); got != "text" {
		t.Fatalf("default desktop status bar style = %q, want text", got)
	}
	for _, tt := range []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "text", false},
		{"icon", "icon", false},
		{"icons", "icon", false},
		{"text", "text", false},
		{"labels", "text", false},
		{"later", "text", true},
	} {
		c := Default()
		if err := c.SetDesktopStatusBarStyle(tt.in); (err != nil) != tt.wantErr {
			t.Fatalf("SetDesktopStatusBarStyle(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if got := c.DesktopStatusBarStyle(); got != tt.want {
			t.Fatalf("DesktopStatusBarStyle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDesktopStatusBarItemsNormalizeAndValidate(t *testing.T) {
	if got, want := Default().DesktopStatusBarItems(), DefaultDesktopStatusBarItems(); !reflect.DeepEqual(got, want) {
		t.Fatalf("default desktop status bar items = %v, want %v", got, want)
	}
	for _, id := range []string{"workspace", "git_branch"} {
		if !slices.Contains(DefaultDesktopStatusBarItems(), id) {
			t.Fatalf("default desktop status bar items must include configurable item %q", id)
		}
	}

	c := Default()
	c.Desktop.StatusBarItems = []string{" balance ", "cache", "cache", "unknown", "model"}
	if got, want := c.DesktopStatusBarItems(), []string{"balance", "cache", "model"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized desktop status bar items = %v, want %v", got, want)
	}

	c = Default()
	if err := c.SetDesktopStatusBarItems([]string{"balance", "cache", "balance", "model"}); err != nil {
		t.Fatalf("SetDesktopStatusBarItems subset: %v", err)
	}
	if got, want := c.DesktopStatusBarItems(), []string{"balance", "cache", "model"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("saved desktop status bar items = %v, want %v", got, want)
	}

	c = Default()
	if err := c.SetDesktopStatusBarItems([]string{"workspace", "git_branch", "model"}); err != nil {
		t.Fatalf("SetDesktopStatusBarItems workspace metadata: %v", err)
	}
	if got, want := c.DesktopStatusBarItems(), []string{"workspace", "git_branch", "model"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("saved workspace metadata status bar items = %v, want %v", got, want)
	}

	if err := c.SetDesktopStatusBarItems(nil); err != nil {
		t.Fatalf("SetDesktopStatusBarItems nil: %v", err)
	}
	if got, want := c.DesktopStatusBarItems(), DefaultDesktopStatusBarItems(); !reflect.DeepEqual(got, want) {
		t.Fatalf("nil desktop status bar items = %v, want default %v", got, want)
	}

	if err := c.SetDesktopStatusBarItems([]string{"ghost"}); err == nil {
		t.Fatal("expected error for unknown status bar item")
	}
}

func TestDesktopCloseBehaviorFallsBackToLegacyUI(t *testing.T) {
	c := Default()
	c.UI.CloseBehavior = "quit"
	if got := c.DesktopCloseBehavior(); got != "quit" {
		t.Fatalf("legacy close behavior = %q, want quit", got)
	}
	c.Desktop.CloseBehavior = "background"
	if got := c.DesktopCloseBehavior(); got != "background" {
		t.Fatalf("desktop close behavior = %q, want background", got)
	}
}

func TestSetUICloseBehavior(t *testing.T) {
	c := Default()
	if err := c.SetUICloseBehavior("background"); err != nil {
		t.Fatalf("SetUICloseBehavior background: %v", err)
	}
	if got := c.UICloseBehavior(); got != "background" {
		t.Fatalf("close behavior = %q, want background", got)
	}
	if err := c.SetUICloseBehavior("quit"); err != nil {
		t.Fatalf("SetUICloseBehavior quit: %v", err)
	}
	if got := c.UICloseBehavior(); got != "quit" {
		t.Fatalf("close behavior = %q, want quit", got)
	}
	if err := c.SetUICloseBehavior("later"); err == nil {
		t.Fatal("expected error for invalid close behavior")
	}
}

func TestSetPlannerModel(t *testing.T) {
	c := Default()
	if err := c.SetPlannerModel("deepseek-pro"); err != nil {
		t.Fatalf("set planner: %v", err)
	}
	if c.Agent.PlannerModel != "deepseek-pro" {
		t.Errorf("planner = %q", c.Agent.PlannerModel)
	}
	if err := c.SetPlannerModel(""); err != nil || c.Agent.PlannerModel != "" {
		t.Errorf("clearing planner failed: err=%v planner=%q", err, c.Agent.PlannerModel)
	}
	if err := c.SetPlannerModel("ghost"); err == nil {
		t.Error("expected error for unknown planner")
	}
}

func TestSetAutoPlan(t *testing.T) {
	c := Default()
	for _, mode := range []string{"on", "off"} {
		if err := c.SetAutoPlan(mode); err != nil {
			t.Fatalf("SetAutoPlan(%q): %v", mode, err)
		}
		if c.Agent.AutoPlan != mode {
			t.Fatalf("auto_plan = %q, want %q", c.Agent.AutoPlan, mode)
		}
	}
	if err := c.SetAutoPlan("ask"); err != nil {
		t.Fatalf("legacy ask should be accepted: %v", err)
	}
	if c.Agent.AutoPlan != "on" {
		t.Fatalf("legacy ask should save as on, got %q", c.Agent.AutoPlan)
	}
	if err := c.SetAutoPlan("auto"); err == nil {
		t.Fatal("expected error for invalid auto_plan mode")
	}
}

func TestSetUIShortcutLayout(t *testing.T) {
	c := Default()
	if got := c.UIShortcutLayout(); got != "classic" {
		t.Fatalf("default shortcut layout = %q, want classic", got)
	}
	if err := c.SetUIShortcutLayout("desktop"); err != nil {
		t.Fatalf("SetUIShortcutLayout desktop: %v", err)
	}
	if got := c.UIShortcutLayout(); got != "desktop" {
		t.Fatalf("shortcut layout = %q, want desktop", got)
	}
	if err := c.SetUIShortcutLayout("dual-axis"); err != nil {
		t.Fatalf("SetUIShortcutLayout alias: %v", err)
	}
	if got := c.UIShortcutLayout(); got != "desktop" {
		t.Fatalf("shortcut layout alias = %q, want desktop", got)
	}
	if err := c.SetUIShortcutLayout("classic"); err != nil {
		t.Fatalf("SetUIShortcutLayout classic: %v", err)
	}
	if got := c.UIShortcutLayout(); got != "classic" {
		t.Fatalf("shortcut layout = %q, want classic", got)
	}
	if err := c.SetUIShortcutLayout("surprise"); err == nil {
		t.Fatal("expected error for invalid shortcut layout")
	}
}

func TestUpsertProvider(t *testing.T) {
	c := Default()
	n := len(c.Providers)

	// Add a new one.
	if err := c.UpsertProvider(ProviderEntry{Name: "local", Kind: "openai", BaseURL: "http://localhost:1234/v1", Model: "x"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(c.Providers) != n+1 {
		t.Fatalf("provider count = %d, want %d", len(c.Providers), n+1)
	}

	// Replace it in place (no growth, position preserved).
	if err := c.UpsertProvider(ProviderEntry{Name: "local", Kind: "openai", BaseURL: "http://localhost:9999/v1", Model: "y"}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if len(c.Providers) != n+1 {
		t.Errorf("replace grew the list to %d", len(c.Providers))
	}
	got, _ := c.Provider("local")
	if got.BaseURL != "http://localhost:9999/v1" || got.Model != "y" {
		t.Errorf("replace didn't apply: %+v", got)
	}

	// Multi-model providers may omit the back-compat single model field.
	if err := c.UpsertProvider(ProviderEntry{
		Name:    "multi",
		Kind:    "openai",
		BaseURL: "http://localhost:8888/v1",
		Models:  []string{"m1", "m2"},
		Default: "m1",
	}); err != nil {
		t.Fatalf("multi-model add: %v", err)
	}

	// Missing required fields error.
	for _, bad := range []ProviderEntry{
		{Kind: "openai", BaseURL: "u", Model: "m"}, // no name
		{Name: "a", BaseURL: "u", Model: "m"},      // no kind
		{Name: "a", Kind: "openai", Model: "m"},    // no base_url
		{Name: "a", Kind: "openai", BaseURL: "u"},  // no model
	} {
		if err := c.UpsertProvider(bad); err == nil {
			t.Errorf("expected validation error for %+v", bad)
		}
	}
}

func TestSetProviderEffort(t *testing.T) {
	c := Default()
	if err := c.SetProviderEffort("deepseek-flash", "MAX"); err != nil {
		t.Fatalf("SetProviderEffort: %v", err)
	}
	p, _ := c.Provider("deepseek-flash")
	if p.Effort != "max" {
		t.Fatalf("effort = %q, want max", p.Effort)
	}
	if err := c.SetProviderEffort("missing", "high"); err == nil {
		t.Fatal("SetProviderEffort should reject unknown provider")
	}
}

func TestSetLanguage(t *testing.T) {
	c := Default()
	if err := c.SetLanguage("zh"); err != nil {
		t.Fatalf("SetLanguage zh: %v", err)
	}
	if c.Language != "zh" {
		t.Fatalf("language = %q, want zh", c.Language)
	}
	if err := c.SetLanguage("auto"); err != nil {
		t.Fatalf("SetLanguage auto: %v", err)
	}
	if c.Language != "" {
		t.Fatalf("language = %q, want cleared", c.Language)
	}
}

func TestSetReasoningLanguage(t *testing.T) {
	c := Default()
	if err := c.SetReasoningLanguage("中文"); err != nil {
		t.Fatalf("SetReasoningLanguage zh: %v", err)
	}
	if c.Agent.ReasoningLanguage != "zh" || c.ReasoningLanguage() != "zh" {
		t.Fatalf("reasoning language = %q/%q, want zh", c.Agent.ReasoningLanguage, c.ReasoningLanguage())
	}
	if err := c.SetReasoningLanguage("model-default"); err != nil {
		t.Fatalf("SetReasoningLanguage legacy default: %v", err)
	}
	if c.Agent.ReasoningLanguage != "" || c.ReasoningLanguage() != "auto" {
		t.Fatalf("legacy default should normalize to empty/auto, got %q/%q", c.Agent.ReasoningLanguage, c.ReasoningLanguage())
	}
	if err := c.SetReasoningLanguage("auto"); err != nil {
		t.Fatalf("SetReasoningLanguage auto: %v", err)
	}
	if c.Agent.ReasoningLanguage != "" || c.ReasoningLanguage() != "auto" {
		t.Fatalf("reasoning language = %q/%q, want empty/auto", c.Agent.ReasoningLanguage, c.ReasoningLanguage())
	}
	if err := c.SetReasoningLanguage("klingon"); err == nil {
		t.Fatal("SetReasoningLanguage should reject unknown values")
	}
}

func TestNormalizeEffortDeepSeek(t *testing.T) {
	e := &ProviderEntry{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4"}
	cap := EffortCapabilityForEntry(e)
	if !cap.Supported || len(cap.Levels) != 3 || cap.Levels[0] != "auto" || cap.Levels[1] != "high" || cap.Levels[2] != "max" {
		t.Fatalf("DeepSeek levels = %+v, want auto/high/max", cap)
	}
	for in, want := range map[string]string{"auto": "", "high": "high", "max": "max", "low": "high", "medium": "high", "xhigh": "max"} {
		got, err := NormalizeEffort(e, in)
		if err != nil || got != want {
			t.Fatalf("NormalizeEffort(%q) = %q/%v, want %q/nil", in, got, err, want)
		}
	}
	if _, err := NormalizeEffort(e, "off"); err == nil {
		t.Fatal("DeepSeek /effort must reject off")
	}
}

func TestNormalizeLegacyEffortMigratesProviderDefaults(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{Name: "deepseek", Effort: "off"},
		{Name: "deepseek-upper", Effort: "OFF"},
		{Name: "deepseek-auto", Effort: "auto"},
		{Name: "deepseek-auto-upper", Effort: "AUTO"},
		{Name: "keep", Effort: "high"},
	}}
	normalizeLegacyEffort(c)
	normalizeEffortConfig(c)
	if c.Providers[0].Effort != "" || c.Providers[1].Effort != "" || c.Providers[2].Effort != "" || c.Providers[3].Effort != "" {
		t.Fatalf("provider default efforts should migrate to empty, got %q/%q/%q/%q", c.Providers[0].Effort, c.Providers[1].Effort, c.Providers[2].Effort, c.Providers[3].Effort)
	}
	if c.Providers[4].Effort != "high" {
		t.Fatalf("non-legacy effort changed: %q", c.Providers[4].Effort)
	}
}

func TestNormalizeEffortAnthropic(t *testing.T) {
	e := &ProviderEntry{Name: "claude", Kind: "anthropic", Model: "claude-opus-4-8"}
	cap := EffortCapabilityForEntry(e)
	if !cap.Supported || len(cap.Levels) != 6 {
		t.Fatalf("Anthropic levels = %+v, want auto plus five levels", cap)
	}
	for _, level := range []string{"low", "medium", "high", "xhigh", "max"} {
		got, err := NormalizeEffort(e, level)
		if err != nil || got != level {
			t.Fatalf("NormalizeEffort(%q) = %q/%v, want %q/nil", level, got, err, level)
		}
	}
	got, err := NormalizeEffort(e, "auto")
	if err != nil || got != "" {
		t.Fatalf("NormalizeEffort(auto) = %q/%v, want empty/nil", got, err)
	}
}

func TestResolveModelPreservesProviderEffort(t *testing.T) {
	c := Default()
	c.Providers = append(c.Providers, ProviderEntry{
		Name:      "deepseek",
		Kind:      "openai",
		BaseURL:   "https://api.deepseek.com",
		Model:     "deepseek-v4-flash",
		Models:    []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		Default:   "deepseek-v4-flash",
		APIKeyEnv: "DEEPSEEK_API_KEY",
		Effort:    "max",
	})
	e, ok := c.ResolveModel("deepseek/deepseek-v4-pro")
	if !ok {
		t.Fatal("ResolveModel did not find deepseek/deepseek-v4-pro")
	}
	if e.Name != "deepseek" || e.Model != "deepseek-v4-pro" || e.Effort != "max" {
		t.Fatalf("resolved entry = %+v, want provider deepseek model deepseek-v4-pro effort max", e)
	}
}

func TestEffectiveVisionForMimoEndpointModels(t *testing.T) {
	c := Default()
	c.Providers = append(c.Providers, legacyMimoCustomProvider("mimo-api"))
	c.Desktop.ProviderAccess = []string{"mimo-api"}
	normalizeDesktopOfficialProviderAccess(c)

	pro, ok := c.ResolveModel("mimo-api/mimo-v2.5-pro")
	if !ok {
		t.Fatal("ResolveModel did not find mimo-api/mimo-v2.5-pro")
	}
	if EffectiveVision(pro) {
		t.Fatalf("mimo-v2.5-pro should remain text-only by default")
	}

	vision, ok := c.ResolveModel("mimo-api/mimo-v2.5")
	if !ok {
		t.Fatal("ResolveModel did not find mimo-api/mimo-v2.5")
	}
	if !EffectiveVision(vision) {
		t.Fatalf("mimo-v2.5 on the official MiMo API should enable vision")
	}

	omni, ok := c.ResolveModel("mimo-api/mimo-v2-omni")
	if !ok {
		t.Fatal("ResolveModel did not find mimo-api/mimo-v2-omni")
	}
	if !EffectiveVision(omni) {
		t.Fatalf("mimo-v2-omni on the official MiMo API should enable vision")
	}
}

func TestEffectiveVisionDoesNotInferCustomMimoProxy(t *testing.T) {
	custom := &ProviderEntry{
		Name:    "mimo-proxy",
		Kind:    "openai",
		BaseURL: "https://proxy.example.com/v1",
		Model:   "mimo-v2.5",
	}
	if EffectiveVision(custom) {
		t.Fatalf("custom MiMo proxy should require explicit vision=true")
	}
	custom.Vision = true
	if !EffectiveVision(custom) {
		t.Fatalf("explicit vision=true should still enable custom providers")
	}
}

func TestEffectiveVisionUsesPerModelVisionList(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name:         "custom",
		Kind:         "openai",
		BaseURL:      "https://proxy.example.com/v1",
		Models:       []string{"text-only", "qwen-vl-plus"},
		Default:      "text-only",
		VisionModels: []string{"qwen-vl-plus"},
	}}}

	textOnly, ok := c.ResolveModel("custom/text-only")
	if !ok {
		t.Fatal("ResolveModel did not find custom/text-only")
	}
	if EffectiveVision(textOnly) {
		t.Fatalf("text-only should remain text-only when not listed in vision_models")
	}

	vision, ok := c.ResolveModel("custom/qwen-vl-plus")
	if !ok {
		t.Fatal("ResolveModel did not find custom/qwen-vl-plus")
	}
	if !EffectiveVision(vision) {
		t.Fatalf("model listed in vision_models should enable image input")
	}

	textOnly.Vision = true
	if !EffectiveVision(textOnly) {
		t.Fatalf("provider-level vision=true should still enable every selected model")
	}
}

func TestRemoveProvider(t *testing.T) {
	c := Default()
	c.Agent.PlannerModel = "deepseek-pro"

	// Cannot remove the default model when no configured fallback is available.
	for i := range c.Providers {
		c.Providers[i].APIKeyEnv = ""
	}
	if err := c.RemoveProvider(c.DefaultModel); err == nil {
		t.Error("expected error removing the default model")
	}
	// Removing the planner provider clears planner_model.
	if err := c.RemoveProvider("deepseek-pro"); err != nil {
		t.Fatalf("remove planner provider: %v", err)
	}
	if c.Agent.PlannerModel != "" {
		t.Errorf("planner should be cleared, got %q", c.Agent.PlannerModel)
	}
	if _, ok := c.Provider("deepseek-pro"); ok {
		t.Error("provider not actually removed")
	}
	// Unknown name errors.
	if err := c.RemoveProvider("ghost"); err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestPermissionMutators(t *testing.T) {
	c := Default()

	if err := c.SetPermissionMode("DENY"); err != nil || c.Permissions.Mode != "deny" {
		t.Errorf("set mode: err=%v mode=%q", err, c.Permissions.Mode)
	}
	if err := c.SetPermissionMode("nonsense"); err == nil {
		t.Error("expected error for bad mode")
	}

	if err := c.AddPermissionRule("deny", "Bash(rm -rf*)"); err != nil {
		t.Fatalf("add deny: %v", err)
	}
	// Duplicate is a no-op, not an error or a second entry.
	if err := c.AddPermissionRule("deny", "Bash(rm -rf*)"); err != nil {
		t.Fatalf("dup add: %v", err)
	}
	if len(c.Permissions.Deny) != 1 {
		t.Errorf("deny list = %v, want one entry", c.Permissions.Deny)
	}
	// Invalid rule and unknown list both error.
	if err := c.AddPermissionRule("deny", "  "); err == nil {
		t.Error("expected error for empty rule")
	}
	if err := c.AddPermissionRule("nope", "read_file"); err == nil {
		t.Error("expected error for unknown list")
	}

	removed, err := c.RemovePermissionRule("deny", "Bash(rm -rf*)")
	if err != nil || !removed {
		t.Errorf("remove: removed=%v err=%v", removed, err)
	}
	if removed, _ := c.RemovePermissionRule("deny", "absent"); removed {
		t.Error("removing absent rule should report false")
	}
}

func TestSkillPathMutators(t *testing.T) {
	c := Default()
	root := t.TempDir()
	if err := c.ExcludeSkillPath(root); err != nil {
		t.Fatalf("exclude skill path: %v", err)
	}
	if err := c.AddSkillPath(root); err != nil {
		t.Fatalf("add skill path: %v", err)
	}
	if len(c.Skills.ExcludedPaths) != 0 {
		t.Fatalf("add skill path should restore excluded path, got %v", c.Skills.ExcludedPaths)
	}
	if err := c.AddSkillPath(filepath.Join(root, ".")); err != nil {
		t.Fatalf("duplicate skill path: %v", err)
	}
	if len(c.Skills.Paths) != 1 {
		t.Fatalf("paths = %v, want one deduped entry", c.Skills.Paths)
	}
	if err := c.AddSkillPath(" "); err == nil {
		t.Fatal("empty skill path should error")
	}
	removed, err := c.RemoveSkillPath(filepath.Join(root, "."))
	if err != nil || !removed {
		t.Fatalf("remove skill path: removed=%v err=%v", removed, err)
	}
	if len(c.Skills.Paths) != 0 {
		t.Fatalf("paths after remove = %v", c.Skills.Paths)
	}
	if removed, err := c.RemoveSkillPath(root); err != nil || removed {
		t.Fatalf("remove absent: removed=%v err=%v", removed, err)
	}
	if err := c.ExcludeSkillPath(filepath.Join(root, ".")); err != nil {
		t.Fatalf("exclude skill path: %v", err)
	}
	if err := c.ExcludeSkillPath(root); err != nil {
		t.Fatalf("duplicate exclude skill path: %v", err)
	}
	if len(c.Skills.ExcludedPaths) != 1 {
		t.Fatalf("excluded paths = %v, want one deduped entry", c.Skills.ExcludedPaths)
	}
	if err := c.ExcludeSkillPath(" "); err == nil {
		t.Fatal("empty excluded skill path should error")
	}
	if err := c.RestoreSkillPath(root); err != nil {
		t.Fatalf("restore skill path: %v", err)
	}
	if len(c.Skills.ExcludedPaths) != 0 {
		t.Fatalf("excluded paths after restore = %v, want empty", c.Skills.ExcludedPaths)
	}
	if err := c.RestoreSkillPath(" "); err == nil {
		t.Fatal("empty restored skill path should error")
	}
}

func TestSkillEnabledMutator(t *testing.T) {
	c := Default()
	if err := c.SetSkillEnabled("review", false); err != nil {
		t.Fatalf("disable skill: %v", err)
	}
	if err := c.SetSkillEnabled("review", false); err != nil {
		t.Fatalf("disable duplicate skill: %v", err)
	}
	if len(c.Skills.DisabledSkills) != 1 || c.Skills.DisabledSkills[0] != "review" {
		t.Fatalf("disabled skills = %v, want [review]", c.Skills.DisabledSkills)
	}
	if !c.IsSkillDisabled("review") {
		t.Fatal("review should be disabled")
	}
	if err := c.SetSkillEnabled("review", true); err != nil {
		t.Fatalf("enable skill: %v", err)
	}
	if len(c.Skills.DisabledSkills) != 0 {
		t.Fatalf("disabled skills after enable = %v, want empty", c.Skills.DisabledSkills)
	}
	if err := c.SetSkillEnabled("bad name", false); err == nil {
		t.Fatal("invalid skill name should error")
	}
}

func TestPluginMutators(t *testing.T) {
	c := Default()

	if err := c.UpsertPlugin(PluginEntry{Name: "ex", Command: "reasonix-plugin-example"}); err != nil {
		t.Fatalf("add stdio: %v", err)
	}
	if err := c.UpsertPlugin(PluginEntry{Name: "stripe", Type: "http", URL: "https://mcp.stripe.com"}); err != nil {
		t.Fatalf("add http: %v", err)
	}
	if len(c.Plugins) != 2 {
		t.Fatalf("plugin count = %d, want 2", len(c.Plugins))
	}

	// Transport validation: stdio needs command, http needs url.
	if err := c.UpsertPlugin(PluginEntry{Name: "bad"}); err == nil {
		t.Error("stdio without command should error")
	}
	if err := c.UpsertPlugin(PluginEntry{Name: "bad", Type: "http"}); err == nil {
		t.Error("http without url should error")
	}
	if err := c.UpsertPlugin(PluginEntry{Name: "bad", Type: "carrier-pigeon", Command: "x"}); err == nil {
		t.Error("unknown transport should error")
	}

	// Replace in place.
	if err := c.UpsertPlugin(PluginEntry{Name: "ex", Command: "other-cmd"}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if len(c.Plugins) != 2 {
		t.Errorf("replace grew plugins to %d", len(c.Plugins))
	}

	if !c.RemovePlugin("ex") {
		t.Error("remove should report true")
	}
	if c.RemovePlugin("ex") {
		t.Error("second remove should report false")
	}
}

func TestAutoStartPlugins(t *testing.T) {
	c := Default()
	off := false
	on := true
	c.Plugins = []PluginEntry{
		{Name: "implicit", Command: "implicit-bin"},
		{Name: "disabled", Command: "disabled-bin", AutoStart: &off},
		{Name: "enabled", Command: "enabled-bin", AutoStart: &on},
	}
	got := c.AutoStartPlugins()
	if len(got) != 2 || got[0].Name != "implicit" || got[1].Name != "enabled" {
		t.Fatalf("AutoStartPlugins = %+v, want implicit + enabled", got)
	}
}

func TestPluginResolvedTierDefaultsToBackground(t *testing.T) {
	for _, tc := range []struct {
		name string
		tier string
		want string
	}{
		{name: "empty", tier: "", want: "background"},
		{name: "legacy lazy", tier: "lazy", want: "background"},
		{name: "background", tier: "background", want: "background"},
		{name: "eager", tier: "eager", want: "eager"},
		{name: "unknown", tier: "startup", want: "background"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (PluginEntry{Name: "mcp", Command: "mcp-server", Tier: tc.tier}).ResolvedTier()
			if got != tc.want {
				t.Fatalf("ResolvedTier(%q) = %q, want %q", tc.tier, got, tc.want)
			}
		})
	}
}

func TestClearPluginAuthentication(t *testing.T) {
	c := Default()
	c.Plugins = []PluginEntry{{
		Name: "dida",
		Type: "http",
		URL:  "https://mcp.dida365.com/mcp?access_token=abc&workspace=main",
		Headers: map[string]string{
			"Authorization": "Bearer ${DIDA_TOKEN}",
			"X-Org":         "team",
		},
		Env: map[string]string{
			"DIDA_TOKEN": "${DIDA_TOKEN}",
			"DEBUG":      "1",
		},
		Tier: "lazy",
	}}
	updated, changed, err := c.ClearPluginAuthentication("dida")
	if err != nil {
		t.Fatalf("ClearPluginAuthentication: %v", err)
	}
	if !changed {
		t.Fatal("ClearPluginAuthentication should report changed")
	}
	if updated.URL != "https://mcp.dida365.com/mcp?workspace=main" {
		t.Fatalf("url = %q", updated.URL)
	}
	if _, ok := updated.Headers["Authorization"]; ok {
		t.Fatalf("auth header should be removed: %v", updated.Headers)
	}
	if updated.Headers["X-Org"] != "team" {
		t.Fatalf("ordinary header should be preserved: %v", updated.Headers)
	}
	if _, ok := updated.Env["DIDA_TOKEN"]; ok {
		t.Fatalf("auth env should be removed: %v", updated.Env)
	}
	if updated.Env["DEBUG"] != "1" {
		t.Fatalf("ordinary env should be preserved: %v", updated.Env)
	}
}

// TestSaveToRoundTrips stages several mutations, persists atomically, and
// re-decodes the file to confirm the changes survived a write/read cycle.
func TestSaveToRoundTrips(t *testing.T) {
	c := Default()
	if err := c.SetDefaultModel("deepseek-pro"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetPlannerModel("deepseek-pro"); err != nil {
		t.Fatal(err)
	}
	if err := c.UpsertProvider(ProviderEntry{Name: "local", Kind: "openai", BaseURL: "http://localhost:1234/v1", Model: "llama"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetPermissionMode("deny"); err != nil {
		t.Fatal(err)
	}
	if err := c.AddPermissionRule("allow", "Bash(go test:*)"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetNetwork(NetworkConfig{
		ProxyMode: "custom",
		Proxy: NetworkProxyConfig{
			Type:   "socks5",
			Server: "127.0.0.1",
			Port:   7890,
		},
	}); err != nil {
		t.Fatal(err)
	}
	autoStart := false
	if err := c.UpsertPlugin(PluginEntry{Name: "stripe", Type: "http", URL: "https://mcp.stripe.com", AutoStart: &autoStart}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "nested", "reasonix.toml")
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	var got Config
	if _, err := toml.DecodeFile(path, &got); err != nil {
		t.Fatalf("saved file does not parse: %v", err)
	}
	if got.DefaultModel != "deepseek-pro" {
		t.Errorf("default_model = %q", got.DefaultModel)
	}
	if got.Agent.PlannerModel != "deepseek-pro" {
		t.Errorf("planner_model = %q", got.Agent.PlannerModel)
	}
	if _, ok := got.Provider("local"); !ok {
		t.Error("added provider 'local' missing after round-trip")
	}
	if got.Permissions.Mode != "deny" {
		t.Errorf("mode = %q", got.Permissions.Mode)
	}
	if len(got.Permissions.Allow) != 1 || got.Permissions.Allow[0] != "Bash(go test:*)" {
		t.Errorf("allow list = %v", got.Permissions.Allow)
	}
	if got.Network.ProxyMode != "custom" || got.Network.Proxy.Server != "127.0.0.1" || got.Network.Proxy.Port != 7890 {
		t.Errorf("network = %+v", got.Network)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Name != "stripe" {
		t.Errorf("plugins = %+v", got.Plugins)
	}
	if got.Plugins[0].AutoStart == nil || *got.Plugins[0].AutoStart {
		t.Errorf("auto_start should round-trip false, got %+v", got.Plugins[0].AutoStart)
	}
}

func TestSaveToScopesUserAndProjectFiles(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := Default()
	c.Desktop.Theme = "dark"
	c.Desktop.ThemeStyle = "graphite"
	c.Desktop.CloseBehavior = "background"

	userPath := UserConfigPath()
	if err := c.SaveTo(userPath); err != nil {
		t.Fatalf("SaveTo user config: %v", err)
	}
	userBody, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatalf("read user config: %v", err)
	}
	if !strings.Contains(string(userBody), "[desktop]") {
		t.Fatalf("user config should include desktop preferences:\n%s", userBody)
	}
	if info, err := os.Stat(userPath); err != nil {
		t.Fatalf("stat user config: %v", err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("user config mode = %o, want 600", info.Mode().Perm())
	}

	projectPath := filepath.Join(t.TempDir(), "reasonix.toml")
	if err := c.SaveTo(projectPath); err != nil {
		t.Fatalf("SaveTo project config: %v", err)
	}
	projectBody, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if strings.Contains(string(projectBody), "[desktop]") || strings.Contains(string(projectBody), "close_behavior") {
		t.Fatalf("project config should not include desktop preferences:\n%s", projectBody)
	}
	if info, err := os.Stat(projectPath); err != nil {
		t.Fatalf("stat project config: %v", err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Fatalf("project config mode = %o, want 644", info.Mode().Perm())
	}
}

func TestLoadForRootKeepsOfficialProviderAliasesDistinct(t *testing.T) {
	isolateUserConfigHome(t)
	root := t.TempDir()
	userPath := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`
config_version = 2
default_model = "deepseek/deepseek-v4-flash"

[desktop]
provider_access = ["deepseek"]

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
api_key_env = "USER_DEEPSEEK_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(`
[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "PROJECT_DEEPSEEK_KEY"
effort = "max"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	userProvider, ok := cfg.Provider("deepseek")
	if !ok {
		t.Fatalf("user deepseek provider missing: %+v", cfg.Providers)
	}
	if userProvider.APIKeyEnv != "USER_DEEPSEEK_KEY" {
		t.Fatalf("deepseek provider = %+v, want user provider preserved", userProvider)
	}
	projectProvider, ok := cfg.Provider("deepseek-flash")
	if !ok {
		t.Fatalf("project deepseek-flash provider missing: %+v", cfg.Providers)
	}
	if projectProvider.APIKeyEnv != "PROJECT_DEEPSEEK_KEY" || projectProvider.Effort != "max" {
		t.Fatalf("deepseek-flash provider = %+v, want project provider preserved", projectProvider)
	}
}

func TestLoadForRootKeepsUserProviderOverSameNamedProjectProvider(t *testing.T) {
	isolateUserConfigHome(t)
	root := t.TempDir()
	userPath := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`
[[providers]]
name = "shared"
kind = "openai"
base_url = "https://global.example/v1"
model = "global-model"
api_key_env = "GLOBAL_SHARED_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(`
[[providers]]
name = "shared"
kind = "openai"
base_url = "https://project.example/v1"
model = "project-model"
api_key_env = "PROJECT_SHARED_KEY"

[[providers]]
name = "project-only"
kind = "openai"
base_url = "https://project.example/v1"
model = "project-only-model"
api_key_env = "PROJECT_ONLY_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	shared, ok := cfg.Provider("shared")
	if !ok {
		t.Fatalf("shared provider missing: %+v", cfg.Providers)
	}
	if shared.BaseURL != "https://global.example/v1" || shared.APIKeyEnv != "GLOBAL_SHARED_KEY" || shared.Model != "global-model" {
		t.Fatalf("shared provider = %+v, want global provider to win over project provider", shared)
	}
	if _, ok := cfg.Provider("project-only"); !ok {
		t.Fatalf("project-only provider missing: %+v", cfg.Providers)
	}
}

func TestLoadForRootKeepsGlobalAgentStepLimitsOverProject(t *testing.T) {
	isolateUserConfigHome(t)
	root := t.TempDir()
	userPath := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`
[agent]
max_steps = 17
planner_max_steps = 9
temperature = 0.4
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(`
default_model = "deepseek-pro"

[agent]
max_steps = 3
planner_max_steps = 4
temperature = 0.8
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	if cfg.Agent.MaxSteps != 17 || cfg.Agent.PlannerMaxSteps != 9 {
		t.Fatalf("agent steps = max:%d planner:%d, want global 17/9", cfg.Agent.MaxSteps, cfg.Agent.PlannerMaxSteps)
	}
	if cfg.Agent.Temperature != 0.8 {
		t.Fatalf("agent temperature = %v, want project override to keep working for other agent settings", cfg.Agent.Temperature)
	}
	if cfg.DefaultModel != "deepseek-pro" {
		t.Fatalf("default_model = %q, want project config to keep overriding unrelated fields", cfg.DefaultModel)
	}
}

func TestLoadForRootIgnoresProjectAgentStepLimitsWithoutUserConfig(t *testing.T) {
	isolateUserConfigHome(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(`
[agent]
max_steps = 3
planner_max_steps = 4
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	if cfg.Agent.MaxSteps != 0 || cfg.Agent.PlannerMaxSteps != 0 {
		t.Fatalf("agent steps = max:%d planner:%d, want built-in global defaults 0/0", cfg.Agent.MaxSteps, cfg.Agent.PlannerMaxSteps)
	}
}

func TestSaveForRootPreservesShadowedProjectProvider(t *testing.T) {
	isolateUserConfigHome(t)
	root := t.TempDir()
	userPath := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`
[[providers]]
name = "shared"
kind = "openai"
base_url = "https://global.example/v1"
model = "global-model"
api_key_env = "GLOBAL_SHARED_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(projectPath, []byte(`
[[providers]]
name = "shared"
kind = "openai"
base_url = "https://project.example/v1"
model = "project-model"
api_key_env = "PROJECT_SHARED_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	if err := cfg.SaveForRoot(root); err != nil {
		t.Fatalf("SaveForRoot: %v", err)
	}
	var saved Config
	if _, err := toml.DecodeFile(projectPath, &saved); err != nil {
		t.Fatalf("saved project config does not parse: %v", err)
	}
	shared, ok := saved.Provider("shared")
	if !ok {
		t.Fatalf("saved project provider missing: %+v", saved.Providers)
	}
	if shared.BaseURL != "https://project.example/v1" || shared.APIKeyEnv != "PROJECT_SHARED_KEY" {
		t.Fatalf("saved provider = %+v, want original project provider", shared)
	}
}

func TestSaveForRootDoesNotWriteUserProvidersIntoProjectConfig(t *testing.T) {
	isolateUserConfigHome(t)
	root := t.TempDir()
	userPath := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`
config_version = 2

[[providers]]
name = "global"
kind = "openai"
base_url = "https://global.example/v1"
model = "global-model"
api_key_env = "GLOBAL_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(projectPath, []byte(`
config_version = 2
default_model = "project-local/project-model"

[[providers]]
name = "project-local"
kind = "openai"
base_url = "https://project.example/v1"
model = "project-model"
api_key_env = "PROJECT_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	if _, ok := cfg.Provider("global"); !ok {
		t.Fatal("runtime config should include user provider before saving")
	}
	if _, ok := cfg.Provider("project-local"); !ok {
		t.Fatal("runtime config should include project provider before saving")
	}
	if err := cfg.SaveForRoot(root); err != nil {
		t.Fatalf("SaveForRoot: %v", err)
	}

	var got Config
	if _, err := toml.DecodeFile(projectPath, &got); err != nil {
		t.Fatalf("saved project config does not parse: %v", err)
	}
	if _, ok := got.Provider("global"); ok {
		t.Fatalf("user provider leaked into project config: %+v", got.Providers)
	}
	if _, ok := got.Provider("project-local"); !ok {
		t.Fatalf("project provider missing after save: %+v", got.Providers)
	}
}

func TestSaveToExistingProjectPersistsTopLevelDelta(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "reasonix.toml")
	if err := os.WriteFile(projectPath, []byte("[permissions]\nallow = [\"Bash(go test:*)\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.ConfigVersion = 2
	if err := cfg.SetDefaultModel("deepseek-pro"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(projectPath); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	body, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if !strings.Contains(string(body), `default_model = "deepseek-pro"`) {
		t.Fatalf("project config dropped top-level default_model delta:\n%s", body)
	}
	if !strings.Contains(string(body), "config_version = 2") {
		t.Fatalf("project config dropped top-level config_version delta:\n%s", body)
	}
	var got Config
	if _, err := toml.DecodeFile(projectPath, &got); err != nil {
		t.Fatalf("saved project config does not parse: %v", err)
	}
	if got.DefaultModel != "deepseek-pro" {
		t.Fatalf("default_model = %q, want deepseek-pro", got.DefaultModel)
	}
	if got.ConfigVersion != 2 {
		t.Fatalf("config_version = %d, want 2", got.ConfigVersion)
	}
}

func TestSaveToExistingProjectRemovesPluginDelta(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "reasonix.toml")
	cfg := Default()
	if err := cfg.UpsertPlugin(PluginEntry{Name: "ed", Type: "http", URL: "https://mcp.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer token"}}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(projectPath); err != nil {
		t.Fatalf("initial SaveTo: %v", err)
	}
	if !cfg.RemovePlugin("ed") {
		t.Fatal("RemovePlugin should report changed")
	}
	if err := cfg.SaveTo(projectPath); err != nil {
		t.Fatalf("SaveTo after remove: %v", err)
	}
	body, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if strings.Contains(string(body), "[[plugins]]") || strings.Contains(string(body), "[plugins.headers]") || strings.Contains(string(body), "Authorization") {
		t.Fatalf("removed plugin should not remain in project config:\n%s", body)
	}
	var got Config
	if _, err := toml.DecodeFile(projectPath, &got); err != nil {
		t.Fatalf("saved project config does not parse: %v", err)
	}
	if len(got.Plugins) != 0 {
		t.Fatalf("plugins = %+v, want none", got.Plugins)
	}
}

func TestSaveForRootDoesNotWriteUserAgentSettingsIntoProjectConfig(t *testing.T) {
	isolateUserConfigHome(t)
	root := t.TempDir()
	userPath := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("[agent]\ntemperature = 0.42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(projectPath, []byte("[permissions]\nallow = [\"Bash(go test:*)\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	if cfg.Agent.Temperature != 0.42 {
		t.Fatalf("runtime temperature = %v, want merged user config", cfg.Agent.Temperature)
	}
	if err := cfg.SaveForRoot(root); err != nil {
		t.Fatalf("SaveForRoot: %v", err)
	}
	body, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if strings.Contains(string(body), "temperature") {
		t.Fatalf("user agent setting leaked into project config:\n%s", body)
	}
}

func TestSetNetworkRejectsIncompleteCustomProxy(t *testing.T) {
	c := Default()
	if err := c.SetNetwork(NetworkConfig{ProxyMode: "custom"}); err == nil {
		t.Fatal("custom proxy without server/port should be rejected")
	}
}

func TestEffortCapabilityCustomSupportedEfforts(t *testing.T) {
	e := &ProviderEntry{
		Name:             "custom",
		Kind:             "openai",
		BaseURL:          "https://example.com",
		SupportedEfforts: []string{"low", "medium", "high"},
		DefaultEffort:    "high",
	}
	cap := EffortCapabilityForEntry(e)
	if !cap.Supported {
		t.Fatalf("expected supported, got %+v", cap)
	}
	wantLevels := []string{"auto", "low", "medium", "high"}
	if len(cap.Levels) != len(wantLevels) {
		t.Fatalf("levels = %v, want %v", cap.Levels, wantLevels)
	}
	for i, l := range wantLevels {
		if cap.Levels[i] != l {
			t.Errorf("levels[%d] = %q, want %q", i, cap.Levels[i], l)
		}
	}
	if cap.Default != "high" {
		t.Errorf("default = %q, want high", cap.Default)
	}
}

func TestEffortCapabilityUsesKnownModelRegistry(t *testing.T) {
	e := &ProviderEntry{
		Name:    "deepseek-proxy",
		Kind:    "openai",
		BaseURL: "https://proxy.example.com/v1",
		Model:   "deepseek-v4-flash",
	}
	cap := EffortCapabilityForEntry(e)
	if !cap.Supported {
		t.Fatalf("deepseek model behind proxy should expose effort, got %+v", cap)
	}
	wantLevels := []string{"auto", "high", "max"}
	if len(cap.Levels) != len(wantLevels) {
		t.Fatalf("levels = %v, want %v", cap.Levels, wantLevels)
	}
	for i, want := range wantLevels {
		if cap.Levels[i] != want {
			t.Fatalf("levels[%d] = %q, want %q", i, cap.Levels[i], want)
		}
	}
	if cap.Default != "high" {
		t.Fatalf("default = %q, want high", cap.Default)
	}
	if protocol := ReasoningProtocolForEntry(e); protocol != ReasoningProtocolDeepSeek {
		t.Fatalf("protocol = %q, want deepseek", protocol)
	}
	if got, err := NormalizeEffort(e, "max"); err != nil || got != "max" {
		t.Fatalf("NormalizeEffort(max) = %q/%v, want max/nil", got, err)
	}
}

func TestReasoningProtocolOverrideControlsEffortCapability(t *testing.T) {
	e := &ProviderEntry{
		Name:              "deepseek-proxy",
		Kind:              "openai",
		BaseURL:           "https://proxy.example.com/v1",
		Model:             "deepseek-v4-flash",
		ReasoningProtocol: "none",
	}
	if cap := EffortCapabilityForEntry(e); cap.Supported {
		t.Fatalf("reasoning_protocol=none should disable effort, got %+v", cap)
	}
	if protocol := ReasoningProtocolForEntry(e); protocol != ReasoningProtocolNone {
		t.Fatalf("protocol = %q, want none", protocol)
	}
	if _, err := NormalizeEffort(e, "max"); err == nil {
		t.Fatal("NormalizeEffort should reject effort when reasoning_protocol=none")
	}

	e.ReasoningProtocol = "openai"
	cap := EffortCapabilityForEntry(e)
	if !cap.Supported {
		t.Fatalf("reasoning_protocol=openai should expose OpenAI effort levels, got %+v", cap)
	}
	wantLevels := []string{"auto", "low", "medium", "high"}
	if len(cap.Levels) != len(wantLevels) {
		t.Fatalf("levels = %v, want %v", cap.Levels, wantLevels)
	}
	for i, want := range wantLevels {
		if cap.Levels[i] != want {
			t.Fatalf("levels[%d] = %q, want %q", i, cap.Levels[i], want)
		}
	}
	if _, err := NormalizeEffort(e, "max"); err == nil {
		t.Fatal("OpenAI reasoning_protocol should reject max")
	}
	if got, err := NormalizeEffort(e, "medium"); err != nil || got != "medium" {
		t.Fatalf("NormalizeEffort(medium) = %q/%v, want medium/nil", got, err)
	}
}

func TestNormalizeEffortCustomSupportedEfforts(t *testing.T) {
	e := &ProviderEntry{
		Name:             "custom",
		Kind:             "openai",
		BaseURL:          "https://example.com",
		SupportedEfforts: []string{"low", "medium", "high"},
	}
	for in, want := range map[string]string{"auto": "", "low": "low", "MEDIUM": "medium", "high": "high"} {
		got, err := NormalizeEffort(e, in)
		if err != nil || got != want {
			t.Fatalf("NormalizeEffort(%q) = %q/%v, want %q/nil", in, got, err, want)
		}
	}
	for _, bad := range []string{"max", "xhigh", "", "  "} {
		if _, err := NormalizeEffort(e, bad); err == nil {
			t.Errorf("NormalizeEffort(%q) should be rejected", bad)
		}
	}
}

func TestNormalizeEffortCustomDefaultEffort(t *testing.T) {
	e := &ProviderEntry{
		Name:             "custom",
		Kind:             "openai",
		BaseURL:          "https://example.com",
		SupportedEfforts: []string{"low", "medium", "high"},
		DefaultEffort:    "xhigh", // not in the list — must fall back to the first level
	}
	cap := EffortCapabilityForEntry(e)
	if cap.Default != "low" {
		t.Fatalf("default = %q, want low (first of supported_efforts)", cap.Default)
	}
	// Omitting DefaultEffort also falls back to the first level.
	e2 := *e
	e2.DefaultEffort = ""
	if cap := EffortCapabilityForEntry(&e2); cap.Default != "low" {
		t.Errorf("empty default = %q, want low", cap.Default)
	}
	// /effort auto still maps to "" regardless of DefaultEffort.
	if got, err := NormalizeEffort(e, "auto"); err != nil || got != "" {
		t.Fatalf("NormalizeEffort(auto) = %q/%v, want empty/nil", got, err)
	}
	e.Effort = "auto"
	if got := EffectiveEffort(e); got != "low" {
		t.Fatalf("stored auto should fall through to default_effort, got %q", got)
	}
	e.Effort = "high"
	if got := EffectiveEffort(e); got != "high" {
		t.Fatalf("explicit effort should win over default_effort, got %q", got)
	}
}

func TestNormalizeEffortCustomLevelsCaseInsensitive(t *testing.T) {
	e := &ProviderEntry{
		Name:             "custom",
		Kind:             "openai",
		BaseURL:          "https://example.com",
		SupportedEfforts: []string{"Low", "MEDIUM", "medium", "auto", " "},
		DefaultEffort:    "MEDIUM",
	}
	cap := EffortCapabilityForEntry(e)
	wantLevels := []string{"auto", "low", "medium"}
	if len(cap.Levels) != len(wantLevels) {
		t.Fatalf("levels = %v, want %v", cap.Levels, wantLevels)
	}
	for i, want := range wantLevels {
		if cap.Levels[i] != want {
			t.Fatalf("levels[%d] = %q, want %q", i, cap.Levels[i], want)
		}
	}
	if cap.Default != "medium" {
		t.Fatalf("default = %q, want medium", cap.Default)
	}
	got, err := NormalizeEffort(e, "MEDIUM")
	if err != nil || got != "medium" {
		t.Fatalf("NormalizeEffort(MEDIUM) = %q/%v, want medium/nil", got, err)
	}
	if got := EffectiveEffort(e); got != "medium" {
		t.Fatalf("EffectiveEffort = %q, want medium", got)
	}
}

func TestUpsertProviderNormalizesCustomEffortFields(t *testing.T) {
	c := &Config{}
	if err := c.UpsertProvider(ProviderEntry{
		Name:              "custom",
		Kind:              "openai",
		BaseURL:           "https://example.com",
		Model:             "m",
		Effort:            " HIGH ",
		ReasoningProtocol: " OPENAI ",
		SupportedEfforts:  []string{"Low", "MEDIUM", "medium", "auto"},
		DefaultEffort:     " LOW ",
	}); err != nil {
		t.Fatalf("UpsertProvider: %v", err)
	}
	got, _ := c.Provider("custom")
	if got.Effort != "high" || got.DefaultEffort != "low" {
		t.Fatalf("effort/default = %q/%q, want high/low", got.Effort, got.DefaultEffort)
	}
	if got.ReasoningProtocol != "openai" {
		t.Fatalf("reasoning_protocol = %q, want openai", got.ReasoningProtocol)
	}
	wantSupported := []string{"low", "medium"}
	if len(got.SupportedEfforts) != len(wantSupported) {
		t.Fatalf("supported_efforts = %v, want %v", got.SupportedEfforts, wantSupported)
	}
	for i, want := range wantSupported {
		if got.SupportedEfforts[i] != want {
			t.Fatalf("supported_efforts[%d] = %q, want %q", i, got.SupportedEfforts[i], want)
		}
	}
}

func TestEffortCapabilityEmptySupportedEffortsNotConfigurable(t *testing.T) {
	// mimo-pro without SupportedEfforts: no built-in heuristic, /effort must reject.
	e := &ProviderEntry{
		Name:    "mimo-pro",
		Kind:    "openai",
		BaseURL: "https://token-plan-cn.xiaomimimo.com/v1",
		Model:   "mimo-v2.5-pro",
	}
	if cap := EffortCapabilityForEntry(e); cap.Supported {
		t.Fatalf("mimo-pro without SupportedEfforts should not be configurable, got %+v", cap)
	}
	if _, err := NormalizeEffort(e, "high"); err == nil {
		t.Fatal("NormalizeEffort should reject level for unsupported provider")
	}
	// `supported_efforts = []` (empty slice) is treated like nil — the v2 design
	// has no way to opt out of the built-in heuristic; users either configure
	// levels or leave the field unset.
	e2 := *e
	e2.SupportedEfforts = []string{}
	if cap := EffortCapabilityForEntry(&e2); cap.Supported {
		t.Fatalf("empty supported_efforts should also fall through to the heuristic, got %+v", cap)
	}
}
