export interface ProviderAccountView {
  providerId: string; accountId: string; presetId?: string; label: string; apiKeyEnv: string;
  enabled: boolean; default: boolean; retired?: boolean; keySet: boolean;
  keySource?: string; keySourcePath?: string; providerNames: string[]; disabledRoutes?: string[];
}
