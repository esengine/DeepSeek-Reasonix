import { type LoadedQQConfig, type QQBotConfig, loadQQConfig, saveQQConfig } from "../config.js";
import { describeQQAccess } from "../qq/access.js";

/** The desktop process is a config editor, not a runtime — only the CLI (`reasonix`) starts an actual QQChannel. `enabledForCli` says the config flag is on; the bot only comes up on the next terminal session. */
export interface DesktopQQSettingsState extends Omit<LoadedQQConfig, "sandbox" | "enabled"> {
  sandbox: boolean;
  enabled: boolean;
  configured: boolean;
  /** Always false: the desktop never holds a live QQ Bot connection (#1317). Kept for protocol back-compat — UIs should read `enabledForCli` instead. */
  connected: boolean;
  /** True iff credentials are saved AND the user has enabled the bot — the next `reasonix` CLI session will auto-start the channel. */
  enabledForCli: boolean;
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
  const enabled = config.enabled === true;
  return {
    ...config,
    sandbox: config.sandbox ?? false,
    enabled,
    configured,
    // Never true — the desktop process doesn't host a QQChannel (#1317).
    connected: false,
    enabledForCli: configured && enabled,
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
