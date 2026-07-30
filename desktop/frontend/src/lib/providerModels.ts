import type { ProviderModelOverrideView } from "./types";

export function mergedFetchedProviderModels(current: string[], fetched: string[], options: { preserveCurated?: boolean } = {}): string[] {
  const saved = uniqueStrings(current);
  if (options.preserveCurated && saved.length > 0) return saved;
  return uniqueStrings([...saved, ...fetched]);
}

export function providerModelCandidates(current: string[], fetched: string[]): string[] {
  return uniqueStrings([...current, ...fetched]).filter(isLikelyChatModel);
}

export function inferredVisionModels(models: string[]): string[] {
  return uniqueStrings(models).filter((model) => isLikelyChatModel(model) && isLikelyVisionModel(model));
}

export function providerDefaultModel(currentDefault: string, models: string[]): string {
  return currentDefault && models.includes(currentDefault) ? currentDefault : models[0] ?? "";
}

export function providerModelContextWindowDrafts(overrides: ProviderModelOverrideView[] | null | undefined): Record<string, string> {
  const drafts: Record<string, string> = {};
  for (const override of overrides ?? []) {
    const model = override.model.trim();
    const contextWindow = normalizedContextWindow(override.contextWindow);
    if (model && contextWindow > 0) drafts[model] = String(contextWindow);
  }
  return drafts;
}

export function providerModelContextWindowIsSmall(value: unknown): boolean {
  const contextWindow = normalizedContextWindow(value);
  return contextWindow > 0 && contextWindow < 16_384;
}

export function mergeProviderModelContextWindows(
  overrides: ProviderModelOverrideView[] | null | undefined,
  models: string[],
  drafts: Record<string, string>,
): ProviderModelOverrideView[] {
  const existing = new Map((overrides ?? []).map((override) => [override.model.trim(), override]));
  const merged: ProviderModelOverrideView[] = [];
  for (const model of uniqueStrings(models)) {
    const previous = existing.get(model);
    const parsedContextWindow = normalizedContextWindow(drafts[model]);
    const override: ProviderModelOverrideView = {
      model,
      reasoningProtocol: previous?.reasoningProtocol ?? "",
      supportedEfforts: previous?.supportedEfforts ?? [],
      defaultEffort: previous?.defaultEffort ?? "",
      vision: previous?.vision ?? null,
      contextWindow: Math.max(parsedContextWindow, 0),
    };
    if (
      override.reasoningProtocol.trim()
      || override.supportedEfforts.length > 0
      || override.defaultEffort.trim()
      || override.vision != null
      || (override.contextWindow ?? 0) > 0
    ) {
      merged.push(override);
    }
  }
  return merged;
}

function normalizedContextWindow(value: unknown): number {
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) return 0;
  return Math.min(Math.trunc(parsed), Number.MAX_SAFE_INTEGER);
}

export function providerRequiresKey(provider: { requiresKey?: boolean; apiKeyEnv?: string }): boolean {
  if (typeof provider.requiresKey === "boolean") return provider.requiresKey;
  return Boolean((provider.apiKeyEnv ?? "").trim());
}

export function providerIsConfigured(provider: { configured?: boolean; requiresKey?: boolean; apiKeyEnv?: string; keySet?: boolean }): boolean {
  if (typeof provider.configured === "boolean") return provider.configured;
  return !providerRequiresKey(provider) || Boolean(provider.keySet);
}

export function providerApiKeyEnvForSave(name: string, apiKeyEnv: string, keyDraft: string): string {
  const explicit = apiKeyEnv.trim();
  if (explicit) return explicit;
  return keyDraft.trim() ? apiKeyEnvFromProviderName(name) : "";
}

export function apiKeyEnvFromProviderName(name: string): string {
  const stem = name
    .trim()
    .toUpperCase()
    .replace(/[^A-Z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
  if (stem) {
    // Dotenv/environment variable names cannot start with a digit. Keep the
    // readable name while giving digit-leading providers (for example
    // "9router") a valid, stable credential slot.
    const validStem = /^[0-9]/.test(stem) ? `CUSTOM_${stem}` : stem;
    return `${validStem}_API_KEY`;
  }
  // When the provider name is entirely non-ASCII (e.g. Chinese characters),
  // generate a stable hash suffix so each custom provider gets a unique slot.
  const hash = fnv1a32(name.trim());
  return `CUSTOM_${hash}_API_KEY`;
}

/** 32-bit FNV-1a hash, returns 8-char lowercase hex. Stable and deterministic. */
function fnv1a32(s: string): string {
  let hash = 0x811c9dc5 >>> 0;
  for (let i = 0; i < s.length; i++) {
    hash ^= s.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash.toString(16).padStart(8, "0");
}

export function isLikelyChatModel(model: string): boolean {
  const lower = model.trim().toLowerCase();
  if (!lower) return false;
  for (const term of ["text-embedding", "text-to-speech", "speech-to-text"]) {
    if (lower.includes(term)) return false;
  }
  const nonChatTokens = new Set([
    "asr",
    "stt",
    "tts",
    "whisper",
    "embedding",
    "moderation",
    "rerank",
    "dall",
    "transcription",
  ]);
  return !lower.split(/[-_./:]+/).some((token) => nonChatTokens.has(token));
}

export function isLikelyVisionModel(model: string): boolean {
  const lower = model.trim().toLowerCase();
  if (!lower) return false;
  if (lower === "mimo-v2.5" || lower === "mimo-v2-omni") return true;
  const tokens = lower.split(/[-_./:]+/);
  if (tokens.includes("audio")) return false;
  if (lower.startsWith("gpt-4o")) return true;
  const visionTokens = new Set(["vl", "vision", "visual", "multimodal", "omni"]);
  return tokens.some((token) => visionTokens.has(token));
}

function uniqueStrings(values: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const value of values) {
    const model = value.trim();
    if (!model || seen.has(model)) continue;
    seen.add(model);
    out.push(model);
  }
  return out;
}
