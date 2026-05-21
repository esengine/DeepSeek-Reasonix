import { t } from "../i18n/index.js";
import {
  type FeishuAccessConfig,
  normalizeFeishuAllowlist,
  normalizeFeishuOpenId,
  redactFeishuOpenId,
} from "./access.js";

export type FeishuSetupStep = "appId" | "appSecret";

export function formatFeishuModeLabel(codeMode: boolean): string {
  return t(codeMode ? "handlers.feishu.modeCode" : "handlers.feishu.modeChat");
}

export function formatFeishuAccessSummary(config: FeishuAccessConfig): string {
  const ownerOpenId = normalizeFeishuOpenId(config.ownerOpenId);
  const allowlist = normalizeFeishuAllowlist(config.allowlist) ?? [];
  const runtimeBoundOpenId = normalizeFeishuOpenId(config.runtimeBoundOpenId);

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
  if (runtimeBoundOpenId) {
    return t("handlers.feishu.accessRuntime", {
      owner: redactFeishuOpenId(runtimeBoundOpenId),
    });
  }
  return t("handlers.feishu.accessOpen");
}

export function formatFeishuSetupPrompt(step: FeishuSetupStep): string {
  return t(step === "appId" ? "handlers.feishu.promptAppId" : "handlers.feishu.promptAppSecret");
}

export function formatFeishuSetupWaiting(step: FeishuSetupStep): string {
  return t(
    step === "appId"
      ? "handlers.feishu.setupWaitingAppId"
      : "handlers.feishu.setupWaitingAppSecret",
  );
}
