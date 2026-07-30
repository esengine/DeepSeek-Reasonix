// Run: tsx src/__tests__/provider-model-refresh.test.ts

import {
  apiKeyEnvFromProviderName,
  inferredVisionModels,
  isLikelyChatModel,
  isLikelyVisionModel,
  mergedFetchedProviderModels,
  mergeProviderModelContextWindows,
  providerApiKeyEnvForSave,
  providerDefaultModel,
  providerIsConfigured,
  providerModelCandidates,
  providerModelContextWindowDrafts,
  providerModelContextWindowIsSmall,
  providerRequiresKey,
} from "../lib/providerModels";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nprovider model refresh");

eq(
  mergedFetchedProviderModels(["coding-pro"], ["coding-pro", "chat", "vision"]),
  ["coding-pro", "chat", "vision"],
  "appends discovered models without removing curated ones",
);

eq(
  mergedFetchedProviderModels(["coding-pro"], ["coding-pro", "chat", "vision"], { preserveCurated: true }),
  ["coding-pro"],
  "background refresh preserves manually curated model list",
);

eq(
  mergedFetchedProviderModels(["coding-pro"], ["chat", "vision"], { preserveCurated: true }),
  ["coding-pro"],
  "background refresh does not re-add deleted models",
);

eq(
  mergedFetchedProviderModels(["mimo-v2.5-pro"], ["mimo-v2.5", "mimo-v2.5-pro"], { preserveCurated: true }),
  ["mimo-v2.5-pro"],
  "manual access refresh preserves selected MiMo model instead of importing provider catalog",
);

eq(
  providerModelCandidates(["mimo-v2.5-pro"], ["mimo-v2.5", "mimo-v2.5-pro"]),
  ["mimo-v2.5-pro", "mimo-v2.5"],
  "manual access refresh can show provider catalog as unsaved candidates",
);

eq(
  providerModelCandidates(["mimo-v2.5-pro"], ["mimo-v2.5-asr", "mimo-v2.5-tts", "mimo-v2.5", "mimo-v2.5-pro"]),
  ["mimo-v2.5-pro", "mimo-v2.5"],
  "manual access refresh filters non-chat candidates before saving",
);

eq(
  [
    isLikelyChatModel("mimo-v2.5-pro"),
    isLikelyChatModel("mimo-v2.5-asr"),
    isLikelyChatModel("mimo-v2.5-tts"),
    isLikelyChatModel("text-embedding-3-small"),
  ],
  [true, false, false, false],
  "matches backend non-chat model heuristic",
);

eq(
  [
    isLikelyVisionModel("gpt-4o"),
    isLikelyVisionModel("gpt-4o-audio-preview"),
    isLikelyVisionModel("gpt-4o-mini-audio-preview"),
    isLikelyVisionModel("mimo-v2.5"),
    isLikelyVisionModel("mimo-v2-omni"),
    isLikelyVisionModel("qwen2.5-vl-72b-instruct"),
    isLikelyVisionModel("mimo-v2.5-asr"),
  ],
  [true, false, false, true, true, true, false],
  "detects likely image-capable chat model IDs",
);

eq(
  inferredVisionModels([
    "mimo-v2.5-pro",
    "mimo-v2.5",
    "mimo-v2-omni",
    "qwen-vl-plus",
    "mimo-v2.5-asr",
    "audio-omni-tts",
    "gpt-4o-audio-preview",
    "gpt-4o-mini-audio-preview",
  ]),
  ["mimo-v2.5", "mimo-v2-omni", "qwen-vl-plus"],
  "infers image-capable models without importing audio models",
);

eq(
  mergedFetchedProviderModels([], ["coding-pro", "chat"], { preserveCurated: true }),
  ["coding-pro", "chat"],
  "background refresh can populate an empty model list",
);

eq(
  providerDefaultModel("coding-pro", ["coding-pro", "chat"]),
  "coding-pro",
  "preserves current default when it remains available",
);

eq(
  providerDefaultModel("deleted", ["coding-pro", "chat"]),
  "coding-pro",
  "falls back to first saved model when default is unavailable",
);

const contextOverrides = [
  {
    model: "short-model",
    reasoningProtocol: "none",
    supportedEfforts: [],
    defaultEffort: "",
    vision: null,
    contextWindow: 32768,
  },
  {
    model: "long-model",
    reasoningProtocol: "",
    supportedEfforts: [],
    defaultEffort: "",
    vision: null,
    contextWindow: 1000000,
  },
];

eq(
  providerModelContextWindowDrafts(contextOverrides),
  { "short-model": "32768", "long-model": "1000000" },
  "loads positive per-model context windows into editable drafts",
);

eq(
  mergeProviderModelContextWindows(contextOverrides, ["short-model", "new-model"], {
    "short-model": "65536",
    "new-model": "131072",
  }),
  [
    { ...contextOverrides[0], contextWindow: 65536 },
    {
      model: "new-model",
      reasoningProtocol: "",
      supportedEfforts: [],
      defaultEffort: "",
      vision: null,
      contextWindow: 131072,
    },
  ],
  "merges model context edits while preserving other overrides and dropping removed models",
);

eq(
  mergeProviderModelContextWindows([], ["invalid-model"], { "invalid-model": "Infinity" }),
  [],
  "drops non-finite model context values before crossing the desktop bridge",
);

eq(
  [
    providerModelContextWindowIsSmall("8192"),
    providerModelContextWindowIsSmall("16384"),
    providerModelContextWindowIsSmall(""),
    providerModelContextWindowIsSmall("Infinity"),
  ],
  [true, false, false, false],
  "warns only for positive context windows below 16384 tokens",
);

eq(
  providerApiKeyEnvForSave("Local Gateway", "", ""),
  "",
  "keeps custom provider keyless when no key env or key value is supplied",
);

eq(
  providerApiKeyEnvForSave("Local Gateway", "", "sk-test"),
  "LOCAL_GATEWAY_API_KEY",
  "creates a key env when saving an inline key for a new custom provider",
);

eq(
  providerApiKeyEnvForSave("9router", "", "sk-test"),
  "CUSTOM_9ROUTER_API_KEY",
  "creates a valid key env for a digit-leading custom provider",
);

eq(
  providerApiKeyEnvForSave("Local Gateway", "GATEWAY_KEY", ""),
  "GATEWAY_KEY",
  "preserves an explicitly configured key env",
);

eq(
  [
    apiKeyEnvFromProviderName("9router"),
    apiKeyEnvFromProviderName("商汤"),
    apiKeyEnvFromProviderName("通义千问"),
  ],
  ["CUSTOM_9ROUTER_API_KEY", "CUSTOM_d39b9067_API_KEY", "CUSTOM_e995c4c9_API_KEY"],
  "generates valid stable key envs for digit-leading and non-ASCII provider names",
);

eq(
  providerApiKeyEnvForSave("商汤", "CUSTOM_API_KEY", "sk-test"),
  "CUSTOM_API_KEY",
  "preserves an explicitly configured legacy custom key env",
);

eq(
  [
    providerRequiresKey({ apiKeyEnv: "" }),
    providerIsConfigured({ apiKeyEnv: "", keySet: false }),
    providerIsConfigured({ apiKeyEnv: "LOCAL_API_KEY", keySet: false, requiresKey: false }),
    providerIsConfigured({ apiKeyEnv: "REMOTE_API_KEY", keySet: false, requiresKey: true }),
    providerIsConfigured({ apiKeyEnv: "REMOTE_API_KEY", keySet: true, requiresKey: true }),
  ],
  [false, true, true, false, true],
  "separates provider selectability from key presence for no-auth providers",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
