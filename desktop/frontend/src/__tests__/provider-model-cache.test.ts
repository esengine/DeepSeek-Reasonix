// Run: tsx src/__tests__/provider-model-cache.test.ts

import type { ProviderView } from "../lib/types";
import {
  cachedFetchProviderModels,
  clearModelCache,
  invalidateProviderCacheByAPIKeyEnv,
  isBackingOff,
} from "../lib/providerModelCache";

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

async function rejects(run: () => Promise<unknown>, label: string) {
  try {
    await run();
    ok(false, label);
  } catch {
    ok(true, label);
  }
}

function provider(overrides: Partial<ProviderView> = {}): ProviderView {
  return {
    name: "custom",
    builtIn: false,
    added: true,
    kind: "openai",
    baseUrl: "https://example.test/v1",
    modelsUrl: "https://example.test/v1/models",
    models: ["model-a"],
    visionModels: [],
    visionModelsConfigured: false,
    default: "model-a",
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

console.log("\nprovider model cache");

const realNow = Date.now;
let now = 10_000;
Date.now = () => now;

try {
  clearModelCache();
  let calls = 0;
  let resolveFetch!: (models: string[]) => void;
  const pendingFetch = () => {
    calls += 1;
    return new Promise<string[]>((resolve) => {
      resolveFetch = resolve;
    });
  };
  const firstPending = cachedFetchProviderModels(pendingFetch, provider());
  const secondPending = cachedFetchProviderModels(pendingFetch, provider());
  resolveFetch(["single-flight"]);
  const [firstModels, secondModels] = await Promise.all([firstPending, secondPending]);
  ok(calls === 1 && firstModels[0] === "single-flight" && secondModels[0] === "single-flight", "deduplicates concurrent requests");

  const cachedModels = await cachedFetchProviderModels(async () => {
    calls += 1;
    return ["unexpected"];
  }, provider());
  ok(calls === 1 && cachedModels[0] === "single-flight", "serves a fresh TTL cache entry");

  now += 60_000;
  const refreshedModels = await cachedFetchProviderModels(async () => {
    calls += 1;
    return ["after-ttl"];
  }, provider());
  ok(calls === 2 && refreshedModels[0] === "after-ttl", "refreshes an expired TTL entry");

  clearModelCache();
  calls = 0;
  const transient = new Error("transient model-list failure");
  await rejects(() => cachedFetchProviderModels(async () => {
    calls += 1;
    throw transient;
  }, provider()), "records the first failed request");
  ok(isBackingOff(provider()), "enters the initial retry window");
  await rejects(() => cachedFetchProviderModels(async () => {
    calls += 1;
    return ["too-early"];
  }, provider()), "reuses the failure during cooldown");
  ok(calls === 1, "does not call the provider during cooldown");
  now += 1_000;
  const recovered = await cachedFetchProviderModels(async () => {
    calls += 1;
    return ["recovered"];
  }, provider());
  ok(calls === 2 && recovered[0] === "recovered" && !isBackingOff(provider()), "retries after the deadline and resets backoff");

  clearModelCache();
  calls = 0;
  const cappedProvider = provider({ name: "backoff-cap" });
  let respectedEveryWindow = true;
  for (const delay of [1_000, 2_000, 4_000, 8_000, 16_000, 32_000, 60_000, 60_000]) {
    try {
      await cachedFetchProviderModels(async () => {
        calls += 1;
        throw transient;
      }, cappedProvider);
    } catch {}
    const beforeCooldownProbe = calls;
    now += delay - 1;
    try {
      await cachedFetchProviderModels(async () => {
        calls += 1;
        return ["too-early"];
      }, cappedProvider);
    } catch {}
    respectedEveryWindow = respectedEveryWindow && calls === beforeCooldownProbe;
    now += 1;
  }
  const afterCap = await cachedFetchProviderModels(async () => {
    calls += 1;
    return ["after-cap"];
  }, cappedProvider);
  ok(respectedEveryWindow && calls === 9 && afterCap[0] === "after-cap", "doubles retry windows and caps them at 60 seconds");

  clearModelCache();
  calls = 0;
  const orderedHeaders = provider({ headers: { "X-B": "2", "X-A": "1" } });
  const reorderedHeaders = provider({ headers: { "X-A": "1", "X-B": "2" } });
  await cachedFetchProviderModels(async () => {
    calls += 1;
    return ["ordered"];
  }, orderedHeaders);
  await cachedFetchProviderModels(async () => {
    calls += 1;
    return ["unexpected"];
  }, reorderedHeaders);
  ok(calls === 1, "normalizes header ordering in the request identity");

  const otherEndpoint = await cachedFetchProviderModels(async () => {
    calls += 1;
    return ["other-endpoint"];
  }, provider({ modelsUrl: "https://example.test/alternate/models", headers: { "X-A": "1", "X-B": "2" } }));
  ok(calls === 2 && otherEndpoint[0] === "other-endpoint", "separates different modelsUrl endpoints");

  clearModelCache();
  calls = 0;
  const oldFingerprint = provider({ modelCatalogFingerprint: "catalog-revision-old" });
  const newFingerprint = provider({ modelCatalogFingerprint: "catalog-revision-new" });
  await cachedFetchProviderModels(async () => {
    calls += 1;
    return ["old-catalog"];
  }, oldFingerprint);
  const afterExternalCredentialChange = await cachedFetchProviderModels(async () => {
    calls += 1;
    return ["new-catalog"];
  }, newFingerprint);
  ok(calls === 2 && afterExternalCredentialChange[0] === "new-catalog", "refetches when the opaque catalog fingerprint changes");

  invalidateProviderCacheByAPIKeyEnv("CUSTOM_API_KEY");
  const afterCredentialChange = await cachedFetchProviderModels(async () => {
    calls += 1;
    return ["new-credential"];
  }, reorderedHeaders);
  ok(calls === 3 && afterCredentialChange[0] === "new-credential", "invalidates every identity that shares a credential env");

  clearModelCache();
  calls = 0;
  let resolveStale!: (models: string[]) => void;
  const stalePending = cachedFetchProviderModels(() => {
    calls += 1;
    return new Promise<string[]>((resolve) => {
      resolveStale = resolve;
    });
  }, provider());
  invalidateProviderCacheByAPIKeyEnv("CUSTOM_API_KEY");
  const current = await cachedFetchProviderModels(async () => {
    calls += 1;
    return ["current"];
  }, provider());
  resolveStale(["stale"]);
  await stalePending;
  const stillCurrent = await cachedFetchProviderModels(async () => {
    calls += 1;
    return ["unexpected"];
  }, provider());
  ok(calls === 2 && current[0] === "current" && stillCurrent[0] === "current", "prevents invalidated inflight results from repopulating the cache");
} finally {
  Date.now = realNow;
  clearModelCache();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
