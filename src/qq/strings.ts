import { t } from "../i18n/index.js";
import {
  type QQAccessConfig,
  normalizeQQAllowlist,
  normalizeQQOpenId,
  redactQQOpenId,
} from "./access.js";

export type QQSetupStep = "appId" | "appSecret";

export function formatQQModeLabel(codeMode: boolean): string {
  return t(codeMode ? "handlers.qq.modeCode" : "handlers.qq.modeChat");
}

export function formatQQAccessSummary(config: QQAccessConfig): string {
  const ownerOpenId = normalizeQQOpenId(config.ownerOpenId);
  const allowlist = normalizeQQAllowlist(config.allowlist) ?? [];
  const runtimeBoundOpenId = normalizeQQOpenId(config.runtimeBoundOpenId);

  if (ownerOpenId) {
    if (allowlist.length > 0) {
      return t("handlers.qq.accessOwnerWithAllowlist", {
        owner: redactQQOpenId(ownerOpenId),
        count: allowlist.length,
      });
    }
    return t("handlers.qq.accessOwner", {
      owner: redactQQOpenId(ownerOpenId),
    });
  }
  if (allowlist.length > 0) {
    return t("handlers.qq.accessAllowlist", { count: allowlist.length });
  }
  if (runtimeBoundOpenId) {
    return t("handlers.qq.accessRuntime", {
      owner: redactQQOpenId(runtimeBoundOpenId),
    });
  }
  return t("handlers.qq.accessOpen");
}

export function formatQQSetupPrompt(step: QQSetupStep): string {
  return t(step === "appId" ? "handlers.qq.promptAppId" : "handlers.qq.promptAppSecret");
}

export function formatQQSetupWaiting(step: QQSetupStep): string {
  return t(
    step === "appId" ? "handlers.qq.setupWaitingAppId" : "handlers.qq.setupWaitingAppSecret",
  );
}
