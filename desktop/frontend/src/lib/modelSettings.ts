import { app } from "./bridge";
import type { ModelSettingsChange, ModelSettingsResult, SettingsView } from "./types";

type WithoutRequest<T> = T extends unknown ? Omit<T, "requestId" | "expectedFingerprint"> : never;
export type ModelSettingsEdit = WithoutRequest<ModelSettingsChange>;

export async function saveModelSettings(settings: SettingsView, change: ModelSettingsEdit): Promise<ModelSettingsResult> {
  const expectedFingerprint = settings.modelSettingsFingerprint;
  if (!expectedFingerprint) throw new Error("Reload Settings before saving model configuration.");
  const requestId = crypto.randomUUID();
  let result: ModelSettingsResult;
  try {
    result = await app.ApplyModelSettings({ ...change, requestId, expectedFingerprint });
  } catch {
    // Read the receipt first. Repeating an interrupted credential write could
    // overwrite a newer edit, so this path never resubmits the operation.
    try { result = await app.GetModelSettingsRequest(requestId); }
    catch { throw new Error("The save result could not be confirmed. Review the current settings before saving again."); }
  }
  if (!result.persisted) throw new Error(result.issues?.map(issue => issue.message).join("\n") || "Model settings were not saved.");
  return result;
}

export function isModelSettingsResult(value: unknown): value is ModelSettingsResult {
  return !!value && typeof value === "object" && typeof (value as ModelSettingsResult).persisted === "boolean";
}
