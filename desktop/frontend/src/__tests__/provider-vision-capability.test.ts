// Run: tsx src/__tests__/provider-vision-capability.test.ts

import type { ProviderView } from "../lib/types";
import { providerModelVisionCapability, providerVisionModelsForView } from "../lib/providerVisionCapability";
import { createMockModelScopePreset } from "../lib/mockModelScopePreset";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function provider(overrides: Partial<ProviderView> = {}): ProviderView {
  return {
    name: "custom",
    builtIn: false,
    added: true,
    kind: "openai",
    baseUrl: "https://example.test/v1",
    modelsUrl: "",
    models: ["vision-model", "text-model", "opaque-model"],
    visionModels: ["vision-model"],
    visionModelsConfigured: true,
    default: "vision-model",
    apiKeyEnv: "CUSTOM_API_KEY",
    keySet: true,
    balanceUrl: "",
    contextWindow: 0,
    reasoningProtocol: "",
    thinking: "",
    supportedEfforts: [],
    defaultEffort: "",
    ...overrides,
  };
}

const legacy = provider({
  modelOverrides: [{
    model: "vision-model",
    reasoningProtocol: "",
    supportedEfforts: [],
    defaultEffort: "",
    vision: false,
  }],
});
ok(
  providerVisionModelsForView(legacy).length === 0,
  "model override can disable a legacy vision list entry",
);

const metadata = provider({
  visionModels: [],
  visionModelsConfigured: false,
  modelCapabilities: [
    { model: "vision-model", inputModalities: ["text", "image"], state: "supported", source: "adapter" },
    { model: "text-model", inputModalities: ["text"], state: "unsupported", source: "adapter" },
  ],
  modelOverrides: [],
});
const adapterMetadataVision = providerVisionModelsForView(metadata);
ok(adapterMetadataVision.length === 1 && adapterMetadataVision[0] === "vision-model", "adapter metadata enables native vision without VisionModels");
ok(providerModelVisionCapability(metadata, "text-model", adapterMetadataVision) === "unsupported", "adapter text-only metadata stays read-only");
ok(
  providerModelVisionCapability(
    provider({
      visionModels: [],
      visionModelsConfigured: false,
      visionCapability: "unsupported",
      modelCapabilities: [],
    }),
    "opaque-model",
    [],
  ) === "unsupported",
  "legacy provider-level unsupported capability remains a read-only fallback",
);

const overrideMetadata = provider({
  visionModels: [],
  visionModelsConfigured: false,
  modelOverrides: [{
    model: "vision-model",
    reasoningProtocol: "",
    supportedEfforts: [],
    defaultEffort: "",
    vision: true,
  }],
});
const metadataVision = providerVisionModelsForView(overrideMetadata);
ok(metadataVision.length === 1 && metadataVision[0] === "vision-model", "model override enables native vision without VisionModels");
ok(providerModelVisionCapability(overrideMetadata, "vision-model", metadataVision) === "supported", "supported model is marked read-only");
ok(providerModelVisionCapability(overrideMetadata, "opaque-model", metadataVision) === "unknown", "unknown model stays conservative");

const modelScopePreset = createMockModelScopePreset(
  (input) => ({ ...provider(), ...input, visionModels: [], visionModelsConfigured: false }),
  (id, label, description, keyEnv, provider) => ({ id, label, description, keyEnv, provider, providers: [provider] }),
);
const modelScopeVision = providerVisionModelsForView(modelScopePreset.provider);
ok(modelScopeVision.length === 3 && modelScopeVision.every((model) => model.startsWith("Qwen/")), "mock ModelScope preserves model-level vision metadata");

process.stdout.write(`provider vision capability: ${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exitCode = 1;
