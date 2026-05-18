import { t } from "./i18n";

export interface QQDesktopSettingsState {
  appId?: string;
  appSecret?: string;
  sandbox: boolean;
  enabled: boolean;
  configured: boolean;
  connected: boolean;
  appIdPreview?: string;
  access: string;
}

export function maskQQAppId(appId: string | undefined): string | undefined {
  if (!appId) return undefined;
  return appId.length > 6 ? `${appId.slice(0, 6)}...` : appId;
}

export function getQQConnectIntent(qq: QQDesktopSettingsState): "configure" | "connect" {
  return qq.configured ? "connect" : "configure";
}

export function getQQStatusLabel(qq: QQDesktopSettingsState): string {
  return qq.connected ? t("settings.qqConnected") : t("settings.qqDisconnected");
}

export function describeQQAccessLabel(access: string): string {
  const owner = /^owner (.+?)(?:, allowlist (\d+))?$/.exec(access);
  if (owner) {
    const openId = owner[1] ?? "unknown";
    const count = owner[2];
    if (count) return t("settings.qqAccessOwnerWithAllowlist", { openId, count });
    return t("settings.qqAccessOwner", { openId });
  }

  const allowlist = /^allowlist (\d+)$/.exec(access);
  if (allowlist) return t("settings.qqAccessAllowlist", { count: allowlist[1] ?? "0" });

  const runtime = /^first-sender \(runtime only, (.+)\)$/.exec(access);
  if (runtime) return t("settings.qqAccessRuntime", { openId: runtime[1] ?? "unknown" });

  return t("settings.qqAccessOpen");
}

export function describeQQRowSummary(qq: QQDesktopSettingsState): string {
  if (!qq.configured) return t("settings.qqSummaryMissing");
  const appId = qq.appIdPreview ?? maskQQAppId(qq.appId) ?? "unknown";
  return t("settings.qqSummaryDetail", {
    appId: t("settings.qqSummaryAppId", { appId }),
    environment: qq.sandbox ? t("settings.qqSandbox") : t("settings.qqProduction"),
    access: describeQQAccessLabel(qq.access),
  });
}
