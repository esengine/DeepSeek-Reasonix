import { asArray } from "./array";
import { t, type DictKey } from "./i18n";
import type { WireFinalReadiness } from "./types";

export function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  return String(err || "");
}

const noticeCodeKeys: Record<string, DictKey> = {
  final_readiness: "notice.finalReadiness",
  empty_final: "notice.emptyFinal",
  executor_handoff: "notice.executorHandoff",
  tool_budget: "notice.toolBudget",
  prompt_queued: "notice.promptQueued",
  loop_guard: "notice.loopGuard",
  workspace_lease: "notice.workspaceLease",
  cancelled_turn_display: "notice.cancelledTurnDisplay",
  session_recovery_forked: "recovery.noticeSavedCopy",
  session_recovery_adopted: "recovery.noticeAdopted",
  session_recovery_adopted_covered: "recovery.noticeAdoptedCovered",
  session_recovery_depth_cap: "recovery.noticeKeptCurrent",
  session_shutdown_recovery_forked: "recovery.noticeSavedCopy",
  decision_receipt: "notice.decisionReceiptTitle",
  context_editing_fallback: "notice.contextEditingFallback",
};

const streamInterruptReasonCodeKeys: Record<string, DictKey> = {
  stream_interrupted_idle_timeout: "notice.streamInterruptReason.idleTimeout",
  stream_interrupted_premature_eof: "notice.streamInterruptReason.prematureEof",
  stream_interrupted_connection_reset: "notice.streamInterruptReason.connectionReset",
};

export function localizedNoticeText(text: string, code?: string): string {
  if (code === "unapplied_steer") {
    const separator = text.indexOf("\n");
    const guidance = separator >= 0 ? text.slice(separator + 1) : text;
    return t("notice.unappliedSteer", { guidance });
  }
  const streamReasonKey = code ? streamInterruptReasonCodeKeys[code] : undefined;
  if (streamReasonKey) {
    return t("notice.streamInterruptReason", { reason: t(streamReasonKey) });
  }
  const key = code ? noticeCodeKeys[code] : undefined;
  return key ? t(key) : localizedBackendNoticeText(text);
}

const deliveryRequirementKeys: Record<string, DictKey> = {
  project_check: "notice.deliveryRequirementProjectCheck",
  todo: "notice.deliveryRequirementTodo",
  criteria: "notice.deliveryRequirementCriteria",
  verification: "notice.deliveryRequirementVerification",
  review: "notice.deliveryRequirementReview",
  signoff: "notice.deliveryRequirementSignoff",
  action: "notice.deliveryRequirementAction",
  mutation: "notice.deliveryRequirementMutation",
  task: "notice.deliveryRequirementTask",
  capability: "notice.deliveryRequirementCapability",
};

export function readinessMissingIds(readiness: WireFinalReadiness | undefined): string[] {
  return asArray(readiness?.missing).map((id) => String(id));
}

export function deliveryReadinessDetail(readiness: WireFinalReadiness | undefined, fallback = ""): string {
  const labels = asArray(readiness?.missing)
    .map((id) => deliveryRequirementKeys[id])
    .filter((key): key is DictKey => Boolean(key))
    .map((key) => t(key));
  return labels.length === 0
    ? fallback
    : t("notice.deliveryIncompleteMissing", { items: labels.join(t("notice.deliveryRequirementSeparator")) });
}

function localizedSessionAction(action: string): string {
  switch (action.trim()) {
    case "changing model": return t("status.actionChangingModel");
    case "changing effort": return t("status.actionChangingEffort");
    case "rebuilding settings": return t("status.actionRebuildingSettings");
    case "switching sessions": return t("status.actionSwitchingSessions");
    case "switching tabs": return t("status.actionSwitchingTabs");
    case "autosave": return t("status.actionAutosave");
    default: return action.trim() || t("status.actionCurrentSession");
  }
}

function backendNoticeKey(msg: string): DictKey | "" {
  switch (msg) {
    case "Task status needs one more check; asking the assistant to finish or explain what is blocking it.": return "notice.finalReadiness";
    case "No visible answer was produced; asking the assistant to respond again.": return "notice.emptyFinal";
    case "The assistant answered before taking action; asking it to use the required tools.": return "notice.executorHandoff";
    case "Tool round limit reached; asking the assistant to summarize progress.": return "notice.toolBudget";
    case "The assistant is stuck retrying a blocked action; asking it to change approach.": return "notice.loopGuard";
    case "Context is getting large; preserving cache until cleanup is needed.": return "notice.contextLarge";
    case "Context cleanup skipped for now.": return "notice.contextCleanupSkipped";
    case "Automatic context cleanup paused because the context window is too small.": return "notice.contextCleanupPaused";
    case "Context was compacted without a generated summary.": return "notice.compactionNoSummary";
    case "Goal is not ready to complete yet; continuing the remaining work.": return "notice.goalNotReady";
    case "Goal still has unfinished task state; continuing the remaining work.": return "notice.goalUnfinished";
    case "Job artifact migration failed.": return "notice.jobArtifactMigrationFailed";
    case "Background job teardown timed out.": return "notice.jobTeardownTimeout";
    case "Some plan-mode tool settings were ignored.": return "notice.planModeToolSettingsIgnored";
    case "Some plan-mode command settings were ignored.": return "notice.planModeCommandSettingsIgnored";
    case "Config migration did not complete.": return "notice.configMigrationIncomplete";
    case "Selected model is missing its API key.": return "notice.modelMissingApiKey";
    case "An MCP server failed to start.": return "notice.mcpServerFailed";
    case "Some MCP servers failed to start; run /mcp for details.": return "notice.mcpServersFailed";
    case "Guardian was disabled because its model was not found.": return "notice.guardianModelMissing";
    case "Guardian was disabled because it could not start.": return "notice.guardianStartFailed";
    default: return "";
  }
}

export function localizedBackendNoticeText(text: string): string {
  const msg = text.trim();
  const autosave = /^Session autosave failed: (.+)$/s.exec(msg);
  if (autosave) return t("status.sessionAutosaveFailed", { err: autosave[1] });
  const saveBefore = /^Session save failed before (.+?): (.+)$/s.exec(msg);
  if (saveBefore) return t("status.sessionSaveFailedBefore", { action: localizedSessionAction(saveBefore[1]), err: saveBefore[2] });
  const modelFallback = /^model (.+) is no longer available; switched to (.+)$/s.exec(msg);
  if (modelFallback) return t("status.modelFallbackSwitched", { model: modelFallback[1], fallback: modelFallback[2] });
  const backgroundJob = /^background (.+) failed: needs attention$/s.exec(msg);
  if (backgroundJob) return t("notice.backgroundJobFailed", { kind: backgroundJob[1] });
  const canonical = backendNoticeKey(msg);
  if (canonical) return t(canonical);
  if (/^session changed on disk; unsaved local transcript was saved as a conflict copy$/i.test(msg) || /^session changed on disk; unsaved local transcript was saved as recovery branch\b/i.test(msg)) return t("recovery.noticeSavedCopy");
  if (/^repeated save conflicts were detected; saved the current conflict copy in place$/i.test(msg) || /^repeated save conflicts were detected; saved the current conflict copy in an isolated recovery branch$/i.test(msg) || /^session conflicts kept recurring; kept the transcript on the current recovery branch$/i.test(msg)) return t("recovery.noticeKeptCurrent");
  if (/^session changed on disk; adopted the newer transcript \(local changes already covered\)$/i.test(msg)) return t("recovery.noticeAdoptedCovered");
  if (/^session changed on disk; adopted the newer transcript$/i.test(msg)) return t("recovery.noticeAdopted");
  return msg;
}

function recoveryNoticeDedupeKey(text: string, code?: string): string {
  switch (code) {
    case "session_recovery_forked":
    case "session_shutdown_recovery_forked": return "recovery:saved-copy";
    case "session_recovery_depth_cap": return "recovery:kept-current";
    case "session_recovery_adopted_covered": return "recovery:adopted-covered";
    case "session_recovery_adopted": return "recovery:adopted";
  }
  const msg = text.trim();
  if (/^session changed on disk; unsaved local transcript was saved as a conflict copy$/i.test(msg) || /^session changed on disk; unsaved local transcript was saved as recovery branch\b/i.test(msg) || msg === t("recovery.noticeSavedCopy")) return "recovery:saved-copy";
  if (/^repeated save conflicts were detected; saved the current conflict copy in place$/i.test(msg) || /^repeated save conflicts were detected; saved the current conflict copy in an isolated recovery branch$/i.test(msg) || /^session conflicts kept recurring; kept the transcript on the current recovery branch$/i.test(msg) || msg === t("recovery.noticeKeptCurrent")) return "recovery:kept-current";
  if (/^session changed on disk; adopted the newer transcript \(local changes already covered\)$/i.test(msg) || msg === t("recovery.noticeAdoptedCovered")) return "recovery:adopted-covered";
  if (/^session changed on disk; adopted the newer transcript$/i.test(msg) || msg === t("recovery.noticeAdopted")) return "recovery:adopted";
  return "";
}

export function quietTranscriptNoticeKey(text: string, code?: string): string {
  const recovery = recoveryNoticeDedupeKey(text, code);
  if (recovery) return recovery;
  const msg = text.trim();
  if (/^guardian enabled · model=.+$/i.test(msg)) return "startup:guardian-enabled";
  if (/^\d+ MCP server\(s\) failed to start: .+ \u2014 run \/mcp for details$/i.test(msg)) return "startup:mcp-failures";
  const directMCPFailure = /^mcp\s+([A-Za-z0-9._-]+):\s+.+$/i.exec(msg);
  if (directMCPFailure && !["add", "auth", "config", "connect", "import", "mode", "remove"].includes(directMCPFailure[1].toLowerCase())) return "startup:mcp-failure";
  if (/^plugin ".+" has been slow \d+ startups in a row \(last \d+ms, budget \d+ms\); demoting to background startup this session$/i.test(msg)) return "startup:plugin-demote";
  if (/^.+ applied: session refreshed after the lease was released$/i.test(msg)) return "settings:deferred-refresh-applied";
  return "";
}
