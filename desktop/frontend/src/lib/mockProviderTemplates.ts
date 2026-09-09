import type { ProviderView } from "./types";
import type { MockProviderPresetTemplate } from "./mockModelScopePreset";
export function mockProviderTemplate(p: Pick<ProviderView, "name" | "kind" | "baseUrl" | "models" | "default" | "apiKeyEnv"> & Partial<ProviderView>): ProviderView {
  return {
    name: p.name,
    builtIn: false,
    added: true,
    kind: p.kind,
    baseUrl: p.baseUrl,
    modelsUrl: p.modelsUrl ?? "",
    models: p.models,
    visionModels: p.visionModels ?? [],
    visionModelsConfigured: Boolean(p.visionModelsConfigured ?? ((p.visionModels ?? []).length > 0)),
    visionCapability: p.visionCapability,
    default: p.default,
    apiKeyEnv: p.apiKeyEnv,
    headers: p.headers,
    extraBody: p.extraBody,
    authHeader: p.authHeader,
    noProxy: p.noProxy,
    keySet: Boolean(p.keySet),
    balanceUrl: p.balanceUrl ?? "",
    contextWindow: p.contextWindow ?? 0,
    reasoningProtocol: p.reasoningProtocol ?? "",
    thinking: p.thinking ?? "",
    webSearch: Boolean(p.webSearch),
    serverWebSearchCapability: Boolean(p.serverWebSearchCapability),
    supportedEfforts: p.supportedEfforts ?? [],
    defaultEffort: p.defaultEffort ?? "",
    modelOverrides: p.modelOverrides,
  };
}

export function mockPreset(id: string, label: string, description: string, keyEnv: string, provider: ProviderView, metadata: Partial<Pick<MockProviderPresetTemplate, "recommended" | "billingMode" | "displayGroup" | "displaySection" | "displayTier" | "routeKind" | "optional" | "displayOrder">> = {}): MockProviderPresetTemplate {
  return { id, label, description, keyEnv, provider, providers: [provider], ...metadata };
}

export function mockBundlePreset(id: string, label: string, description: string, keyEnv: string, providers: ProviderView[], metadata: Partial<Pick<MockProviderPresetTemplate, "recommended" | "billingMode" | "displayGroup" | "displaySection" | "displayTier" | "routeKind" | "optional" | "displayOrder">> = {}): MockProviderPresetTemplate {
  return { id, label, description, keyEnv, provider: providers[0], providers, ...metadata };
}

export const mockKimiAPIModels = ["kimi-k3", "kimi-k2.7-code", "kimi-k2.7-code-highspeed", "kimi-k2.6", "kimi-k2.5"];
export const mockLongCatModels = ["LongCat-2.0"];
export const mockTokenRhythmModels = ["deepseek-v4-flash", "deepseek-v4-pro", "glm-5", "glm-5.1", "minimax-m2.7", "kimi-k2.5", "kimi-k2.6", "minimax-m2.5", "mimo-v2.5-pro", "qwen3.7-max", "kimi-k2.7-code", "glm-5.2", "qwen3.8-max", "deepseek-v4-flash-0731"];
export const mockTokenRhythmModelOverrides = mockTokenRhythmModels.flatMap((model) => {
  if (model.startsWith("glm-")) return [{ model, reasoningProtocol: "glm", supportedEfforts: ["enabled", "disabled"], defaultEffort: "enabled" }];
  if (model.startsWith("deepseek-")) return [{ model, reasoningProtocol: "deepseek", supportedEfforts: model === "deepseek-v4-pro" ? ["disabled", "high", "max"] : ["disabled", "low", "high", "max"], defaultEffort: "high" }];
  return [];
});
export const mockMiMoV25Models = ["mimo-v2.5-pro", "mimo-v2.5"];
export const mockMiniMaxModels = ["MiniMax-M3", "MiniMax-M2.7", "MiniMax-M2.7-highspeed"];
export const mockGLMAPIModels = ["glm-5.2", "glm-5.1", "glm-5", "glm-5-turbo", "glm-5v-turbo", "glm-4.7", "glm-4.7-flash", "glm-4.7-flashx", "glm-4.6", "glm-4.5", "glm-4.5-air", "glm-4.5-flash"];
export const mockGLMCodingModels = ["glm-5.2", "glm-5.1", "glm-5", "glm-4.7"];
export const mockGLMAnthropicModels = ["glm-5.2[1m]", "glm-5.2", "glm-5.1", "glm-5", "glm-4.7", "glm-4.5-air"];
export const mockQwenAPIModels = ["qwen3.7-plus", "qwen3.7-max", "qwen3.6-plus", "qwen3.5-plus", "qwen3-max-2026-01-23", "qwen3-coder-next", "qwen3-coder-plus", "MiniMax-M2.5", "glm-5", "glm-4.7", "kimi-k2.5"];
export const mockQwenPlanModels = ["qwen3.7-plus", "qwen3.6-plus", "kimi-k2.5", "glm-5", "MiniMax-M2.5", "qwen3.5-plus", "qwen3-max-2026-01-23", "qwen3-coder-next", "qwen3-coder-plus", "glm-4.7"];
export const mockQwenPlanVisionModels = ["qwen3.7-plus", "qwen3.6-plus", "qwen3.5-plus", "kimi-k2.5"];
export const mockStepFunModels = ["step-3.7-flash", "step-3.5-flash", "step-3.5-flash-2603"];
export const mockOpenCodeGoModels = ["glm-5.3", "glm-5.2", "glm-5.1", "kimi-k3", "kimi-k2.7-code", "kimi-k2.6", "deepseek-v4-pro", "deepseek-v4-flash", "mimo-v2.5-pro", "mimo-v2.5", "hy3"];
export const mockNovitaModels = ["zai-org/glm-5.2", "moonshotai/kimi-k2.7-code", "minimax/minimax-m3", "deepseek/deepseek-v4-pro", "deepseek/deepseek-v4-flash", "qwen/qwen3.7-max", "qwen/qwen3.6-plus", "zai-org/glm-5v-turbo"];
export const mockGMIModels = ["zai-org/GLM-5.2-FP8", "deepseek-ai/DeepSeek-V4-Pro", "deepseek-ai/DeepSeek-V4-Flash", "moonshotai/Kimi-K2.7-Code", "anthropic/claude-sonnet-4.6", "openai/gpt-5.5"];
export const mockVercelModels = ["anthropic/claude-sonnet-4.6", "anthropic/claude-opus-4.8", "openai/gpt-5.4", "openai/gpt-5.4-pro", "moonshotai/kimi-k2.7-code", "zai/glm-5.2", "deepseek/deepseek-v4-pro"];
export const mockOllamaCloudModels = ["glm-5.2", "kimi-k2.7-code", "deepseek-v4-pro", "deepseek-v4-flash", "minimax-m3", "nemotron-3-nano:30b", "qwen3-coder-next"];

