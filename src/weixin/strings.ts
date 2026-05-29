import { t } from "../i18n/index.js";
import {
  type WeixinAccessConfig,
  normalizeWeixinAllowlist,
  normalizeWeixinUserId,
  redactWeixinUserId,
} from "./access.js";

export type WeixinSetupStep = "credentials";

export function formatWeixinModeLabel(codeMode: boolean): string {
  return t(codeMode ? "handlers.weixin.modeCode" : "handlers.weixin.modeChat");
}

export function formatWeixinAccessSummary(config: WeixinAccessConfig): string {
  const ownerUserId = normalizeWeixinUserId(config.ownerUserId);
  const allowlist = normalizeWeixinAllowlist(config.allowlist) ?? [];
  const runtimeBoundUserId = normalizeWeixinUserId(config.runtimeBoundUserId);
  if (ownerUserId) {
    if (allowlist.length > 0) {
      return t("handlers.weixin.accessOwnerWithAllowlist", {
        owner: redactWeixinUserId(ownerUserId),
        count: allowlist.length,
      });
    }
    return t("handlers.weixin.accessOwner", { owner: redactWeixinUserId(ownerUserId) });
  }
  if (allowlist.length > 0) {
    return t("handlers.weixin.accessAllowlist", { count: allowlist.length });
  }
  if (runtimeBoundUserId) {
    return t("handlers.weixin.accessRuntime", { owner: redactWeixinUserId(runtimeBoundUserId) });
  }
  return t("handlers.weixin.accessRequiredShort");
}

export function formatWeixinSetupPrompt(_step: WeixinSetupStep): string {
  return t("handlers.weixin.promptCredentials");
}

export function formatWeixinSetupWaiting(_step: WeixinSetupStep): string {
  return t("handlers.weixin.setupWaitingCredentials");
}
