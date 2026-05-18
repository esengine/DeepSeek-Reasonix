import { type LoadedQQConfig, type QQBotConfig, loadQQConfig, saveQQConfig } from "../config.js";
import { describeQQAccess } from "../qq/access.js";

export interface DesktopQQSettingsState extends Omit<LoadedQQConfig, "sandbox" | "enabled"> {
  sandbox: boolean;
  enabled: boolean;
  configured: boolean;
  connected: boolean;
  appIdPreview?: string;
  access: string;
}

export interface DesktopQQSettingsPatch {
  appId?: string;
  appSecret?: string;
  sandbox: boolean;
}

function trimOptional(value: string | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed ? trimmed : undefined;
}

function toPreview(appId: string | undefined): string | undefined {
  if (!appId) return undefined;
  return appId.length > 6 ? `${appId.slice(0, 6)}...` : appId;
}

function toAccess(config: QQBotConfig | LoadedQQConfig): string {
  return describeQQAccess({
    ownerOpenId: config.ownerOpenId,
    allowlist: config.allowlist,
  });
}

export function loadDesktopQQState(path?: string): DesktopQQSettingsState {
  const config = loadQQConfig(path);
  const configured = Boolean(config.appId && config.appSecret);
  return {
    ...config,
    sandbox: config.sandbox ?? false,
    enabled: config.enabled === true,
    configured,
    connected: configured && config.enabled === true,
    appIdPreview: toPreview(config.appId),
    access: toAccess(config),
  };
}

export function saveDesktopQQSettings(
  patch: DesktopQQSettingsPatch,
  path?: string,
): DesktopQQSettingsState {
  const existing = loadQQConfig(path);
  saveQQConfig(
    {
      ...existing,
      appId: trimOptional(patch.appId),
      appSecret: trimOptional(patch.appSecret),
      sandbox: patch.sandbox,
    },
    path,
  );
  return loadDesktopQQState(path);
}

export function setDesktopQQEnabled(enabled: boolean, path?: string): DesktopQQSettingsState {
  const existing = loadQQConfig(path);
  if (enabled && !(existing.appId && existing.appSecret)) {
    throw new Error("QQ App ID and App Secret are required.");
  }
  saveQQConfig({ ...existing, enabled }, path);
  return loadDesktopQQState(path);
}
