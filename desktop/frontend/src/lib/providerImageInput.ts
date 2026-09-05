import type { ProviderModelCapabilityView, ProviderModelOverrideView } from "./types";

export type ImageInputMode = "auto" | "on" | "off";

// A negative-only guard for manually typed models before a backend preview
// exists. Mirrors openai.IsDeepSeek / IsOfficialDeepSeekVisionModel; it never
// infers positive capabilities from a model name.
export function imageInputHardBlocked(baseURL: string | undefined, model: string, capability?: ProviderModelCapabilityView): boolean {
  if (capability?.imageInputEnableAllowed !== undefined) return !capability.imageInputEnableAllowed;
  try {
    return new URL(baseURL ?? "").hostname.toLowerCase().endsWith(".deepseek.com")
      && model.trim().toLowerCase() !== "deepseek-v4-flash-vision-exp";
  } catch { return false; }
}

export function imageInputModes(overrides?: ProviderModelOverrideView[] | null): Record<string, ImageInputMode> {
  return Object.fromEntries((overrides ?? []).map((item) => [item.model, item.vision == null ? "auto" : item.vision ? "on" : "off"]));
}

export function imageInputModeForModel(modes: Record<string, ImageInputMode>, model: string): ImageInputMode {
  const key = matchingModelKey(Object.keys(modes), model);
  return key ? modes[key] : "auto";
}

export function matchingModelKey(keys: string[], model: string): string | undefined {
  const exact = model.trim();
  return keys.includes(exact) ? exact : [...keys].sort().find((key) => key.trim().toLowerCase() === exact.toLowerCase());
}

export function modelCapabilityForModel(capabilities: ProviderModelCapabilityView[] | null | undefined, model: string): ProviderModelCapabilityView | undefined {
  const key = matchingModelKey((capabilities ?? []).map((item) => item.model), model);
  return capabilities?.find((item) => item.model === key);
}

// Image editing only owns vision. Context/output/reasoning fields survive.
export function mergeImageInputModes(
  overrides: ProviderModelOverrideView[] | null | undefined,
  models: string[],
  modes: Record<string, ImageInputMode>,
): ProviderModelOverrideView[] {
  return models.flatMap((model) => {
    const key = matchingModelKey((overrides ?? []).map((item) => item.model), model);
    const previous = overrides?.find((item) => item.model === key);
    const mode = imageInputModeForModel(modes, model);
    if (!previous && mode === "auto") return [];
    const item: ProviderModelOverrideView = {
      reasoningProtocol: "", supportedEfforts: [], defaultEffort: "",
      ...previous, model, vision: mode === "auto" ? null : mode === "on",
    };
    return [item];
  });
}

export function imageInputState(mode: ImageInputMode, capability?: ProviderModelCapabilityView, fallback = "unknown"): string {
  if (capability?.imageInputEnableAllowed === false) return "unsupported";
  if (mode !== "auto") return mode === "on" ? "supported" : "unsupported";
  return capability?.automaticState ?? (capability?.source === "override" ? "unknown" : capability?.state) ?? fallback;
}
