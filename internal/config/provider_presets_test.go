package config

import "testing"

func TestCuratedProviderPresetsCoverRequestedProviders(t *testing.T) {
	wantIDs := []string{
		"longcat-openai",
		"longcat-anthropic",
		"token-rhythm",
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
		"deepseek-responses",
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

func TestDeepSeekAnthropicPresetIsOptionalAndModelScoped(t *testing.T) {
	preset, ok := CuratedProviderPreset("deepseek-anthropic")
	if !ok || len(preset.Entries) != 1 {
		t.Fatalf("DeepSeek Anthropic preset = %+v, want one entry", preset)
	}
	entry := preset.Entries[0]
	if entry.Kind != "anthropic" || entry.BaseURL != deepSeekAnthropicBaseURL || entry.Default != "deepseek-v4-flash" || entry.Thinking != "enabled" || !EffectiveWebSearch(&entry) || entry.Vision || entry.APIKeyEnv != "DEEPSEEK_API_KEY" {
		t.Fatalf("DeepSeek Anthropic preset entry = %+v", entry)
	}
	var cfg Config
	if err := cfg.UpsertProvider(entry); err != nil {
		t.Fatalf("UpsertProvider: %v", err)
	}
	flash, ok := cfg.ResolveModel("deepseek-anthropic/deepseek-v4-flash")
	if !ok {
		t.Fatal("Flash model did not resolve")
	}
	pro, ok := cfg.ResolveModel("deepseek-anthropic/deepseek-v4-pro")
	if !ok {
		t.Fatal("Pro model did not resolve")
	}
	if cap := EffortCapabilityForEntry(flash); cap.Default != "high" || !containsString(cap.Levels, "disabled") || !containsString(cap.Levels, "low") || !containsString(cap.Levels, "max") {
		t.Fatalf("Flash effort capability = %+v", cap)
	}
	if got, err := NormalizeEffort(flash, "low"); err != nil || got != "low" {
		t.Fatalf("Flash low effort = %q/%v, want low/nil", got, err)
	}
	if cap := EffortCapabilityForEntry(pro); cap.Default != "high" || containsString(cap.Levels, "low") || !containsString(cap.Levels, "max") {
		t.Fatalf("Pro effort capability = %+v", cap)
	}
}

func TestDeepSeekResponsesPresetMatchesOfficialSupport(t *testing.T) {
	preset, ok := CuratedProviderPreset("deepseek-responses")
	if !ok || len(preset.Entries) != 1 {
		t.Fatalf("deepseek responses preset = %+v, found=%v", preset, ok)
	}
	entry := preset.Entries[0]
	if entry.Kind != "responses" || entry.BaseURL != "https://api.deepseek.com" || entry.ResponsesMode != "stateless" {
		t.Fatalf("deepseek responses endpoint = %+v", entry)
	}
	if len(entry.Models) != 1 || entry.Models[0] != "deepseek-v4-flash" || entry.Default != "deepseek-v4-flash" {
		t.Fatalf("deepseek responses models = %v default=%q", entry.Models, entry.Default)
	}
	if entry.ModelsURL != "" {
		t.Fatalf("deepseek responses models URL = %q, want static supported-model list", entry.ModelsURL)
	}
	if !EffectiveWebSearch(&entry) || entry.Vision || entry.VisionModels != nil {
		t.Fatalf("deepseek responses capabilities = web_search:%t vision:%t vision_models:%v", EffectiveWebSearch(&entry), entry.Vision, entry.VisionModels)
	}
}

func TestCuratedProviderPresetsDisplayOrder(t *testing.T) {
	wantPrefix := []string{
		"deepseek-responses",
		"glm-cn",
		"zai-global",
		"glm-coding-plan-cn",
		"glm-coding-plan-cn-anthropic",
		"zai-coding-plan-global",
		"zai-coding-plan-global-anthropic",
		"longcat-openai",
		"longcat-anthropic",
		"token-rhythm",
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

func TestCuratedProviderPresetsHideRedundantDeepSeekAnthropicPreset(t *testing.T) {
	for _, preset := range CuratedProviderPresets() {
		if preset.ID == "deepseek-anthropic" {
			t.Fatal("redundant DeepSeek Anthropic preset should not be shown in the curated list")
		}
	}
	if _, ok := CuratedProviderPreset("deepseek-anthropic"); !ok {
		t.Fatal("legacy DeepSeek Anthropic preset must remain available for compatibility")
	}
}

func TestTokenRhythmPresetMatchesPublicAPIIntegration(t *testing.T) {
	preset, ok := CuratedProviderPreset("token-rhythm")
	if !ok || len(preset.Entries) != 1 {
		t.Fatalf("Token Rhythm preset = %+v, want one entry", preset)
	}
	if preset.Label != "Token Rhythm" || preset.KeyEnv != "TOKEN_RHYTHM_API_KEY" {
		t.Fatalf("Token Rhythm identity = label %q key %q", preset.Label, preset.KeyEnv)
	}
	entry := preset.Entries[0]
	if entry.Kind != "openai" || entry.BaseURL != "https://tokenrhythm.studio/v1" || entry.ModelsURL != "https://tokenrhythm.studio/v1/models" {
		t.Fatalf("Token Rhythm endpoint mismatch: %+v", entry)
	}
	if entry.DefaultModel() != "deepseek-v4-flash" || !entry.HasModel("qwen3.8-max") || entry.HasModel("qwen-image-2.0") {
		t.Fatalf("Token Rhythm chat catalog mismatch: models=%v default=%q", entry.Models, entry.DefaultModel())
	}

	var cfg Config
	if err := cfg.UpsertProvider(entry); err != nil {
		t.Fatalf("upsert Token Rhythm preset: %v", err)
	}
	deepseek, ok := cfg.ResolveModel("token-rhythm/deepseek-v4-flash")
	if !ok || deepseek.ContextWindow != 1_000_000 || ReasoningProtocolForEntry(deepseek) != ReasoningProtocolDeepSeek {
		t.Fatalf("Token Rhythm DeepSeek capability mismatch: %+v", deepseek)
	}
	kimi, ok := cfg.ResolveModel("token-rhythm/kimi-k2.7-code")
	if !ok || kimi.ContextWindow != 256_000 || !EffectiveVision(kimi) {
		t.Fatalf("Token Rhythm Kimi capability mismatch: %+v", kimi)
	}
	glm, ok := cfg.ResolveModel("token-rhythm/glm-5.1")
	if !ok || glm.ContextWindow != 200_000 || EffectiveVision(glm) || ReasoningProtocolForEntry(glm) != ReasoningProtocolGLM {
		t.Fatalf("Token Rhythm GLM capability mismatch: %+v", glm)
	}
	glmCap := EffortCapabilityForEntry(glm)
	if !glmCap.Supported || glmCap.Default != "enabled" || !stringSlicesEqual(glmCap.Levels, []string{"auto", "enabled", "disabled"}) {
		t.Fatalf("Token Rhythm GLM effort mismatch: %+v", glmCap)
	}
	flash0731, ok := cfg.ResolveModel("token-rhythm/deepseek-v4-flash-0731")
	if !ok || ReasoningProtocolForEntry(flash0731) != ReasoningProtocolDeepSeek {
		t.Fatalf("Token Rhythm DeepSeek 0731 protocol mismatch: %+v", flash0731)
	}
	flashCap := EffortCapabilityForEntry(flash0731)
	if !flashCap.Supported || flashCap.Default != "high" || !stringSlicesEqual(flashCap.Levels, []string{"auto", "disabled", "low", "high", "max"}) {
		t.Fatalf("Token Rhythm DeepSeek 0731 effort mismatch: %+v", flashCap)
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

	qwen, ok := CuratedProviderPreset("qwen-cn")
	if !ok {
		t.Fatal("missing qwen-cn preset")
	}
	qwen.Entries[0].ModelOverrides["glm-5"] = ProviderModelOverride{ContextWindow: 1}
	freshQwen, ok := CuratedProviderPreset("qwen-cn")
	if !ok {
		t.Fatal("missing fresh qwen-cn preset")
	}
	if got := freshQwen.Entries[0].ModelOverrides["glm-5"].ContextWindow; got != 202_752 {
		t.Fatalf("fresh qwen glm-5 context window = %d, want 202752", got)
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
	kimiCNK3, ok := cfg.ResolveModel("kimi-cn/kimi-k3")
	if !ok {
		t.Fatal("kimi-cn/kimi-k3 did not resolve")
	}
	if !EffectiveVision(kimiCNK3) || kimiCNK3.ContextWindow != 1_048_576 ||
		ReasoningProtocolForEntry(kimiCNK3) != ReasoningProtocolOpenAI ||
		!stringSlicesEqual(kimiCNK3.SupportedEfforts, []string{"low", "high", "max"}) ||
		EffectiveEffort(kimiCNK3) != "max" {
		t.Fatalf("kimi-cn/kimi-k3 capability mismatch: %+v", kimiCNK3)
	}
	kimiCNK27, ok := cfg.ResolveModel("kimi-cn/kimi-k2.7-code")
	if !ok || ReasoningProtocolForEntry(kimiCNK27) != ReasoningProtocolNone {
		t.Fatalf("kimi-cn K2.7 reasoning protocol changed: %+v", kimiCNK27)
	}
	kimiGlobal, ok := cfg.Provider("kimi-global")
	if !ok {
		t.Fatal("kimi-global provider missing")
	}
	if kimiGlobal.BaseURL != "https://api.moonshot.ai/v1" || kimiGlobal.APIKeyEnv != "MOONSHOT_API_KEY" {
		t.Fatalf("kimi-global endpoint/key mismatch: %+v", kimiGlobal)
	}
	kimiGlobalK3, ok := cfg.ResolveModel("kimi-global/kimi-k3")
	if !ok || !EffectiveVision(kimiGlobalK3) || kimiGlobalK3.ContextWindow != 1_048_576 || EffectiveEffort(kimiGlobalK3) != "max" {
		t.Fatalf("kimi-global/kimi-k3 capability mismatch: %+v", kimiGlobalK3)
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
	if longcat.ContextWindow != longCat20ContextWindow {
		t.Fatalf("longcat-openai context_window = %d, want %d", longcat.ContextWindow, longCat20ContextWindow)
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
	if longcatAnthropic.ContextWindow != longCat20ContextWindow {
		t.Fatalf("longcat-anthropic context_window = %d, want %d", longcatAnthropic.ContextWindow, longCat20ContextWindow)
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
	if glmPlanAnthropic.Kind != "anthropic" || !glmPlanAnthropic.AuthHeader || glmPlanAnthropic.DefaultModel() != "glm-5.2" || glmPlanAnthropic.ContextWindow != 1000000 {
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
	if zaiPlanAnthropic.Kind != "anthropic" || !zaiPlanAnthropic.AuthHeader || zaiPlanAnthropic.BaseURL != "https://api.z.ai/api/anthropic" || zaiPlanAnthropic.DefaultModel() != "glm-5.2" || zaiPlanAnthropic.ContextWindow != 1000000 {
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
	kimiK3, ok := cfg.ResolveModel("opencode-go/kimi-k3")
	if !ok {
		t.Fatal("opencode-go/kimi-k3 did not resolve")
	}
	if protocol := ReasoningProtocolForEntry(kimiK3); protocol != ReasoningProtocolOpenAI {
		t.Fatalf("opencode Kimi K3 protocol = %q, want openai", protocol)
	}
	if cap := EffortCapabilityForEntry(kimiK3); !cap.Supported || cap.Default != "max" || !containsString(cap.Levels, "high") || !containsString(cap.Levels, "max") {
		t.Fatalf("opencode Kimi K3 effort capability = %+v, want high/max", cap)
	}
	for _, level := range []string{"high", "max"} {
		if got, err := NormalizeEffort(kimiK3, level); err != nil || got != level {
			t.Fatalf("opencode Kimi K3 /effort %s = %q, %v; want %s", level, got, err, level)
		}
	}
	if kimiK3.ContextWindow != 1_048_576 || !EffectiveVision(kimiK3) {
		t.Fatalf("opencode Kimi K3 context/vision capability mismatch: %+v", kimiK3)
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
	qwenProviders := []string{
		"qwen-cn",
		"qwen-global",
		"qwen-coding-plan-cn",
		"qwen-coding-plan-cn-anthropic",
		"qwen-coding-plan-global",
		"qwen-coding-plan-global-anthropic",
	}
	qwenContextWindows := map[string]int{
		"qwen3.7-plus":         1_000_000,
		"qwen3-coder-plus":     1_000_000,
		"qwen3-max-2026-01-23": 262_144,
		"qwen3-coder-next":     262_144,
		"MiniMax-M2.5":         196_608,
		"glm-5":                202_752,
		"glm-4.7":              202_752,
		"kimi-k2.5":            262_144,
	}
	for _, providerID := range qwenProviders {
		for model, want := range qwenContextWindows {
			resolved, ok := cfg.ResolveModel(providerID + "/" + model)
			if !ok {
				t.Fatalf("resolve %s/%s failed", providerID, model)
			}
			if got := resolved.ContextWindow; got != want {
				t.Fatalf("%s/%s context window = %d, want %d", providerID, model, got, want)
			}
		}
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
