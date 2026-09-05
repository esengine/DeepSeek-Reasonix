import { matchingModelKey } from "./providerImageInput";
import type { ProviderModelOverrideView } from "./types";

export type ModelDraft = { model: string; context: string; output: string; vision: "auto" | "yes" | "no" };

/** Validate explicit overrides without turning an inherited value into a guessed limit. */
export function modelDraftError(draft: ModelDraft, existing: string[], original?: string): "required" | "duplicate" | "context" | "output" | null {
  const model = draft.model.trim();
  if (!model) return "required";
  if (existing.some((name) => name !== original && name === model)) return "duplicate";
  const valid = (value: string, allowOmit = false) => !value.trim() || (Number.isSafeInteger(Number(value)) && (Number(value) > 0 || (allowOmit && Number(value) === -1)));
  if (!valid(draft.context)) return "context";
  if (!valid(draft.output, true)) return "output";
  return null;
}

/** Keep unedited reasoning and future view fields when changing one model. */
export function applyModelDraft(overrides: ProviderModelOverrideView[], draft: ModelDraft, original?: string): ProviderModelOverrideView[] {
  const key = matchingModelKey(overrides.map((item) => item.model), original ?? draft.model);
  const previous = overrides.find((item) => item.model === key);
  const next: ProviderModelOverrideView = {
    reasoningProtocol: "", supportedEfforts: [], defaultEffort: "", ...previous,
    model: draft.model.trim(), contextWindow: Number(draft.context) || 0, maxOutputTokens: Number(draft.output) || 0,
    vision: draft.vision === "auto" ? null : draft.vision === "yes",
  };
  return [...overrides.filter((item) => item.model !== key && item.model !== next.model), next];
}
