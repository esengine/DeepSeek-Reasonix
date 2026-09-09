import type { ProviderView } from "./types";
import { matchingModelKey, modelCapabilityForModel } from "./providerImageInput";

export type ProviderModelVisionCapability = "supported" | "unsupported" | "unknown";

/**
 * Returns the models known to accept images, merging model metadata with the
 * legacy provider-level list without allowing a stale legacy entry to override
 * an explicit per-model false value.
 */
export function providerVisionModelsForView(
  provider: Pick<ProviderView, "models" | "visionModels" | "modelOverrides" | "modelCapabilities">,
  models = provider.models,
): string[] {
  const legacyVision = new Set(provider.visionModels);
  return models.filter((model) => {
    const metadata = modelCapabilityForModel(provider.modelCapabilities, model);
    if (metadata?.imageInputEnableAllowed === false) return false;
    const key = matchingModelKey((provider.modelOverrides ?? []).map((item) => item.model), model);
    const override = provider.modelOverrides?.find((item) => item.model === key)?.vision;
    if (override !== undefined && override !== null) return override;
    if (metadata) return metadata.state === "supported";
    return legacyVision.has(model);
  });
}

/** Returns a conservative read-only capability label for one model. */
export function providerModelVisionCapability(
  provider: Pick<ProviderView, "visionModelsConfigured" | "modelCapabilities" | "visionCapability">,
  model: string,
  visionModels: string[],
): ProviderModelVisionCapability {
  const metadata = modelCapabilityForModel(provider.modelCapabilities, model);
  if (metadata) return metadata.state === "supported" ? "supported" : metadata.state === "unknown" ? "unknown" : "unsupported";
  if (visionModels.includes(model)) return "supported";
  if (provider.visionModelsConfigured || provider.visionCapability === "unsupported") return "unsupported";
  return "unknown";
}
