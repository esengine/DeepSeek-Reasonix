import type { ProviderModelCapabilityView, ProviderView } from "./types";

// Provider model cache with single-flight deduplication and time-based
// exponential backoff. The memory-only cache identity mirrors every ProviderView
// field that can change model discovery or credential resolution; persistent
// cooldowns use the backend's opaque fingerprint instead.

type CacheEntry = { at: number; models: string[] };
type BackoffEntry = { delay: number; retryAt: number; error: unknown };

const cache = new Map<string, CacheEntry>();
const inflight = new Map<string, Promise<string[]>>();
const backoff = new Map<string, BackoffEntry>();
const generations = new Map<string, number>();

const TTL = 60_000;
const BACKOFF_INITIAL = 1_000;
const BACKOFF_CAP = 60_000;
let cacheEpoch = 0;

function normalizedHeaders(headers?: Record<string, string> | null): [string, string][] {
  return Object.entries(headers ?? {}).sort(([a], [b]) => a.localeCompare(b));
}

export function providerDiscoveryIdentity(p: ProviderView): string {
  return JSON.stringify([
    p.apiKeyEnv.trim(),
    p.name.trim(),
    p.kind.trim(),
    p.baseUrl.trim(),
    p.modelsUrl.trim(),
    p.chatUrl?.trim() ?? "",
    p.requestUrl?.trim() ?? "",
    Boolean(p.noProxy),
    Boolean(p.authHeader),
    normalizedHeaders(p.headers),
    (p.keySource ?? "").trim(),
    (p.keySourcePath ?? "").trim(),
    (p.modelCatalogFingerprint ?? "").trim(),
  ]);
}

const cacheKey = providerDiscoveryIdentity;

function cacheKeyAPIKeyEnv(key: string): string {
  try {
    const parsed = JSON.parse(key) as unknown[];
    return typeof parsed[0] === "string" ? parsed[0] : "";
  } catch {
    return "";
  }
}

function generation(key: string): number {
  return generations.get(key) ?? 0;
}

function invalidateKey(key: string): void {
  generations.set(key, generation(key) + 1);
  cache.delete(key);
  backoff.delete(key);
  // Do not cancel an active request, but stop new callers from joining it.
  // Its generation guard prevents a stale result from repopulating the cache.
  inflight.delete(key);
}

function knownKeys(): Set<string> {
  return new Set([
    ...cache.keys(),
    ...inflight.keys(),
    ...backoff.keys(),
    ...generations.keys(),
  ]);
}

export async function cachedFetchProviderModels(
  fetchFn: (provider: ProviderView) => Promise<string[]>,
  provider: ProviderView,
  force = false,
): Promise<string[]> {
  const key = cacheKey(provider);
  const now = Date.now();

  if (!force) {
    const hit = cache.get(key);
    if (hit && now - hit.at < TTL) return [...hit.models];
  }

  const pending = inflight.get(key);
  if (pending) return pending;

  const cooldown = backoff.get(key);
  if (!force && cooldown && now < cooldown.retryAt) {
    throw cooldown.error;
  }

  const requestEpoch = cacheEpoch;
  const requestGeneration = generation(key);
  let request: Promise<string[]>;
  request = fetchFn(provider)
    .then((models) => {
      if (cacheEpoch === requestEpoch && generation(key) === requestGeneration) {
        cache.set(key, { at: Date.now(), models: [...models] });
        backoff.delete(key);
      }
      return models;
    })
    .catch((error) => {
      if (cacheEpoch === requestEpoch && generation(key) === requestGeneration) {
        const previous = backoff.get(key);
        const delay = previous ? Math.min(previous.delay * 2, BACKOFF_CAP) : BACKOFF_INITIAL;
        backoff.set(key, { delay, retryAt: Date.now() + delay, error });
      }
      throw error;
    })
    .finally(() => {
      if (inflight.get(key) === request) inflight.delete(key);
    });

  inflight.set(key, request);
  return request;
}

/** Metadata-preserving companion to cachedFetchProviderModels. */
export async function cachedFetchProviderModelCatalog(
  fetchFn: (provider: ProviderView) => Promise<ProviderModelCapabilityView[]>,
  provider: ProviderView,
  force = false,
): Promise<ProviderModelCapabilityView[]> {
  void force;
  const fetched = await fetchFn(provider);
  return fetched.map((item) => ({
	...item,
    model: item.model,
    inputModalities: [...(item.inputModalities ?? [])],
    state: item.state,
    source: item.source,
  }));
}

/** Tell the caller whether this provider is still inside its retry window. */
export function isBackingOff(provider: ProviderView): boolean {
  const state = backoff.get(cacheKey(provider));
  return Boolean(state && Date.now() < state.retryAt);
}

/** Clear cache/backoff for one exact provider request identity. */
export function invalidateProviderCache(provider: ProviderView): void {
  invalidateKey(cacheKey(provider));
}

/** Clear every provider identity that resolves credentials through this env. */
export function invalidateProviderCacheByAPIKeyEnv(apiKeyEnv: string): void {
  const normalized = apiKeyEnv.trim();
  if (!normalized) return;
  for (const key of knownKeys()) {
    if (cacheKeyAPIKeyEnv(key) === normalized) invalidateKey(key);
  }
}

/** Clear the entire model cache without allowing stale inflight writes back in. */
export function clearModelCache(): void {
  cacheEpoch += 1;
  cache.clear();
  inflight.clear();
  backoff.clear();
  generations.clear();
}

/** Return true when the network is too slow for background model discovery. */
export function shouldSkipAutoRefresh(): boolean {
  const nav = navigator as Navigator & {
    connection?: { saveData?: boolean; effectiveType?: string };
  };
  const conn = nav.connection;
  return Boolean(conn?.saveData || conn?.effectiveType === "slow-2g" || conn?.effectiveType === "2g");
}
