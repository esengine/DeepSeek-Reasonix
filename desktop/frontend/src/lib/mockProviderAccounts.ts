import { asArray } from "./array";
import type { ProviderAccountView, SettingsView } from "./types";

export type ProviderAccountBindings = {
  AddProviderPresetAccount(presetID: string, label: string, key: string): Promise<string>;
  SetProviderAccountDefault(providerID: string, accountID: string): Promise<void>;
  SetProviderAccountEnabled(providerID: string, accountID: string, enabled: boolean): Promise<void>;
  RetireProviderAccount(providerID: string, accountID: string): Promise<void>;
  RestoreProviderAccount(providerID: string, accountID: string): Promise<void>;
  SetProviderAccountRouteEnabled(providerID: string, accountID: string, routeID: string, enabled: boolean): Promise<void>;
  RenameProviderAccount(providerID: string, accountID: string, label: string): Promise<void>;
  SetProviderAccountKey(providerID: string, accountID: string, value: string): Promise<string>;
  ClearProviderAccountKey(providerID: string, accountID: string): Promise<void>;
};

export function mockProviderAccountBindings(settings: SettingsView) {
  return {
    async AddProviderPresetAccount(presetID: string, label: string, key: string) {
      const group = settings.providerPresets.find((p) => p.id === presetID)?.accountGroupId || presetID;
      const accountId = label.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-") || "team";
      settings.providerAccounts = asArray(settings.providerAccounts);
      settings.providerAccounts.push({
        providerId: group, accountId, label,
        apiKeyEnv: `${group.toUpperCase().replace(/[^A-Z0-9]+/g, "_")}_API_KEY_${accountId.toUpperCase()}`,
        enabled: true, default: settings.providerAccounts.every((a) => a.providerId !== group),
        keySet: Boolean(key.trim()), providerNames: [],
      });
      return "";
    },
    async SetProviderAccountDefault(providerID: string, accountID: string) {
      settings.providerAccounts = mapAccounts(settings, (a) => a.providerId === providerID ? { ...a, default: a.accountId === accountID } : a);
    },
    async SetProviderAccountEnabled(providerID: string, accountID: string, enabled: boolean) {
      settings.providerAccounts = mapAccounts(settings, (a) => a.providerId === providerID && a.accountId === accountID ? { ...a, enabled } : a);
    },
    async RetireProviderAccount(providerID: string, accountID: string) {
      settings.providerAccounts = mapAccounts(settings, (a) => a.providerId === providerID && a.accountId === accountID ? { ...a, retired: true, enabled: false, default: false } : a);
    },
    async RestoreProviderAccount(providerID: string, accountID: string) {
      settings.providerAccounts = mapAccounts(settings, (a) => a.providerId === providerID && a.accountId === accountID ? { ...a, retired: false, enabled: true, disabledRoutes: [] } : a);
    },
    async SetProviderAccountRouteEnabled(providerID: string, accountID: string, routeID: string, enabled: boolean) {
      settings.providerAccounts = mapAccounts(settings, (a) => {
        if (a.providerId !== providerID || a.accountId !== accountID) return a;
        const disabled = new Set(asArray(a.disabledRoutes));
        if (enabled) disabled.delete(routeID); else disabled.add(routeID);
        return { ...a, disabledRoutes: Array.from(disabled).sort() };
      });
    },
    async RenameProviderAccount(providerID: string, accountID: string, label: string) {
      settings.providerAccounts = mapAccounts(settings, (a) => a.providerId === providerID && a.accountId === accountID ? { ...a, label } : a);
    },
    async SetProviderAccountKey() { return ""; },
    async ClearProviderAccountKey() {},
  };
}

function mapAccounts(settings: SettingsView, fn: (account: ProviderAccountView) => ProviderAccountView) {
  return asArray(settings.providerAccounts).map(fn);
}
