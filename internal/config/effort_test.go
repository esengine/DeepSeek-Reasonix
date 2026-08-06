package config

import (
	"strings"
	"testing"
)

func TestDeepSeekV4FlashEffortCapabilityIncludesLow(t *testing.T) {
	flash := &ProviderEntry{Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash"}
	cap := EffortCapabilityForEntry(flash)
	want := []string{"auto", "disabled", "low", "high", "max"}
	if !cap.Supported || len(cap.Levels) != len(want) {
		t.Fatalf("Flash capability = %+v, want %v", cap, want)
	}
	for i := range want {
		if cap.Levels[i] != want[i] {
			t.Fatalf("Flash levels[%d] = %q, want %q", i, cap.Levels[i], want[i])
		}
	}
	if cap.Default != "high" {
		t.Fatalf("Flash default = %q, want high", cap.Default)
	}
	if got, err := NormalizeEffort(flash, "low"); err != nil || got != "low" {
		t.Fatalf("Flash low = %q/%v, want low/nil", got, err)
	}
	if got, err := NormalizeEffort(flash, "xhigh"); err != nil || got != "high" {
		t.Fatalf("Flash xhigh = %q/%v, want high/nil", got, err)
	}
	flash.ReasoningProtocol = ReasoningProtocolDeepSeek
	if got := EffortCapabilityForEntry(flash); len(got.Levels) != len(want) || got.Levels[2] != "low" {
		t.Fatalf("explicit DeepSeek Flash capability = %+v, want model-specific low", got)
	}
	if got, err := NormalizeEffort(flash, "low"); err != nil || got != "low" {
		t.Fatalf("explicit DeepSeek Flash low = %q/%v, want low/nil", got, err)
	}
	if got, err := NormalizeEffort(flash, "xhigh"); err != nil || got != "high" {
		t.Fatalf("explicit DeepSeek Flash xhigh = %q/%v, want high/nil", got, err)
	}

	pro := &ProviderEntry{Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro"}
	if got, err := NormalizeEffort(pro, "low"); err != nil || got != "high" {
		t.Fatalf("Pro low = %q/%v, want existing high mapping", got, err)
	}
	if got, err := NormalizeEffort(pro, "xhigh"); err != nil || got != "max" {
		t.Fatalf("Pro xhigh = %q/%v, want max/nil", got, err)
	}
	for _, level := range EffortCapabilityForEntry(pro).Levels {
		if level == "low" {
			t.Fatalf("Pro capability unexpectedly exposes low: %+v", EffortCapabilityForEntry(pro))
		}
	}
}

func TestIsMiniMaxEntry(t *testing.T) {
	for _, tc := range []struct {
		baseURL string
		want    bool
	}{
		{"https://api.minimaxi.com/v1", true},
		{"https://api.minimaxi.com", true},
		{"https://api.minimaxi.com/anthropic", true},
		// Subdomain variants of the canonical host.
		{"https://eu.minimaxi.com/v1", true},
		{"https://us.minimaxi.com/v1", true},
		// Bare apex (no api. prefix) is rejected: it would only match if
		// the user pointed their base_url at the apex domain, which is a
		// misconfiguration — not a path we want to silently accept.
		{"https://minimaxi.com/v1", false},
		{"https://minimaxi.com", false},
		// Other vendors must not match.
		{"https://api.deepseek.com", false},
		{"https://api.xiaomimimo.com/v1", false},
		{"https://api.minimax.example.com", false}, // wrong spelling — must not match
		{"", false},
	} {
		e := &ProviderEntry{Kind: "openai", BaseURL: tc.baseURL}
		if got := isMiniMaxEntry(e); got != tc.want {
			t.Errorf("baseURL=%q: isMiniMaxEntry=%v, want %v", tc.baseURL, got, tc.want)
		}
	}
}

func TestIsZhipuEntry(t *testing.T) {
	for _, tc := range []struct {
		baseURL string
		want    bool
	}{
		{"https://open.bigmodel.cn/api/paas/v4", true},
		{"https://open.bigmodel.cn", true},
		{"https://api.bigmodel.cn/v1", true},
		{"https://api.z.ai/api/paas/v4", true},
		{"https://open.z.ai/v1", true},
		// Bare apexes rejected.
		{"https://bigmodel.cn/v1", false},
		{"https://z.ai", false},
		// Other vendors must not match.
		{"https://api.deepseek.com", false},
		{"https://api.minimaxi.com/v1", false},
		{"", false},
	} {
		e := &ProviderEntry{Kind: "openai", BaseURL: tc.baseURL}
		if got := isZhipuEntry(e); got != tc.want {
			t.Errorf("baseURL=%q: isZhipuEntry=%v, want %v", tc.baseURL, got, tc.want)
		}
	}
}

func TestIsLongCatEntry(t *testing.T) {
	for _, tc := range []struct {
		baseURL string
		want    bool
	}{
		{"https://api.longcat.chat/openai/v1", true},
		{"https://api.longcat.chat/anthropic", true},
		{"https://longcat.chat/openai/v1", false},
		{"https://api.deepseek.com", false},
		{"", false},
	} {
		e := &ProviderEntry{Kind: "openai", BaseURL: tc.baseURL}
		if got := isLongCatEntry(e); got != tc.want {
			t.Errorf("baseURL=%q: isLongCatEntry=%v, want %v", tc.baseURL, got, tc.want)
		}
	}
}

func TestIsOllamaCloudEntry(t *testing.T) {
	for _, tc := range []struct {
		baseURL string
		want    bool
	}{
		{"https://ollama.com/v1", true},
		{"https://api.ollama.com/v1", true},
		{"https://localhost:11434/v1", false},
		{"http://127.0.0.1:11434/v1", false},
		{"https://api.openai.com/v1", false},
		{"", false},
	} {
		e := &ProviderEntry{Kind: "openai", BaseURL: tc.baseURL}
		if got := isOllamaCloudEntry(e); got != tc.want {
			t.Errorf("baseURL=%q: isOllamaCloudEntry=%v, want %v", tc.baseURL, got, tc.want)
		}
	}
}

func TestIsMimoEntry(t *testing.T) {
	for _, tc := range []struct {
		baseURL string
		want    bool
	}{
		{"https://api.xiaomimimo.com/v1", true},
		{"https://api.xiaomimimo.com/v1/responses", true},
		{"https://api.deepseek.com", false},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", false},
		{"https://api.xiaomimimo.com.attacker.example/v1", false},
		{"https://example.com/?u=api.xiaomimimo.com", false},
		{"", false},
	} {
		if got := isMimoEntry(&ProviderEntry{BaseURL: tc.baseURL}); got != tc.want {
			t.Errorf("baseURL=%q: isMimoEntry=%v, want %v", tc.baseURL, got, tc.want)
		}
	}
	if isMimoEntry(nil) {
		t.Fatal("isMimoEntry(nil) must be false")
	}
}

func TestMimoEffortSupportsNone(t *testing.T) {
	e := &ProviderEntry{
		Kind:              "responses",
		BaseURL:           "https://api.xiaomimimo.com/v1",
		ReasoningProtocol: ReasoningProtocolOpenAI,
	}
	cap := EffortCapabilityForEntry(e)
	if !cap.Supported {
		t.Fatal("MiMo effort must be supported")
	}
	for _, level := range []string{"auto", "none", "low", "medium", "high"} {
		if !containsString(cap.Levels, level) {
			t.Errorf("MiMo capability missing %q: %v", level, cap.Levels)
		}
	}
	got, err := NormalizeEffort(e, "none")
	if err != nil || got != "none" {
		t.Fatalf("MiMo none = %q/%v, want none/nil", got, err)
	}
	for _, level := range []string{"low", "medium", "high"} {
		if got, err := NormalizeEffort(e, level); err != nil || got != level {
			t.Fatalf("MiMo %s = %q/%v, want %s/nil", level, got, err, level)
		}
	}
	if _, err := NormalizeEffort(e, "xhigh"); err == nil {
		t.Fatal("MiMo xhigh must be rejected")
	}
	// A plain OpenAI endpoint keeps rejecting none.
	plain := &ProviderEntry{Kind: "openai", BaseURL: "https://example.com/v1", ReasoningProtocol: ReasoningProtocolOpenAI}
	if got, err := NormalizeEffort(plain, "none"); err == nil || got != "" {
		t.Fatalf("plain OpenAI none = %q/%v, want error", got, err)
	}
}

func TestEffortCapabilityZhipu(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4.5-air"}
	cap := EffortCapabilityForEntry(e)
	if !cap.Supported {
		t.Fatalf("GLM entry should expose /effort, got %+v", cap)
	}
	wantLevels := []string{"auto", "enabled", "disabled"}
	if len(cap.Levels) != len(wantLevels) {
		t.Fatalf("levels = %v, want %v", cap.Levels, wantLevels)
	}
	for i, l := range wantLevels {
		if cap.Levels[i] != l {
			t.Errorf("levels[%d] = %q, want %q", i, cap.Levels[i], l)
		}
	}
	if cap.Default != "enabled" {
		t.Errorf("default = %q, want enabled (GLM ships with thinking on)", cap.Default)
	}
}

func TestEffortCapabilityExplicitGLMProtocolOnGateway(t *testing.T) {
	e := &ProviderEntry{
		Name:              "glm-gateway",
		Kind:              "openai",
		BaseURL:           "https://gateway.example.com/v1",
		Model:             "glm-5.2",
		ReasoningProtocol: ReasoningProtocolGLM,
	}
	cap := EffortCapabilityForEntry(e)
	want := []string{"auto", "enabled", "disabled"}
	if !cap.Supported || cap.Default != "enabled" || len(cap.Levels) != len(want) {
		t.Fatalf("explicit GLM capability = %+v, want levels %v default enabled", cap, want)
	}
	for i := range want {
		if cap.Levels[i] != want[i] {
			t.Fatalf("explicit GLM levels[%d] = %q, want %q", i, cap.Levels[i], want[i])
		}
	}
	if got, err := NormalizeEffort(e, "disabled"); err != nil || got != "disabled" {
		t.Fatalf("explicit GLM disabled = %q/%v, want disabled/nil", got, err)
	}
	if got, err := NormalizeEffort(e, "high"); err != nil || got != "enabled" {
		t.Fatalf("explicit GLM legacy high = %q/%v, want enabled/nil", got, err)
	}
}

func TestGLMModelRegistryUpgradesLegacyGatewayConfig(t *testing.T) {
	for _, model := range []string{"glm-5", "glm-5.1", "glm-5.2"} {
		t.Run(model, func(t *testing.T) {
			// This is the shape persisted by an older Token Rhythm installation:
			// no reasoning_protocol and no model_overrides metadata.
			e := &ProviderEntry{
				Name:    "token-rhythm",
				Kind:    "openai",
				BaseURL: "https://tokenrhythm.studio/v1",
				Model:   model,
			}
			if got := ReasoningProtocolForEntry(e); got != ReasoningProtocolGLM {
				t.Fatalf("ReasoningProtocolForEntry() = %q, want %q", got, ReasoningProtocolGLM)
			}
			cap := EffortCapabilityForEntry(e)
			want := []string{"auto", "enabled", "disabled"}
			if !cap.Supported || cap.Default != "enabled" || !stringSlicesEqual(cap.Levels, want) {
				t.Fatalf("legacy gateway GLM capability = %+v, want levels %v default enabled", cap, want)
			}
		})
	}

	nonGLM := &ProviderEntry{Kind: "openai", BaseURL: "https://tokenrhythm.studio/v1", Model: "my-glm-5.2-alias"}
	if got := ReasoningProtocolForEntry(nonGLM); got != "" {
		t.Fatalf("non-exact GLM alias protocol = %q, want empty without explicit override", got)
	}
	otherGateway := &ProviderEntry{Kind: "openai", BaseURL: "https://opencode.ai/zen/go/v1", Model: "glm-5.2"}
	if got := ReasoningProtocolForEntry(otherGateway); got != "" {
		t.Fatalf("unrelated gateway GLM protocol = %q, want empty without explicit override", got)
	}
}

func TestEffortCapabilityLongCat(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://api.longcat.chat/openai/v1", Model: "LongCat-2.0"}
	cap := EffortCapabilityForEntry(e)
	if !cap.Supported {
		t.Fatalf("LongCat entry should expose /effort, got %+v", cap)
	}
	wantLevels := []string{"auto", "enabled", "disabled"}
	if len(cap.Levels) != len(wantLevels) {
		t.Fatalf("levels = %v, want %v", cap.Levels, wantLevels)
	}
	for i, l := range wantLevels {
		if cap.Levels[i] != l {
			t.Errorf("levels[%d] = %q, want %q", i, cap.Levels[i], l)
		}
	}
	if cap.Default != "enabled" {
		t.Errorf("default = %q, want enabled", cap.Default)
	}
}

func TestEffortCapabilityOllamaCloud(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://ollama.com/v1", Model: "nemotron-3-nano:30b"}
	cap := EffortCapabilityForEntry(e)
	if !cap.Supported {
		t.Fatalf("Ollama Cloud entry should expose /effort, got %+v", cap)
	}
	wantLevels := []string{"auto", "none", "low", "medium", "high", "max"}
	if len(cap.Levels) != len(wantLevels) {
		t.Fatalf("levels = %v, want %v", cap.Levels, wantLevels)
	}
	for i, l := range wantLevels {
		if cap.Levels[i] != l {
			t.Errorf("levels[%d] = %q, want %q", i, cap.Levels[i], l)
		}
	}
	if cap.Default != "auto" {
		t.Errorf("default = %q, want auto", cap.Default)
	}
}

func TestNormalizeEffortOllamaCloud(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://ollama.com/v1", Model: "nemotron-3-nano:30b"}
	cases := []struct {
		in, want string
	}{
		{"auto", ""},
		{"none", "none"},
		{"disabled", "none"},
		{"off", "none"},
		{"low", "low"},
		{"medium", "medium"},
		{"high", "high"},
		{"xhigh", "max"},
		{"max", "max"},
	}
	for _, tc := range cases {
		got, err := NormalizeEffort(e, tc.in)
		if err != nil {
			t.Errorf("NormalizeEffort(%q) returned error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeEffort(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeEffortZhipu(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4.5-air"}
	cases := []struct {
		in, want string
	}{
		{"auto", ""}, // auto == leave to provider default == empty
		{"enabled", "enabled"},
		{"disabled", "disabled"},
		{"ENABLED", "enabled"}, // case-insensitive
		// Stale values from other vendors resolve to a valid GLM level rather
		// than failing the /effort command.
		{"off", "disabled"}, // retired DeepSeek "no thinking"
		{"low", "enabled"},
		{"medium", "enabled"},
		{"high", "enabled"},
		{"xhigh", "enabled"},
		{"max", "enabled"},
	}
	for _, tc := range cases {
		got, err := NormalizeEffort(e, tc.in)
		if err != nil {
			t.Errorf("NormalizeEffort(%q) returned error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeEffort(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeEffortLongCat(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://api.longcat.chat/openai/v1", Model: "LongCat-2.0"}
	cases := []struct {
		in, want string
	}{
		{"auto", ""},
		{"enabled", "enabled"},
		{"disabled", "disabled"},
		{"ENABLED", "enabled"},
		{"off", "disabled"},
		{"low", "enabled"},
		{"medium", "enabled"},
		{"high", "enabled"},
		{"xhigh", "enabled"},
		{"max", "enabled"},
	}
	for _, tc := range cases {
		got, err := NormalizeEffort(e, tc.in)
		if err != nil {
			t.Errorf("NormalizeEffort(%q) returned error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeEffort(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEffortCapabilityMiniMax(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"}
	cap := EffortCapabilityForEntry(e)
	if !cap.Supported {
		t.Fatalf("M3 entry should expose /effort, got %+v", cap)
	}
	wantLevels := []string{"auto", "adaptive", "disabled"}
	if len(cap.Levels) != len(wantLevels) {
		t.Fatalf("levels = %v, want %v", cap.Levels, wantLevels)
	}
	for i, l := range wantLevels {
		if cap.Levels[i] != l {
			t.Errorf("levels[%d] = %q, want %q", i, cap.Levels[i], l)
		}
	}
	if cap.Default != "adaptive" {
		t.Errorf("default = %q, want adaptive (M3 ships with thinking on)", cap.Default)
	}
}

func TestNormalizeEffortMiniMax(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"}
	cases := []struct {
		in, want string
	}{
		{"auto", ""}, // auto == "leave to provider default" == empty
		{"adaptive", "adaptive"},
		{"disabled", "disabled"},
		{"ADAPTIVE", "adaptive"}, // case-insensitive
		// Stale values from other vendors still resolve to a valid M3 level
		// rather than failing the /effort command.
		{"off", "disabled"}, // retired DeepSeek "no thinking" → M3 actually supports "disabled"
		{"low", "adaptive"},
		{"medium", "adaptive"},
		{"high", "adaptive"},
		{"xhigh", "disabled"},
		{"max", "disabled"},
	}
	for _, tc := range cases {
		got, err := NormalizeEffort(e, tc.in)
		if err != nil {
			t.Errorf("NormalizeEffort(%q) returned error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeEffort(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeEffortMiniMaxRejectsGarbage(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"}
	// "turbo" and similar unrecognised inputs reach the MiniMax switch and
	// are rejected with the MiniMax-specific usage hint. Empty input is
	// rejected earlier with the generic hint; both are valid user-facing
	// errors. "off" is *not* in this list — it's a retired level we now
	// migrate to "adaptive" (tested in TestNormalizeEffortMiniMax above).
	cases := map[string]string{
		"turbo": "auto|adaptive|disabled",
		"":      "auto|<level>",
	}
	for in, wantHint := range cases {
		_, err := NormalizeEffort(e, in)
		if err == nil {
			t.Errorf("NormalizeEffort(%q) should be rejected", in)
			continue
		}
		if !strings.Contains(err.Error(), wantHint) {
			t.Errorf("NormalizeEffort(%q) error = %q, want it to mention %q", in, err.Error(), wantHint)
		}
	}
}

func TestEffectiveEffortMiniMax(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"}
	// unset → EffectiveEffort stays empty so the wire layer defaults to adaptive
	if got := EffectiveEffort(e); got != "" {
		t.Errorf("unset EffectiveEffort = %q, want \"\"", got)
	}
	// explicit value is preserved verbatim
	e.Effort = "disabled"
	if got := EffectiveEffort(e); got != "disabled" {
		t.Errorf("explicit EffectiveEffort = %q, want disabled", got)
	}
}
