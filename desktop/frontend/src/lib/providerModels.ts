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
