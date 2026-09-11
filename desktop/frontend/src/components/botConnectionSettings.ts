import { asArray } from "../lib/array";
import type { useT } from "../lib/i18n";
import type { BotAccessView, BotConnectionDiagnostic, BotConnectionView, BotSettingsView } from "../lib/types";

export type BotInstallTarget = "qq" | "feishu" | "lark" | "weixin" | "dingtalk";
export type BotOfficialInstallTarget = Exclude<BotInstallTarget, "qq">;

export function diagnosticMessage(diag?: BotConnectionDiagnostic | string): string {
  if (typeof diag === "string") return diag;
  return diag?.message || diag?.status || "";
}

export function diagnosticReportDetail(diag?: BotConnectionDiagnostic | string): string {
  if (typeof diag === "string") return "";
  return diag?.reportDetail || "";
}

export function botTargetLabel(target: BotInstallTarget, t: ReturnType<typeof useT>): string {
  switch (target) {
    case "qq": return "QQ";
    case "lark": return "Lark";
    case "weixin": return t("settings.botWeixin");
    case "dingtalk": return t("settings.botDingtalk");
    default: return t("settings.botFeishu");
  }
}

export function botTargetHint(target: BotInstallTarget, t: ReturnType<typeof useT>): string {
  switch (target) {
    case "qq": return t("settings.botInstallQQHint");
    case "lark": return t("settings.botInstallLarkHint");
    case "weixin": return t("settings.botInstallWeixinHint");
    case "dingtalk": return t("settings.botInstallDingtalkHint");
    default: return t("settings.botInstallFeishuHint");
  }
}

export function qqBotAdded(qq: BotSettingsView["qq"]): boolean {
  return Boolean(qq.enabled || qq.secretSet || qq.appId.trim());
}

export function botAccessEntryCount(access: BotAccessView): number {
  return [
    ...asArray(access.users),
    ...asArray(access.groups),
    ...asArray(access.approvers),
    ...asArray(access.admins),
  ].filter((value) => value.trim()).length;
}

export function botAccessReady(access: BotAccessView): boolean {
  if (access.allowAll || access.pairingEnabled) return true;
  if (!access.enabled) return false;
  return botAccessEntryCount(access) > 0;
}

export function botInstallTargetMatchesConnection(target: BotOfficialInstallTarget, connection: BotConnectionView): boolean {
  if (target === "weixin") return connection.provider === "weixin";
  if (target === "dingtalk") return connection.provider === "dingtalk";
  if (target === "lark") return connection.provider === "feishu" && connection.domain === "lark";
  return connection.provider === "feishu" && connection.domain !== "lark";
}

export function botInstallTargetForConnection(connection: BotConnectionView): BotInstallTarget {
  if (connection.provider === "weixin") return "weixin";
  if (connection.provider === "dingtalk") return "dingtalk";
  if (connection.provider === "feishu" && connection.domain === "lark") return "lark";
  if (connection.provider === "qq") return "qq";
  return "feishu";
}

export function formatInstallUserCode(code: string): string {
  const compact = code.replace(/[^a-z0-9]/gi, "").toUpperCase().slice(0, 8);
  if (compact.length <= 4) return compact;
  return `${compact.slice(0, 4)}-${compact.slice(4)}`;
}

export function formatInstallTimeLeft(seconds: number): string {
  const value = Math.max(0, Math.floor(seconds));
  const minutes = Math.floor(value / 60);
  const rest = value % 60;
  return `${minutes}:${String(rest).padStart(2, "0")}`;
}

export function botConnectionLabel(connection: BotConnectionView, t: ReturnType<typeof useT>): string {
  if (connection.domain === "lark") return "Lark";
  if (connection.provider === "weixin") return t("settings.botWeixin");
  if (connection.provider === "dingtalk") return t("settings.botDingtalk");
  if (connection.provider === "qq") return "QQ";
  return t("settings.botFeishu");
}

export function firstConnectionRemote(connection: BotConnectionView): string {
  return connection.sessionMappings.find((mapping) => mapping.remoteId.trim())?.remoteId ?? "";
}

export function botConnectionScopeLabel(connection: BotConnectionView, t: ReturnType<typeof useT>): string {
  return connection.workspaceRoot.trim() ? t("settings.botScopeProject") : t("settings.botScopeGlobal");
}

export function botConnectionSecretEnv(connection: BotConnectionView): string {
  return connection.provider === "weixin" ? connection.credential.tokenEnv : connection.credential.appSecretEnv;
}

export function botConnectionSecretPatch(connection: BotConnectionView, value: string): Partial<BotConnectionView["credential"]> {
  return connection.provider === "weixin" ? { tokenEnv: value } : { appSecretEnv: value };
}

export function botConnectionCredentialSummary(connection: BotConnectionView, t: ReturnType<typeof useT>): string {
  if (connection.provider === "weixin") {
    return connection.credential.accountId
      ? t("settings.botCredentialAccount", { value: connection.credential.accountId })
      : t("settings.botCredentialLocalWeixin");
  }
  if (connection.credential.appId) {
    if (connection.provider === "dingtalk") {
      return t("settings.botCredentialClientId", { value: connection.credential.appId });
    }
    return t("settings.botCredentialApp", { value: connection.credential.appId });
  }
  return t("settings.botCredentialConfigured");
}
