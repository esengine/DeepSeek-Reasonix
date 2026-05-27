import { t } from "../i18n/index.js";
import {
  type TelegramAccessConfig,
  normalizeTelegramAllowlist,
  normalizeTelegramUserId,
  redactTelegramUserId,
} from "./access.js";

export type TelegramSetupStep = "botToken";

export function formatTelegramModeLabel(codeMode: boolean): string {
  return t(codeMode ? "handlers.telegram.modeCode" : "handlers.telegram.modeChat");
}

export function formatTelegramAccessSummary(config: TelegramAccessConfig): string {
  const ownerUserId = normalizeTelegramUserId(config.ownerUserId);
  const allowlist = normalizeTelegramAllowlist(config.allowlist) ?? [];
  const runtimeBoundUserId = normalizeTelegramUserId(config.runtimeBoundUserId);

  if (ownerUserId) {
    if (allowlist.length > 0) {
      return t("handlers.telegram.accessOwnerWithAllowlist", {
        owner: redactTelegramUserId(ownerUserId),
        count: allowlist.length,
      });
    }
    return t("handlers.telegram.accessOwner", {
      owner: redactTelegramUserId(ownerUserId),
    });
  }
  if (allowlist.length > 0) {
    return t("handlers.telegram.accessAllowlist", { count: allowlist.length });
  }
  if (runtimeBoundUserId) {
    return t("handlers.telegram.accessRuntime", {
      owner: redactTelegramUserId(runtimeBoundUserId),
    });
  }
  return t("handlers.telegram.accessRequiredShort");
}

export function formatTelegramSetupPrompt(_step: TelegramSetupStep): string {
  return t("handlers.telegram.promptBotToken");
}

export function formatTelegramSetupWaiting(_step: TelegramSetupStep): string {
  return t("handlers.telegram.setupWaitingBotToken");
}
