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
  if (stem) return `${stem}_API_KEY`;
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
