import { t, type DictKey } from "./i18n";
import { errorMessage } from "./controllerNotices";
import type { MessageActionScope } from "./messageActions";
import { app } from "./bridge";
import type { TabMeta } from "./types";

export async function restoreNavigationBackend(sourceTabId: string, targetTabId: string, sourceTab?: TabMeta): Promise<{
  restoredTabId: string;
  restoredMeta?: TabMeta;
} | null> {
  if (sourceTabId !== targetTabId && await app.SetActiveTab(sourceTabId).then(() => true).catch(() => false)) {
    return { restoredTabId: sourceTabId };
  }
  if (!sourceTab?.topicId) return null;
  try {
    const restoredMeta = sourceTab.sessionPath
      ? await app.OpenTopicSession(sourceTab.scope, sourceTab.workspaceRoot, sourceTab.topicId, sourceTab.sessionPath)
      : await app.ActivateTopic(sourceTab.scope, sourceTab.workspaceRoot, sourceTab.topicId, "");
    return { restoredTabId: restoredMeta.id, restoredMeta };
  } catch {
    return null;
  }
}

export function messageActionBusyText(scope: MessageActionScope): string {
  switch (scope) {
    case "fork":
    case "fork-worktree": return t("rewind.busyFork");
    case "summ-from": return t("rewind.busySummFrom");
    case "summ-upto": return t("rewind.busySummUpto");
    case "conversation": return t("rewind.busyConversation");
    case "code": return t("rewind.busyCode");
    default: return t("rewind.busyBoth");
  }
}

type SettingNoticeKeys = {
  busy: DictKey;
  busyRunning: DictKey;
  busyPrompt: DictKey;
  busyJobs: DictKey;
  leaseHeld: DictKey;
  starting: DictKey;
  startupFailed: DictKey;
  retry: DictKey;
  failed: DictKey;
};

function settingSwitchNoticeText(err: unknown, setting: "effort" | "model" | "token mode", keys: SettingNoticeKeys): string {
  const msg = errorMessage(err).trim() || "unknown error";
  const lower = msg.toLowerCase();
  if (lower.includes("finish or cancel") && lower.includes(`before changing ${setting}`)) {
    const detail = /running=(true|false);\s*pending_prompt=(true|false);\s*background_jobs=(\d+)/i.exec(msg);
    if (detail?.[2] === "true") return t(keys.busyPrompt);
    if (detail?.[1] === "true") return t(keys.busyRunning);
    const jobs = Number(detail?.[3] ?? 0);
    if (jobs > 0) return t(keys.busyJobs, { n: jobs });
    return t(keys.busy);
  }
  if (lower.includes("already open in another reasonix window") || lower.includes("session lease held")) return t(keys.leaseHeld);
  if (lower.includes("workspace is still starting")) return t(keys.starting);
  if (lower.startsWith("workspace failed to start")) return t(keys.startupFailed, { err: msg });
  if (lower.includes(`changed while switching ${setting}`) || (lower.includes("tab ") && lower.includes("not found"))) return t(keys.retry);
  return t(keys.failed, { err: msg });
}

export function effortSwitchNoticeText(err: unknown): string {
  return settingSwitchNoticeText(err, "effort", {
    busy: "status.effortSwitchBusy",
    busyRunning: "status.effortSwitchBusyRunning",
    busyPrompt: "status.effortSwitchBusyPrompt",
    busyJobs: "status.effortSwitchBusyJobs",
    leaseHeld: "status.effortSwitchLeaseHeld",
    starting: "status.effortSwitchStarting",
    startupFailed: "status.effortSwitchStartupFailed",
    retry: "status.effortSwitchRetry",
    failed: "status.effortSwitchFailed",
  });
}

export function modelSwitchNoticeText(err: unknown): string {
  const msg = errorMessage(err).trim() || "unknown error";
  const unknownModel = /^unknown model (.+)$/i.exec(msg);
  if (unknownModel) return t("status.modelSwitchUnknown", { model: unknownModel[1] });
  const unavailable = /^model (.+) is not available because provider (.+) is not added$/i.exec(msg);
  if (unavailable) return t("status.modelSwitchProviderUnavailable", { model: unavailable[1], provider: unavailable[2] });
  return settingSwitchNoticeText(msg, "model", {
    busy: "status.modelSwitchBusy",
    busyRunning: "status.modelSwitchBusyRunning",
    busyPrompt: "status.modelSwitchBusyPrompt",
    busyJobs: "status.modelSwitchBusyJobs",
    leaseHeld: "status.modelSwitchLeaseHeld",
    starting: "status.modelSwitchStarting",
    startupFailed: "status.modelSwitchStartupFailed",
    retry: "status.modelSwitchRetry",
    failed: "status.modelSwitchFailed",
  });
}
