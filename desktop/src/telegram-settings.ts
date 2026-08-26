import { t } from "./i18n";
import { loadTelegramConfig, saveTelegramConfig } from "../../src/config.js";

export interface TelegramDesktopSettingsState {
  botToken?: string;
  enabled: boolean;
  configured: boolean;
  runtimeState: "disconnected" | "connecting" | "connected" | "failed";
  lastError?: string;
  botTokenPreview?: string;
  access: string;
}

export function maskTelegramBotToken(token: string | undefined): string | undefined {
  if (!token) return undefined;
  return token.length > 6 ? `${token.slice(0, 6)}...` : token;
}

export function getTelegramConnectIntent(tg: TelegramDesktopSettingsState): "configure" | "connect" {
  return tg.configured ? "connect" : "configure";
}

export function getTelegramStatusLabel(tg: TelegramDesktopSettingsState): string {
  switch (tg.runtimeState) {
    case "connected":
      return t("settings.telegramConnected");
    case "connecting":
      return t("settings.telegramConnecting");
    case "failed":
      return t("settings.telegramFailed");
    default:
      return t("settings.telegramDisconnected");
  }
}

export function describeTelegramAccessLabel(access: string): string {
  const owner = /^owner (.+?)(?:, allowlist (\d+))?$/.exec(access);
  if (owner) {
    const userId = owner[1] ?? "unknown";
    const count = owner[2];
    if (count) return t("settings.telegramAccessOwnerWithAllowlist", { userId, count });
    return t("settings.telegramAccessOwner", { userId });
  }

  const allowlist = /^allowlist (\d+)$/.exec(access);
  if (allowlist) return t("settings.telegramAccessAllowlist", { count: allowlist[1] ?? "0" });

  const runtime = /^first-sender \(runtime only, (.+)\)$/.exec(access);
  if (runtime) return t("settings.telegramAccessRuntime", { userId: runtime[1] ?? "unknown" });

  return t("settings.telegramAccessOpen");
}

export function describeTelegramRowSummary(tg: TelegramDesktopSettingsState): string {
  if (!tg.configured) return t("settings.telegramSummaryMissing");
  const token = tg.botTokenPreview ?? maskTelegramBotToken(tg.botToken) ?? "unknown";
  return t("settings.telegramSummaryDetail", {
    token: t("settings.telegramSummaryToken", { token }),
    access: describeTelegramAccessLabel(tg.access),
  });
}

export function loadDesktopTelegramState(): TelegramDesktopSettingsState {
  const config = loadTelegramConfig();
  return {
    botToken: config.botToken,
    enabled: config.enabled ?? false,
    configured: !!config.botToken,
    runtimeState: "disconnected",
    botTokenPreview: maskTelegramBotToken(config.botToken),
    access: "access control configured",
  };
}

export function saveDesktopTelegramSettings(config: {
  botToken?: string;
  enabled?: boolean;
}): void {
  saveTelegramConfig({
    botToken: config.botToken,
    enabled: config.enabled ?? true,
  });
}

export function setDesktopTelegramEnabled(enabled: boolean): void {
  saveDesktopTelegramSettings({ enabled });
}
