export interface ProviderProtocolEndpoint {
  baseUrl: string;
  source: string;
  checkedOn: string;
  authHeader?: boolean;
  responsesMode?: string;
}

export interface ProviderCatalog {
  protocols?: Record<string, ProviderProtocolEndpoint>;
  brandId: string;
  brandLabel: string;
  region: string;
  product: string;
  format: string;
  baseUrl: string;
}

export interface ProviderPresetView {
  catalog?: ProviderCatalog;
  id: string;
  label: string;
  description: string;
  keyEnv: string;
  recommended?: boolean;
  billingMode?: string;
  displayGroup?: string;
  displaySection?: string;
  displayTier?: "primary" | "advanced" | "compatibility" | string;
  routeKind?: string;
  optional?: boolean;
  displayOrder?: number;
  providerNames: string[];
  models: string[];
  added: boolean;
  status?: "available" | "installed" | "installed_modified" | "partial" | "name_conflict" | "similar_existing";
  statusProviderNames?: string[];
  missingProviderNames?: string[];
  keySet: boolean;
  requiresKey?: boolean;
  configured?: boolean;
  keySource?: string;
  keySourcePath?: string;
}
