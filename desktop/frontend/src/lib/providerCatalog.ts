import data from "./providerCatalog.generated.json";
import type { ProviderCatalog, ProviderPresetView } from "./types";

// Old desktop hosts omit catalog metadata. The generated fallback comes from the
// same Go registry, so protocol names never have to be guessed from display text.
export function catalogForPreset(preset: ProviderPresetView): ProviderCatalog {
  const known: Record<string, ProviderCatalog> = data.catalogs;
  return preset.catalog ?? known[preset.id] ?? {
    brandId: preset.id, brandLabel: preset.label, region: "global", product: "api",
    format: preset.routeKind || "openai", baseUrl: "",
  };
}


// Generated metadata also covers the built-in official provider and older hosts.
export function protocolsForCatalog(catalog: ProviderCatalog) {
  const known = Object.values(data.catalogs).find(c => c.brandId === catalog.brandId && c.region === catalog.region && c.product === catalog.product) as ProviderCatalog | undefined;
  return catalog.protocols ?? known?.protocols ?? {};
}
