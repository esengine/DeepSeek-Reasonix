import type { ProviderView } from "./types";

export type MockProviderPresetTemplate = {
  id: string;
  label: string;
  description: string;
  keyEnv: string;
  provider: ProviderView;
  providers?: ProviderView[];
  recommended?: boolean;
  billingMode?: string;
  displayGroup?: string;
  displaySection?: string;
  displayTier?: string;
  routeKind?: string;
  optional?: boolean;
  displayOrder?: number;
};

type MockProviderTemplate = (
  provider: Pick<ProviderView, "name" | "kind" | "baseUrl" | "models" | "default" | "apiKeyEnv"> & Partial<ProviderView>,
) => ProviderView;

type MockPresetFactory = (
  id: string,
  label: string,
  description: string,
  keyEnv: string,
  provider: ProviderView,
) => MockProviderPresetTemplate;

export function createMockModelScopePreset(
  mockProviderTemplate: MockProviderTemplate,
  mockPreset: MockPresetFactory,
): MockProviderPresetTemplate {
  const models = [
    "Qwen/Qwen3.5-397B-A17B",
    "Qwen/Qwen3.5-122B-A10B",
    "Qwen/Qwen3.5-27B",
    "deepseek-ai/DeepSeek-V4-Flash-0731",
    "deepseek-ai/DeepSeek-V4-Pro",
    "MiniMax/MiniMax-M3",
    "ZhipuAI/GLM-5.2",
  ];
  const visionModels = models.slice(0, 3);
  return mockPreset(
    "modelscope",
    "ModelScope",
    "ModelScope community OpenAI-compatible endpoint with Qwen, DeepSeek and other open-source models.",
    "MODELSCOPE_API_KEY",
    mockProviderTemplate({
      name: "modelscope",
      kind: "openai",
      baseUrl: "https://api-inference.modelscope.cn/v1",
      models,
      default: models[0],
      apiKeyEnv: "MODELSCOPE_API_KEY",
      modelOverrides: visionModels.map((model) => ({
        model,
        reasoningProtocol: "",
        supportedEfforts: [],
        defaultEffort: "",
        vision: true,
      })),
    }),
  );
}
