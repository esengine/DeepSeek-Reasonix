import type { PresetName } from "../../config.js";

export interface PresetSettings {
  model: string;
  reasoningEffort: "high" | "max";
}

export const PRESETS: Record<PresetName, PresetSettings> = {
  flash: {
    model: "deepseek-v4-flash",
    reasoningEffort: "max",
  },
  pro: {
    model: "deepseek-v4-pro",
    reasoningEffort: "max",
  },
};

export const PRESET_DESCRIPTIONS: Record<PresetName, { headline: string; cost: string }> = {
  flash: {
    headline: "v4-flash always",
    cost: "cheapest · predictable · /pro still works for a one-turn bump",
  },
  pro: {
    headline: "v4-pro always",
    cost: "~3× flash · for hard multi-turn work",
  },
};

export function resolvePreset(name: string | undefined): PresetSettings {
  return name === "pro" ? PRESETS.pro : PRESETS.flash;
}

/** Canonical name — anything that isn't strictly `flash`/`pro` falls to `flash`. */
export function canonicalPresetName(name: string | undefined): PresetName {
  return name === "pro" ? "pro" : "flash";
}

export function presetNameForSettings(settings: PresetSettings): PresetName {
  return settings.model === "deepseek-v4-pro" ? "pro" : "flash";
}
