import { t } from "../i18n/index.js";
import {
  type FeishuAccessConfig,
  normalizeFeishuAllowlist,
  normalizeFeishuOpenId,
  redactFeishuOpenId,
} from "./access.js";

export type FeishuSetupStep = "appId" | "appSecret" | "ownerOpenId";

export function formatFeishuModeLabel(codeMode: boolean): string {
  return t(codeMode ? "handlers.feishu.modeCode" : "handlers.feishu.modeChat");
}

export function formatFeishuAccessSummary(config: FeishuAccessConfig): string {
  const ownerOpenId = normalizeFeishuOpenId(config.ownerOpenId);
  const allowlist = normalizeFeishuAllowlist(config.allowlist) ?? [];

  if (ownerOpenId) {
    if (allowlist.length > 0) {
      return t("handlers.feishu.accessOwnerWithAllowlist", {
        owner: redactFeishuOpenId(ownerOpenId),
        count: allowlist.length,
      });
    }
    return t("handlers.feishu.accessOwner", {
      owner: redactFeishuOpenId(ownerOpenId),
    });
  }
  if (allowlist.length > 0) {
    return t("handlers.feishu.accessAllowlist", { count: allowlist.length });
  }
  return t("handlers.feishu.accessRestricted");
}

export function formatFeishuSetupPrompt(step: FeishuSetupStep): string {
  if (step === "appId") return t("handlers.feishu.promptAppId");
  if (step === "appSecret") return t("handlers.feishu.promptAppSecret");
  return t("handlers.feishu.promptOwnerOpenId");
}

export function formatFeishuSetupWaiting(step: FeishuSetupStep): string {
  if (step === "appId") return t("handlers.feishu.setupWaitingAppId");
  if (step === "appSecret") return t("handlers.feishu.setupWaitingAppSecret");
  return t("handlers.feishu.setupWaitingOwnerOpenId");
}
