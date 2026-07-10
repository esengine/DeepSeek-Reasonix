package config

import "testing"

func TestCuratedProviderPresetsCoverRequestedProviders(t *testing.T) {
	wantIDs := []string{
		"longcat-openai",
		"longcat-anthropic",
		"kimi-cn",
		"kimi-global",
		"kimi-coding-plan",
		"mimo-api",
		"mimo-anthropic",
		"mimo-token-plan-cn",
		"mimo-token-plan-cn-anthropic",
		"mimo-token-plan-sgp",
		"mimo-token-plan-sgp-anthropic",
		"mimo-token-plan-ams",
		"mimo-token-plan-ams-anthropic",
		"minimax-cn-api",
		"minimax-global-api",
		"minimax-cn-anthropic",
		"minimax-global-anthropic",
		"glm-cn",
		"zai-global",
		"glm-coding-plan-cn",
		"glm-coding-plan-cn-anthropic",
		"zai-coding-plan-global",
		"zai-coding-plan-global-anthropic",
		"opencode-go",
		"opencode-go-anthropic",
		"opencode-zen-anthropic",
		"qwen-cn",
		"qwen-global",
		"qwen-coding-plan-cn",
		"qwen-coding-plan-cn-anthropic",
		"qwen-coding-plan-global",
		"qwen-coding-plan-global-anthropic",
		"stepfun",
		"stepfun-anthropic",
		"novita",
		"gmi",
		"vercel-ai-gateway",
		"huggingface",
		"nvidia",
		"kilocode",
		"ollama-cloud",
	}
	got := map[string]ProviderPreset{}
	for _, preset := range CuratedProviderPresets() {
		got[preset.ID] = preset
		if preset.ID == "" || preset.Label == "" || preset.KeyEnv == "" {
			t.Fatalf("preset has missing identity fields: %+v", preset)
		}
		if len(preset.Entries) == 0 {
			t.Fatalf("preset %q has no entries", preset.ID)
		}
		for _, entry := range preset.Entries {
			if entry.APIKeyEnv == "" {
				t.Fatalf("preset %q entry %q has no api_key_env", preset.ID, entry.Name)
			}
			if entry.PresetID != preset.ID || entry.PresetVersion != ProviderPresetVersion {
				t.Fatalf("preset %q entry %q metadata = %q/%d, want %q/%d", preset.ID, entry.Name, entry.PresetID, entry.PresetVersion, preset.ID, ProviderPresetVersion)
			}
			var cfg Config
			if err := cfg.UpsertProvider(entry); err != nil {
				t.Fatalf("preset %q entry %q failed validation: %v", preset.ID, entry.Name, err)
			}
		}
	}
	for _, id := range wantIDs {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing preset %q", id)
		}
	}
}

func TestCuratedProviderPresetsDisplayOrder(t *testing.T) {
	wantPrefix := []string{
		"glm-cn",
		"zai-global",
		"glm-coding-plan-cn",
		"glm-coding-plan-cn-anthropic",
		"zai-coding-plan-global",
		"zai-coding-plan-global-anthropic",
		"longcat-openai",
		"longcat-anthropic",
		"kimi-cn",
		"kimi-global",
		"kimi-coding-plan",
		"minimax-cn-api",
		"minimax-global-api",
		"minimax-cn-anthropic",
		"minimax-global-anthropic",
	}
	got := CuratedProviderPresets()
	if len(got) < len(wantPrefix) {
		t.Fatalf("got %d presets, want at least %d", len(got), len(wantPrefix))
	}
	for i, want := range wantPrefix {
		if got[i].ID != want {
			t.Fatalf("preset[%d] = %q, want %q", i, got[i].ID, want)
		}
	}
}

func TestCuratedProviderPresetsStepFunUsesOfficialBaseURLs(t *testing.T) {
	tests := []struct {
		id      string
		kind    string
		baseURL string
	}{
		{
			id:      "stepfun",
			kind:    "openai",
			baseURL: "https://api.stepfun.com/step_plan/v1",
		},
		{
			id:      "stepfun-anthropic",
			kind:    "anthropic",
			baseURL: "https://api.stepfun.com/step_plan",
		},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			preset, ok := CuratedProviderPreset(tt.id)
			if !ok {
				t.Fatalf("missing preset %q", tt.id)
			}
			if len(preset.Entries) != 1 {
				t.Fatalf("preset %q has %d entries, want 1", tt.id, len(preset.Entries))
			}
			entry := preset.Entries[0]
			if entry.Kind != tt.kind {
				t.Fatalf("preset %q kind = %q, want %q", tt.id, entry.Kind, tt.kind)
			}
			if entry.BaseURL != tt.baseURL {
				t.Fatalf("preset %q base_url = %q, want %q", tt.id, entry.BaseURL, tt.baseURL)
			}
		})
	}
}

func TestCuratedProviderPresetReturnsDeepCopy(t *testing.T) {
	preset, ok := CuratedProviderPreset("minimax-cn-api")
	if !ok {
		t.Fatal("missing minimax-cn-api preset")
	}
	preset.Entries[0].Models[0] = "mutated"
	preset.Entries[0].ExtraBody["reasoning_split"] = false
	preset.Entries[0].PresetID = "mutated"

	fresh, ok := CuratedProviderPreset("minimax-cn-api")
	if !ok {
		t.Fatal("missing fresh minimax-cn-api preset")
	}
	if got := fresh.Entries[0].Models[0]; got != "MiniMax-M3" {
		t.Fatalf("fresh minimax first model = %q, want MiniMax-M3", got)
	}
	if got, _ := fresh.Entries[0].ExtraBody["reasoning_split"].(bool); !got {
		t.Fatalf("fresh minimax reasoning_split = %v, want true", fresh.Entries[0].ExtraBody["reasoning_split"])
	}
	if got := fresh.Entries[0].PresetID; got != "minimax-cn-api" {
		t.Fatalf("fresh minimax preset_id = %q, want minimax-cn-api", got)
	}
}

func TestCuratedProviderPresetCapabilities(t *testing.T) {
	var cfg Config
	for _, preset := range CuratedProviderPresets() {
		for _, entry := range preset.Entries {
			if err := cfg.UpsertProvider(entry); err != nil {
				t.Fatalf("upsert preset %q: %v", preset.ID, err)
			}
		}
	}

	kimiCN, ok := cfg.Provider("kimi-cn")
	if !ok {
		t.Fatal("kimi-cn provider missing")
	}
	if kimiCN.DefaultModel() != "kimi-k2.7-code" || !kimiCN.HasVisionModel("kimi-k2.7-code-highspeed") || kimiCN.BalanceURL == "" {
		t.Fatalf("kimi-cn capability mismatch: %+v", kimiCN)
	}
	kimiGlobal, ok := cfg.Provider("kimi-global")
	if !ok {
		t.Fatal("kimi-global provider missing")
	}
	if kimiGlobal.BaseURL != "https://api.moonshot.ai/v1" || kimiGlobal.APIKeyEnv != "MOONSHOT_API_KEY" {
		t.Fatalf("kimi-global endpoint/key mismatch: %+v", kimiGlobal)
	}
	kimiPlan, ok := cfg.Provider("kimi-coding-plan")
	if !ok {
		t.Fatal("kimi-coding-plan provider missing")
	}
	if kimiPlan.Kind != "anthropic" || kimiPlan.DefaultModel() != "kimi-for-coding" || !kimiPlan.HasVisionModel("kimi-for-coding") || kimiPlan.Thinking != "adaptive" || kimiPlan.HasModel("kimi-code") {
		t.Fatalf("kimi-coding-plan capability mismatch: %+v", kimiPlan)
	}

	longcat, ok := cfg.ResolveModel("longcat-openai/LongCat-2.0")
	if !ok {
		t.Fatal("longcat-openai/LongCat-2.0 did not resolve")
	}
	if longcat.BaseURL != "https://api.longcat.chat/openai/v1" || longcat.ModelsURL != "https://api.longcat.chat/openai/v1/models" || longcat.APIKeyEnv != "LONGCAT_API_KEY" {
		t.Fatalf("longcat-openai endpoint/key mismatch: %+v", longcat)
	}
	if cap := EffortCapabilityForEntry(longcat); !cap.Supported || cap.Default != "enabled" || !containsString(cap.Levels, "disabled") {
		t.Fatalf("longcat-openai effort capability = %+v, want enabled/disabled", cap)
	}
	if price := longcat.PriceForModel("LongCat-2.0"); price == nil || price.Currency != "¥" || price.Input != 2 || price.Output != 8 || price.CacheHit != 0.04 {
		t.Fatalf("LongCat-2.0 price = %+v, want RMB discounted pricing", price)
	}
	longcatAnthropic, ok := cfg.Provider("longcat-anthropic")
	if !ok {
		t.Fatal("longcat-anthropic provider missing")
	}
	if longcatAnthropic.Kind != "anthropic" || longcatAnthropic.BaseURL != "https://api.longcat.chat/anthropic" || longcatAnthropic.ModelsURL != "https://api.longcat.chat/anthropic/v1/models" || !longcatAnthropic.AuthHeader || longcatAnthropic.Thinking != "enabled" {
		t.Fatalf("longcat-anthropic capability mismatch: %+v", longcatAnthropic)
	}

	mimo, ok := cfg.Provider("mimo-api")
	if !ok {
		t.Fatal("mimo-api provider missing")
	}
	if !mimo.NoProxy {
		t.Fatal("mimo-api preset should bypass configured proxy for China-only endpoint")
	}
	if mimo.DefaultModel() != "mimo-v2.5-pro" || !mimo.HasVisionModel("mimo-v2.5") || mimo.HasVisionModel("mimo-v2.5-pro") {
		t.Fatalf("mimo vision capability mismatch: %+v", mimo.VisionModels)
	}
	if price := mimo.PriceForModel("mimo-v2.5-pro"); price == nil || price.Currency != "¥" {
		t.Fatalf("mimo-v2.5-pro price = %+v, want RMB pricing", price)
	}
	mimoAnthropic, ok := cfg.Provider("mimo-anthropic")
	if !ok {
		t.Fatal("mimo-anthropic provider missing")
	}
	if mimoAnthropic.Kind != "anthropic" || mimoAnthropic.BaseURL != "https://api.xiaomimimo.com/anthropic" || mimoAnthropic.Thinking != "adaptive" {
		t.Fatalf("mimo-anthropic capability mismatch: %+v", mimoAnthropic)
	}
	mimoPlan, ok := cfg.Provider("mimo-token-plan-cn")
	if !ok {
		t.Fatal("mimo-token-plan-cn provider missing")
	}
	if !mimoPlan.NoProxy || mimoPlan.APIKeyEnv != "MIMO_TOKEN_PLAN_API_KEY" || !mimoPlan.HasVisionModel("mimo-v2.5") {
		t.Fatalf("mimo-token-plan-cn capability mismatch: %+v", mimoPlan)
	}
	mimoSGP, ok := cfg.Provider("mimo-token-plan-sgp")
	if !ok {
		t.Fatal("mimo-token-plan-sgp provider missing")
	}
	if mimoSGP.NoProxy || mimoSGP.BaseURL != "https://token-plan-sgp.xiaomimimo.com/v1" {
		t.Fatalf("mimo-token-plan-sgp endpoint/proxy mismatch: %+v", mimoSGP)
	}
	mimoPlanAnthropic, ok := cfg.Provider("mimo-token-plan-cn-anthropic")
	if !ok {
		t.Fatal("mimo-token-plan-cn-anthropic provider missing")
	}
	if mimoPlanAnthropic.Kind != "anthropic" || !mimoPlanAnthropic.NoProxy || mimoPlanAnthropic.BaseURL != "https://token-plan-cn.xiaomimimo.com/anthropic" {
		t.Fatalf("mimo-token-plan-cn-anthropic capability mismatch: %+v", mimoPlanAnthropic)
	}

	minimax, ok := cfg.ResolveModel("minimax-cn-api/MiniMax-M3")
	if !ok {
		t.Fatal("minimax-cn-api/MiniMax-M3 did not resolve")
	}
	if cap := EffortCapabilityForEntry(minimax); !cap.Supported || cap.Default != "adaptive" || !containsString(cap.Levels, "disabled") {
		t.Fatalf("minimax effort capability = %+v, want adaptive/disabled", cap)
	}
	minimaxGlobalAPI, ok := cfg.Provider("minimax-global-api")
	if !ok {
		t.Fatal("minimax-global-api provider missing")
	}
	if minimaxGlobalAPI.BaseURL != "https://api.minimax.io/v1" || !minimaxGlobalAPI.HasModel("MiniMax-M2.7-highspeed") {
		t.Fatalf("minimax-global-api capability mismatch: %+v", minimaxGlobalAPI)
	}
	minimaxPlan, ok := cfg.Provider("minimax-cn-anthropic")
	if !ok {
		t.Fatal("minimax-cn-anthropic provider missing")
	}
	if minimaxPlan.Kind != "anthropic" || !minimaxPlan.AuthHeader || !minimaxPlan.HasVisionModel("MiniMax-M3") || !minimaxPlan.HasModel("MiniMax-M2.7-highspeed") {
		t.Fatalf("minimax-cn-anthropic capability mismatch: %+v", minimaxPlan)
	}
	minimaxGlobal, ok := cfg.Provider("minimax-global-anthropic")
	if !ok {
		t.Fatal("minimax-global-anthropic provider missing")
	}
	if minimaxGlobal.Kind != "anthropic" || !minimaxGlobal.AuthHeader || minimaxGlobal.BaseURL != "https://api.minimax.io/anthropic" {
		t.Fatalf("minimax-global-anthropic capability mismatch: %+v", minimaxGlobal)
	}

	glm, ok := cfg.ResolveModel("glm-cn/glm-5.2")
	if !ok {
		t.Fatal("glm-cn/glm-5.2 did not resolve")
	}
	if cap := EffortCapabilityForEntry(glm); !cap.Supported || cap.Default != "enabled" || !containsString(cap.Levels, "disabled") {
		t.Fatalf("glm effort capability = %+v, want enabled/disabled", cap)
	}
	if !glm.HasVisionModel("glm-5v-turbo") {
		t.Fatalf("glm vision capability mismatch: %+v", glm.VisionModels)
	}
	zaiGlobal, ok := cfg.ResolveModel("zai-global/glm-5.2")
	if !ok {
		t.Fatal("zai-global/glm-5.2 did not resolve")
	}
	if cap := EffortCapabilityForEntry(zaiGlobal); !cap.Supported || cap.Default != "enabled" {
		t.Fatalf("zai-global effort capability = %+v, want enabled", cap)
	}
	glmPlanCN, ok := cfg.Provider("glm-coding-plan-cn")
	if !ok {
		t.Fatal("glm-coding-plan-cn provider missing")
	}
	if !glmPlanCN.NoProxy || glmPlanCN.DefaultModel() != "glm-5.2" || glmPlanCN.ContextWindow != 1000000 {
		t.Fatalf("glm-coding-plan-cn capability mismatch: %+v", glmPlanCN)
	}
	glmPlanAnthropic, ok := cfg.Provider("glm-coding-plan-cn-anthropic")
	if !ok {
		t.Fatal("glm-coding-plan-cn-anthropic provider missing")
	}
	if glmPlanAnthropic.Kind != "anthropic" || !glmPlanAnthropic.AuthHeader || glmPlanAnthropic.DefaultModel() != "glm-5.2[1m]" {
		t.Fatalf("glm-coding-plan-cn-anthropic capability mismatch: %+v", glmPlanAnthropic)
	}
	zaiPlanGlobal, ok := cfg.Provider("zai-coding-plan-global")
	if !ok {
		t.Fatal("zai-coding-plan-global provider missing")
	}
	if zaiPlanGlobal.NoProxy || zaiPlanGlobal.BaseURL != "https://api.z.ai/api/coding/paas/v4" || zaiPlanGlobal.DefaultModel() != "glm-5.2" {
		t.Fatalf("zai-coding-plan-global capability mismatch: %+v", zaiPlanGlobal)
	}
	zaiPlanAnthropic, ok := cfg.Provider("zai-coding-plan-global-anthropic")
	if !ok {
		t.Fatal("zai-coding-plan-global-anthropic provider missing")
	}
	if zaiPlanAnthropic.Kind != "anthropic" || !zaiPlanAnthropic.AuthHeader || zaiPlanAnthropic.BaseURL != "https://api.z.ai/api/anthropic" {
		t.Fatalf("zai-coding-plan-global-anthropic capability mismatch: %+v", zaiPlanAnthropic)
	}

	deepseek, ok := cfg.ResolveModel("opencode-go/deepseek-v4-pro")
	if !ok {
		t.Fatal("opencode-go/deepseek-v4-pro did not resolve")
	}
	if protocol := ReasoningProtocolForEntry(deepseek); protocol != ReasoningProtocolDeepSeek {
		t.Fatalf("opencode deepseek protocol = %q, want deepseek", protocol)
	}
	if cap := EffortCapabilityForEntry(deepseek); !cap.Supported || cap.Default != "high" || !containsString(cap.Levels, "max") {
		t.Fatalf("opencode deepseek effort capability = %+v, want high/max", cap)
	}

	kimi, ok := cfg.ResolveModel("opencode-go/kimi-k2.6")
	if !ok {
		t.Fatal("opencode-go/kimi-k2.6 did not resolve")
	}
	if protocol := ReasoningProtocolForEntry(kimi); protocol != ReasoningProtocolOpenAI {
		t.Fatalf("opencode kimi protocol = %q, want openai", protocol)
	}
	if cap := EffortCapabilityForEntry(kimi); !cap.Supported || cap.Default != "high" || !containsString(cap.Levels, "medium") {
		t.Fatalf("opencode kimi effort capability = %+v, want low/medium/high", cap)
	}

	plain, ok := cfg.ResolveModel("opencode-go/glm-5.2")
	if !ok {
		t.Fatal("opencode-go/glm-5.2 did not resolve")
	}
	if cap := EffortCapabilityForEntry(plain); cap.Supported {
		t.Fatalf("opencode plain model effort capability = %+v, want unsupported without override", cap)
	}
	zen, ok := cfg.Provider("opencode-zen-anthropic")
	if !ok {
		t.Fatal("opencode-zen-anthropic provider missing")
	}
	if zen.Kind != "anthropic" || zen.BaseURL != "https://opencode.ai/zen" || !zen.HasModel("qwen3.6-plus") {
		t.Fatalf("opencode-zen-anthropic capability mismatch: %+v", zen)
	}
	goAnthropic, ok := cfg.Provider("opencode-go-anthropic")
	if !ok {
		t.Fatal("opencode-go-anthropic provider missing")
	}
	if goAnthropic.Kind != "anthropic" || goAnthropic.BaseURL != "https://opencode.ai/zen/go" || goAnthropic.DefaultModel() != "qwen3.7-plus" || !goAnthropic.HasModel("minimax-m3") {
		t.Fatalf("opencode-go-anthropic capability mismatch: %+v", goAnthropic)
	}

	qwenCN, ok := cfg.Provider("qwen-cn")
	if !ok {
		t.Fatal("qwen-cn provider missing")
	}
	if !qwenCN.NoProxy || qwenCN.DefaultModel() != "qwen3.7-plus" || !qwenCN.HasVisionModel("qwen3.7-plus") || !qwenCN.HasModel("qwen3.7-max") {
		t.Fatalf("qwen-cn capability mismatch: %+v", qwenCN)
	}

	qwenGlobal, ok := cfg.Provider("qwen-global")
	if !ok {
		t.Fatal("qwen-global provider missing")
	}
	if qwenGlobal.NoProxy || qwenGlobal.BaseURL != "https://dashscope-intl.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("qwen-global endpoint/proxy mismatch: %+v", qwenGlobal)
	}
	qwenPlanCN, ok := cfg.Provider("qwen-coding-plan-cn")
	if !ok {
		t.Fatal("qwen-coding-plan-cn provider missing")
	}
	if !qwenPlanCN.NoProxy || !qwenPlanCN.HasModel("qwen3.6-plus") || !qwenPlanCN.HasVisionModel("qwen3.7-plus") || qwenPlanCN.HasVisionModel("qwen3-coder-plus") {
		t.Fatalf("qwen-coding-plan-cn capability mismatch: %+v", qwenPlanCN)
	}
	qwenPlanCNAnthropic, ok := cfg.Provider("qwen-coding-plan-cn-anthropic")
	if !ok {
		t.Fatal("qwen-coding-plan-cn-anthropic provider missing")
	}
	if qwenPlanCNAnthropic.Kind != "anthropic" || !qwenPlanCNAnthropic.NoProxy || qwenPlanCNAnthropic.BaseURL != "https://coding.dashscope.aliyuncs.com/apps/anthropic" {
		t.Fatalf("qwen-coding-plan-cn-anthropic capability mismatch: %+v", qwenPlanCNAnthropic)
	}
	qwenPlanGlobal, ok := cfg.Provider("qwen-coding-plan-global")
	if !ok {
		t.Fatal("qwen-coding-plan-global provider missing")
	}
	if qwenPlanGlobal.NoProxy || !qwenPlanGlobal.HasModel("qwen3.6-plus") || qwenPlanGlobal.BaseURL != "https://coding-intl.dashscope.aliyuncs.com/v1" {
		t.Fatalf("qwen-coding-plan-global capability mismatch: %+v", qwenPlanGlobal)
	}

	gmi, ok := cfg.Provider("gmi")
	if !ok {
		t.Fatal("gmi provider missing")
	}
	if got := gmi.Headers["User-Agent"]; got != "Reasonix" {
		t.Fatalf("gmi User-Agent header = %q, want Reasonix", got)
	}
	vercel, ok := cfg.Provider("vercel-ai-gateway")
	if !ok {
		t.Fatal("vercel-ai-gateway provider missing")
	}
	if vercel.Kind != "anthropic" || !vercel.AuthHeader || vercel.DefaultModel() != "anthropic/claude-sonnet-4.6" || !vercel.HasModel("moonshotai/kimi-k2.7-code") {
		t.Fatalf("vercel-ai-gateway capability mismatch: %+v", vercel)
	}

	ollama, ok := cfg.ResolveModel("ollama-cloud/nemotron-3-nano:30b")
	if !ok {
		t.Fatal("ollama-cloud/nemotron-3-nano:30b did not resolve")
	}
	if cap := EffortCapabilityForEntry(ollama); !cap.Supported || cap.Default != "auto" || !containsString(cap.Levels, "max") || !containsString(cap.Levels, "none") {
		t.Fatalf("ollama-cloud effort capability = %+v, want none/max", cap)
	}
}

func TestCuratedProviderPresetDeepSeekReasoningProtocolScope(t *testing.T) {
	var cfg Config
	for _, preset := range CuratedProviderPresets() {
		for _, entry := range preset.Entries {
			if err := cfg.UpsertProvider(entry); err != nil {
				t.Fatalf("upsert preset %q: %v", preset.ID, err)
			}
		}
	}

	tests := []struct {
		ref  string
		want string
	}{
		{ref: "opencode-go/deepseek-v4-pro", want: ReasoningProtocolDeepSeek},
		{ref: "opencode-go/deepseek-v4-flash", want: ReasoningProtocolDeepSeek},
		{ref: "ollama-cloud/deepseek-v4-pro", want: ReasoningProtocolDeepSeek},
		{ref: "ollama-cloud/deepseek-v4-flash", want: ReasoningProtocolDeepSeek},
		{ref: "novita/deepseek/deepseek-v4-pro"},
		{ref: "novita/deepseek/deepseek-v4-flash"},
		{ref: "gmi/deepseek-ai/DeepSeek-V4-Pro"},
		{ref: "gmi/deepseek-ai/DeepSeek-V4-Flash"},
		{ref: "nvidia/deepseek-ai/deepseek-v4-pro"},
		{ref: "vercel-ai-gateway/deepseek/deepseek-v4-pro"},
	}
	for _, tc := range tests {
		t.Run(tc.ref, func(t *testing.T) {
			entry, ok := cfg.ResolveModel(tc.ref)
			if !ok {
				t.Fatalf("ResolveModel(%q) failed", tc.ref)
			}
			if got := ReasoningProtocolForEntry(entry); got != tc.want {
				t.Fatalf("ReasoningProtocolForEntry(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}
