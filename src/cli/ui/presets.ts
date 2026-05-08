import type { PresetName } from "../../config.js";

export interface PresetSettings {
  model: string;
  reasoningEffort: "high" | "max";
  autoEscalate: boolean;
}

/** Old names `fast`/`smart`/`max` aliased via `resolvePreset` so legacy configs still load. */
export const PRESETS: Record<"auto" | "flash" | "pro", PresetSettings> = {
  auto: {
    model: "deepseek-v4-flash",
    reasoningEffort: "max",
    autoEscalate: true,
  },
  flash: {
    model: "deepseek-v4-flash",
    reasoningEffort: "max",
    autoEscalate: false,
  },
  pro: {
    model: "deepseek-v4-pro",
    reasoningEffort: "max",
    autoEscalate: false,
  },
};

export const PRESET_DESCRIPTIONS: Record<
  "auto" | "flash" | "pro",
  { headline: string; cost: string }
> = {
  auto: {
    headline: "flash → pro on hard turns",
    cost: "default · ~96% turns stay on flash · pro kicks in only when needed",
  },
  flash: {
    headline: "v4-flash always",
    cost: "cheapest · predictable · /pro still works for a one-turn bump",
  },
  pro: {
    headline: "v4-pro always",
    cost: "~3× flash (5/31 discount) / ~12× full price · for hard multi-turn work",
  },
};

/** Legacy aliases: fast→flash+high, smart→auto, max→pro. Unknown names fall through to auto. */
export function resolvePreset(name: PresetName | undefined): PresetSettings {
  if (name === "auto" || name === "flash" || name === "pro") return PRESETS[name];
  if (name === "fast") return { ...PRESETS.flash, reasoningEffort: "high" };
  if (name === "smart") return PRESETS.auto;
  if (name === "max") return PRESETS.pro;
  return PRESETS.auto;
}

/** Canonical name for storage / display — unknown values become auto. */
export function canonicalPresetName(name: PresetName | undefined): "auto" | "flash" | "pro" {
  if (name === "auto" || name === "flash" || name === "pro") return name;
  return "auto";
}
