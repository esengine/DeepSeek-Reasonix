import { useCallback, useEffect, useMemo, useRef } from "react";
import type { PlanConfirmChoice } from "../cli/ui/PlanConfirm.js";
import type { ReviseChoice } from "../cli/ui/PlanReviseConfirm.js";
import type { ThemeChoice } from "../cli/ui/ThemePicker.js";
import type { SlashResult } from "../cli/ui/slash/types.js";
import { listThemeNames } from "../cli/ui/theme/tokens.js";
import { type CheckpointMeta, fmtAgo, restoreCheckpoint } from "../code/checkpoints.js";
import { loadFeishuConfig, saveFeishuConfig } from "../config.js";
import { t } from "../i18n/index.js";
import { type SessionInfo, freshSessionName } from "../memory/session.js";
import type { ChoiceOption } from "../tools/choice.js";
import type { PlanStep } from "../tools/plan.js";
import type { FeishuAccessConfig } from "./access.js";
import { FeishuChannel } from "./channel.js";
import {
  type FeishuSetupStep,
  formatFeishuAccessSummary,
  formatFeishuModeLabel,
  formatFeishuSetupPrompt,
  formatFeishuSetupWaiting,
} from "./strings.js";

type FeishuInteractionKind =
  | "run_command"
  | "run_background"
  | "path_access"
  | "plan_proposed"
  | "plan_checkpoint"
  | "plan_revision"
  | "choice";

type FeishuSlashInteractionKind =
  | "sessions_picker"
  | "checkpoint_picker"
  | "model_picker"
  | "theme_picker";

interface FeishuInteractionState {
  kind: FeishuInteractionKind | null;
  payload: unknown;
}

interface FeishuSlashInteractionState {
  kind: FeishuSlashInteractionKind | null;
  payload: unknown;
}

interface PendingFeishuConnectSetup {
  step: FeishuSetupStep;
  appId?: string;
  appSecret?: string;
  ownerOpenId?: string;
  allowlist?: readonly string[];
  resolve: (message: string) => void;
  reject: (error: Error) => void;
  promise: Promise<string>;
}

interface FeishuLogger {
  pushInfo: (text: string) => void;
  pushWarning: (title: string, detail: string) => void;
}

interface UseFeishuChannelArgs {
  codeMode: boolean;
  initialChannel?: FeishuChannel;
  log: FeishuLogger;
  setQueuedSubmit: (text: string) => void;
  sessionName?: string | null;
  currentRootDir: string;
  pendingGateIdRef: { current: number | null };
  completedStepIdsRef: { current: Set<string> };
  planStepsRef: { current: PlanStep[] | null };
  onCreateSession?: (name: string) => void;
  onSelectSession?: (name: string) => void;
  onModelPick: (target: string) => string;
  onThemePick: (target: ThemeChoice) => string;
  onShellConfirmRef: {
    current: (choice: "run_once" | "always_allow" | "deny") => void;
  };
  onPathConfirmRef: {
    current: (choice: "run_once" | "always_allow" | "deny") => void;
  };
  onPlanCancelRef: {
    current: () => void | Promise<void>;
  };
  onPlanFeedbackRef: {
    current: (
      feedback: string,
      override: { plan: string; mode: "refine" | "approve" | "reject" },
    ) => void | Promise<void>;
  };
  onCheckpointConfirmRef: {
    current: (choice: "continue" | "revise" | "stop") => void;
  };
  onCheckpointReviseRef: {
    current: (feedback: string, snap: { stepId: string; title?: string }) => void;
  };
  onPlanRevisionRef: {
    current: (choice: ReviseChoice | "cancel") => void;
  };
  onChoiceResolveRef: {
    current: (
      resolution:
        | { type: "pick"; optionId: string }
        | { type: "text"; text: string }
        | { type: "cancel" },
    ) => void;
  };
}

interface RemoteSlashHandlingArgs {
  result: SlashResult;
  codeMode: boolean;
  sessions: SessionInfo[];
  checkpoints: CheckpointMeta[];
  models: string[] | null | undefined;
  restoreCodeOnlyMessage: string;
}

function parseIndexedChoice(text: string): number {
  const rawIndex = text.match(/^(\d+)/)?.[1];
  return rawIndex ? Number.parseInt(rawIndex, 10) - 1 : -1;
}

function isCancelText(text: string): boolean {
  const lower = text.toLowerCase();
  return lower === "q" || lower.includes("cancel") || lower.includes("quit");
}

function isNewText(text: string): boolean {
  const lower = text.toLowerCase();
  return lower === "n" || lower.includes("new");
}

function parseRunPermissionChoice(text: string): "run_once" | "always_allow" | "deny" {
  const lower = text.toLowerCase();
  if (lower.includes("1") || lower.includes("run")) return "run_once";
  if (lower.includes("2") || lower.includes("always")) return "always_allow";
  return "deny";
}

function parsePlanChoice(text: string): "approve" | "refine" | "cancel" {
  const lower = text.toLowerCase();
  if (lower.includes("1") || lower.includes("approve")) return "approve";
  if (lower.includes("2") || lower.includes("refine")) return "refine";
  return "cancel";
}

function parseCheckpointChoice(text: string): "continue" | "revise" | "stop" {
  const lower = text.toLowerCase();
  if (lower.includes("1") || lower.includes("continue")) return "continue";
  if (lower.includes("2") || lower.includes("revise")) return "revise";
  return "stop";
}

function parseRevisionChoice(text: string): ReviseChoice | "cancel" {
  const lower = text.toLowerCase();
  if (lower.includes("1") || lower.includes("accept")) return "accept";
  if (lower.includes("2") || lower.includes("reject")) return "reject";
  return "cancel";
}

function stripFollowupPrefix(text: string): string {
  return text
    .replace(
      /^(?:\d+\s*|approve\s*|refine\s*|cancel\s*|continue\s*|revise\s*|stop\s*|accept\s*|reject\s*|run\s*|always\s*|deny\s*)/iu,
      "",
    )
    .trim();
}

export function useFeishuChannel({
  codeMode,
  initialChannel,
  log,
  setQueuedSubmit,
  sessionName,
  currentRootDir,
  pendingGateIdRef,
  completedStepIdsRef,
  planStepsRef,
  onCreateSession,
  onSelectSession,
  onModelPick,
  onThemePick,
  onShellConfirmRef,
  onPathConfirmRef,
  onPlanCancelRef,
  onPlanFeedbackRef,
  onCheckpointConfirmRef,
  onCheckpointReviseRef,
  onPlanRevisionRef,
  onChoiceResolveRef,
}: UseFeishuChannelArgs) {
  const channelRef = useRef<FeishuChannel | null>(initialChannel ?? null);
  const interactionRef = useRef<FeishuInteractionState>({ kind: null, payload: null });
  const slashInteractionRef = useRef<FeishuSlashInteractionState>({ kind: null, payload: null });
  const replyThisTurnRef = useRef(false);
  const pendingConnectSetupRef = useRef<PendingFeishuConnectSetup | null>(null);

  const sendText = useCallback(
    (message: string) => {
      const send = channelRef.current?.sendResponse(message);
      send?.catch((err) => {
        log.pushWarning("Feishu", `sendResponse error: ${(err as Error).message}`);
      });
    },
    [log],
  );

  const sendInfo = useCallback(
    (message: string) => {
      log.pushInfo(message);
      sendText(message);
    },
    [log, sendText],
  );

  const persistFeishuConfig = useCallback(
    (config: {
      appId: string;
      appSecret: string;
      enabled: boolean;
      ownerOpenId?: string;
      allowlist?: readonly string[];
    }) => {
      saveFeishuConfig({
        appId: config.appId,
        appSecret: config.appSecret,
        enabled: config.enabled,
        ownerOpenId: config.ownerOpenId,
        allowlist: config.allowlist ? [...config.allowlist] : undefined,
      });
    },
    [],
  );

  const completeConnect = useCallback(
    async ({
      appId,
      appSecret,
      ownerOpenId,
      allowlist,
    }: {
      appId: string;
      appSecret: string;
      ownerOpenId?: string;
      allowlist?: readonly string[];
    }) => {
      if (!appId || !appSecret) {
        throw new Error(t("handlers.feishu.credentialsRequired"));
      }

      persistFeishuConfig({
        appId,
        appSecret,
        enabled: false,
        ownerOpenId,
        allowlist,
      });
      if (channelRef.current) {
        persistFeishuConfig({
          appId,
          appSecret,
          enabled: true,
          ownerOpenId,
          allowlist,
        });
        channelRef.current.refreshAccessConfig();
        return t("handlers.feishu.alreadyConnected", {
          mode: formatFeishuModeLabel(codeMode),
        });
      }

      const channel = new FeishuChannel({
        onSubmitMessage: (message) => setQueuedSubmit(message),
        onError: (message) => log.pushWarning("Feishu", message),
      });
      await channel.start();
      channelRef.current = channel;
      persistFeishuConfig({
        appId,
        appSecret,
        enabled: true,
        ownerOpenId,
        allowlist,
      });
      return t("handlers.feishu.connected", {
        mode: formatFeishuModeLabel(codeMode),
      });
    },
    [codeMode, log, persistFeishuConfig, setQueuedSubmit],
  );

  const beginConnectSetup = useCallback(
    ({
      appId,
      appSecret,
      ownerOpenId,
      allowlist,
    }: {
      appId?: string;
      appSecret?: string;
      ownerOpenId?: string;
      allowlist?: readonly string[];
    }): Promise<string> => {
      const current = pendingConnectSetupRef.current;
      if (current) {
        log.pushInfo(formatFeishuSetupWaiting(current.step));
        return current.promise;
      }

      let resolveSetup: ((message: string) => void) | null = null;
      let rejectSetup: ((error: Error) => void) | null = null;
      const promise = new Promise<string>((resolve, reject) => {
        resolveSetup = resolve;
        rejectSetup = reject;
      });
      const step: FeishuSetupStep = appId ? "appSecret" : "appId";
      pendingConnectSetupRef.current = {
        step,
        appId,
        appSecret,
        ownerOpenId,
        allowlist,
        resolve: (message) => resolveSetup?.(message),
        reject: (error) => rejectSetup?.(error),
        promise,
      };
      log.pushInfo(formatFeishuSetupPrompt(step));
      return promise;
    },
    [log],
  );

  const connect = useCallback(
    async (args: readonly string[]): Promise<string> => {
      const existing = loadFeishuConfig();
      const appId = args[0]?.trim() || existing.appId || "";
      const appSecret = args[1]?.trim() || existing.appSecret || "";

      if (!appId || !appSecret) {
        return beginConnectSetup({
          appId: appId || undefined,
          appSecret: appSecret || undefined,
          ownerOpenId: existing.ownerOpenId,
          allowlist: existing.allowlist,
        });
      }

      return completeConnect({
        appId,
        appSecret,
        ownerOpenId: existing.ownerOpenId,
        allowlist: existing.allowlist,
      });
    },
    [beginConnectSetup, completeConnect],
  );

  const disconnect = useCallback(async (): Promise<string> => {
    const pendingSetup = pendingConnectSetupRef.current;
    if (pendingSetup) {
      pendingConnectSetupRef.current = null;
      pendingSetup.reject(new Error(t("handlers.feishu.setupCancelled")));
    }
    const existing = loadFeishuConfig();
    const current = channelRef.current;
    channelRef.current = null;
    if (current) await current.stop();
    saveFeishuConfig({ ...existing, enabled: false });
    return t("handlers.feishu.disconnected");
  }, []);

  const status = useCallback((): string => {
    const config = loadFeishuConfig();
    const configured = Boolean(config.appId && config.appSecret);
    const connected = !!channelRef.current;
    const enabled = !!config.enabled;
    const appId = config.appId ? `${config.appId.slice(0, 6)}...` : t("handlers.feishu.none");
    const access = channelRef.current
      ? formatFeishuAccessSummary({
          ownerOpenId: config.ownerOpenId,
          allowlist: config.allowlist,
          runtimeBoundOpenId: channelRef.current.getRuntimeBoundOpenId(),
        } satisfies FeishuAccessConfig)
      : formatFeishuAccessSummary({
          ownerOpenId: config.ownerOpenId,
          allowlist: config.allowlist,
        });
    const pendingSetup = pendingConnectSetupRef.current;
    if (pendingSetup) {
      return t("handlers.feishu.statusSetup", {
        step: formatFeishuSetupWaiting(pendingSetup.step),
      });
    }
    return t("handlers.feishu.status", {
      connected: connected
        ? t("handlers.feishu.stateConnected")
        : t("handlers.feishu.stateDisconnected"),
      enabled: enabled ? t("handlers.feishu.stateEnabled") : t("handlers.feishu.stateDisabled"),
      configured: configured
        ? t("handlers.feishu.stateConfigured")
        : t("handlers.feishu.stateNotConfigured"),
      appId,
      access,
      mode: formatFeishuModeLabel(codeMode),
    });
  }, [codeMode]);

  const resetInteractions = useCallback(() => {
    interactionRef.current = { kind: null, payload: null };
    slashInteractionRef.current = { kind: null, payload: null };
    replyThisTurnRef.current = false;
  }, []);

  const clearSlashInteraction = useCallback(() => {
    slashInteractionRef.current = { kind: null, payload: null };
  }, []);

  const canBypassBusy = useCallback(
    (queuedSubmit: string) =>
      queuedSubmit.startsWith("[Feishu] ") &&
      interactionRef.current.kind !== null &&
      pendingGateIdRef.current !== null,
    [pendingGateIdRef],
  );

  const beginSessionsPicker = useCallback(
    (sessions: SessionInfo[]) => {
      slashInteractionRef.current = { kind: "sessions_picker", payload: sessions };
      const lines = sessions.map((s, idx) => `${idx + 1}. ${s.name}`);
      lines.push("N. New session");
      lines.push("Q. Cancel");
      sendText(`Choose a session:\n\n${lines.join("\n")}`);
    },
    [sendText],
  );

  const beginCheckpointPicker = useCallback(
    (checkpoints: CheckpointMeta[]) => {
      slashInteractionRef.current = { kind: "checkpoint_picker", payload: checkpoints };
      const lines = checkpoints.map(
        (c, idx) => `${idx + 1}. ${c.name} (${c.id.slice(0, 7)}, ${fmtAgo(c.createdAt)})`,
      );
      lines.push("Q. Cancel");
      sendText(`Choose a checkpoint to restore:\n\n${lines.join("\n")}`);
    },
    [sendText],
  );

  const beginModelPicker = useCallback(
    (models: string[]) => {
      slashInteractionRef.current = { kind: "model_picker", payload: models };
      const lines = models.map((model, idx) => `${idx + 1}. ${model}`);
      lines.push("Q. Cancel");
      sendText(`Choose a model or preset:\n\n${lines.join("\n")}`);
    },
    [sendText],
  );

  const beginThemePicker = useCallback(
    (themes: ThemeChoice[]) => {
      slashInteractionRef.current = { kind: "theme_picker", payload: themes };
      const lines = themes.map((theme, idx) => `${idx + 1}. ${theme}`);
      lines.push("Q. Cancel");
      sendText(`Choose a theme:\n\n${lines.join("\n")}`);
    },
    [sendText],
  );

  const notifyTerminalOnly = useCallback((message: string) => sendText(message), [sendText]);

  const consumeSlashReply = useCallback(
    (text: string): boolean => {
      const pickedIndex = parseIndexedChoice(text);
      switch (slashInteractionRef.current.kind) {
        case "sessions_picker": {
          const sessions = (slashInteractionRef.current.payload as SessionInfo[]) ?? [];
          slashInteractionRef.current = { kind: null, payload: null };
          if (isCancelText(text)) return true;
          if (isNewText(text)) {
            if (onCreateSession) {
              const nextSession = freshSessionName(sessionName ?? undefined);
              onCreateSession(nextSession);
              sendText("Switched to a new session.");
            } else {
              sendText(
                "This runtime cannot switch sessions remotely. Create a new session in the terminal.",
              );
            }
            return true;
          }
          if (pickedIndex >= 0 && pickedIndex < sessions.length) {
            const target = sessions[pickedIndex];
            if (target && onSelectSession) {
              onSelectSession(target.name);
              sendText(`Switched to session: ${target.name}`);
            }
          }
          return true;
        }
        case "checkpoint_picker": {
          const checkpoints = (slashInteractionRef.current.payload as CheckpointMeta[]) ?? [];
          slashInteractionRef.current = { kind: null, payload: null };
          if (isCancelText(text)) return true;
          if (pickedIndex >= 0 && pickedIndex < checkpoints.length) {
            const target = checkpoints[pickedIndex];
            if (!target) return true;
            const result = restoreCheckpoint(currentRootDir, target.id);
            const lines = [
              `Restored "${target.name}" (${target.id.slice(0, 7)}, ${fmtAgo(target.createdAt)})`,
            ];
            if (result.restored.length > 0) lines.push(`Wrote ${result.restored.length} file(s)`);
            if (result.removed.length > 0) lines.push(`Deleted ${result.removed.length} file(s)`);
            if (result.skipped.length > 0) lines.push(`Skipped ${result.skipped.length} file(s)`);
            const message = lines.join("\n");
            log.pushInfo(message);
            sendText(message);
          }
          return true;
        }
        case "model_picker": {
          const choices = (slashInteractionRef.current.payload as string[]) ?? [];
          slashInteractionRef.current = { kind: null, payload: null };
          if (isCancelText(text)) return true;
          if (pickedIndex >= 0 && pickedIndex < choices.length) {
            const target = choices[pickedIndex];
            if (target) {
              const message = onModelPick(target);
              log.pushInfo(message);
              sendText(message);
            }
          }
          return true;
        }
        case "theme_picker": {
          const choices = (slashInteractionRef.current.payload as ThemeChoice[]) ?? [];
          slashInteractionRef.current = { kind: null, payload: null };
          if (isCancelText(text)) return true;
          if (pickedIndex >= 0 && pickedIndex < choices.length) {
            const target = choices[pickedIndex];
            if (target) {
              const message = onThemePick(target);
              log.pushInfo(message);
              sendText(message);
            }
          }
          return true;
        }
        default:
          return false;
      }
    },
    [
      currentRootDir,
      log,
      onCreateSession,
      onModelPick,
      onSelectSession,
      onThemePick,
      sendText,
      sessionName,
    ],
  );

  const consumePauseReply = useCallback(
    (text: string): boolean => {
      if (interactionRef.current.kind === null || pendingGateIdRef.current === null) return false;
      replyThisTurnRef.current = true;
      const followup = stripFollowupPrefix(text);
      const interaction = interactionRef.current;
      interactionRef.current = { kind: null, payload: null };

      switch (interaction.kind) {
        case "run_command":
        case "run_background":
          onShellConfirmRef.current(parseRunPermissionChoice(text));
          return true;
        case "path_access":
          onPathConfirmRef.current(parseRunPermissionChoice(text));
          return true;
        case "plan_proposed": {
          const payload = (interaction.payload as { plan?: string }) ?? {};
          const choice = parsePlanChoice(text);
          if (choice === "cancel") {
            void onPlanCancelRef.current();
          } else {
            void onPlanFeedbackRef.current(followup, {
              plan: payload.plan ?? "",
              mode: choice === "approve" ? "approve" : "refine",
            });
          }
          return true;
        }
        case "plan_checkpoint": {
          const payload = (interaction.payload as { stepId?: string; title?: string }) ?? {};
          const choice = parseCheckpointChoice(text);
          if (choice === "revise") {
            onCheckpointReviseRef.current(followup, {
              stepId: payload.stepId ?? "",
              title: payload.title,
            });
          } else {
            onCheckpointConfirmRef.current(choice);
          }
          return true;
        }
        case "plan_revision":
          onPlanRevisionRef.current(parseRevisionChoice(text));
          return true;
        case "choice": {
          const payload =
            (interaction.payload as {
              options?: ChoiceOption[];
              allowCustom?: boolean;
            }) ?? {};
          const options = payload.options ?? [];
          const pickedIndex = parseIndexedChoice(text);
          if (pickedIndex >= 0 && pickedIndex < options.length) {
            const selected = options[pickedIndex];
            if (selected) onChoiceResolveRef.current({ type: "pick", optionId: selected.id });
            return true;
          }
          for (const option of options) {
            if (text.toLowerCase().includes(option.title.toLowerCase())) {
              onChoiceResolveRef.current({ type: "pick", optionId: option.id });
              return true;
            }
          }
          if (payload.allowCustom) {
            onChoiceResolveRef.current({ type: "text", text });
          } else {
            onChoiceResolveRef.current({ type: "cancel" });
          }
          return true;
        }
        default:
          return false;
      }
    },
    [
      onCheckpointConfirmRef,
      onCheckpointReviseRef,
      onChoiceResolveRef,
      onPathConfirmRef,
      onPlanCancelRef,
      onPlanFeedbackRef,
      onPlanRevisionRef,
      onShellConfirmRef,
      pendingGateIdRef,
    ],
  );

  const noteTurnFromFeishu = useCallback((fromFeishu: boolean) => {
    replyThisTurnRef.current = fromFeishu;
  }, []);

  const maybeSendFinalReply = useCallback(
    (lastAssistantText: string) => {
      if (channelRef.current && lastAssistantText && replyThisTurnRef.current) {
        channelRef.current.sendResponse(lastAssistantText).catch((err) => {
          log.pushWarning("Feishu", `sendResponse error: ${(err as Error).message}`);
        });
      }
    },
    [log],
  );

  const clearTurnReply = useCallback(() => {
    replyThisTurnRef.current = false;
  }, []);

  const handlePauseRequest = useCallback(
    (kind: string, payload: Record<string, unknown>) => {
      if (!channelRef.current) return;
      interactionRef.current = { kind: kind as FeishuInteractionKind, payload };

      let feishuMessage = "";
      switch (kind) {
        case "run_command":
        case "run_background": {
          const p = payload as { command: string };
          feishuMessage = `Need confirmation\n\nCommand: \`${p.command}\`\n\nReply with:\n1. Run once\n2. Always allow\n3. Deny`;
          break;
        }
        case "path_access": {
          const p = payload as { path: string; intent: "read" | "write"; toolName: string };
          const intentText = p.intent === "read" ? "Read" : "Write";
          feishuMessage = `Need file access confirmation\n\nAction: ${intentText}\nPath: ${p.path}\nTool: ${p.toolName}\n\nReply with:\n1. Run once\n2. Always allow\n3. Deny`;
          break;
        }
        case "plan_proposed": {
          const p = payload as { plan: string };
          feishuMessage = `Plan confirmation\n\n${p.plan}\n\nReply with:\n1. Approve\n2. Refine\n3. Cancel`;
          break;
        }
        case "plan_checkpoint": {
          const p = payload as { title?: string; result: string };
          const completed = completedStepIdsRef.current.size;
          const total = planStepsRef.current?.length ?? 0;
          feishuMessage = `Step complete (${completed}/${total})\n\n${p.title ? `Step: ${p.title}\n` : ""}Result: ${p.result}\n\nReply with:\n1. Continue\n2. Revise\n3. Stop`;
          break;
        }
        case "plan_revision": {
          const p = payload as { reason: string };
          feishuMessage = `Plan revision proposed\n\n${p.reason}\n\nReply with:\n1. Accept\n2. Reject\n3. Cancel`;
          break;
        }
        case "choice": {
          const p = payload as { question: string; options: ChoiceOption[]; allowCustom: boolean };
          const optionsList = p.options.map((opt, idx) => `${idx + 1}. ${opt.title}`).join("\n");
          feishuMessage = `Please choose\n\n${p.question}\n\nOptions:\n${optionsList}${p.allowCustom ? "\n\n(You can also reply with custom text.)" : ""}`;
          break;
        }
      }
      if (feishuMessage) sendText(feishuMessage);
    },
    [completedStepIdsRef, planStepsRef, sendText],
  );

  const buildModelChoices = useCallback(
    (models: string[] | null | undefined) => [
      "auto",
      "flash",
      "pro",
      ...((models && models.length > 0
        ? models
        : [
            "deepseek-v4-flash",
            "deepseek-v4-pro",
            "deepseek-chat",
            "deepseek-reasoner",
          ]) as string[]),
    ],
    [],
  );

  const buildThemeChoices = useCallback((): ThemeChoice[] => ["auto", ...listThemeNames()], []);

  const parseSubmit = useCallback(
    (raw: string) => {
      let text = raw.trim();
      if (!text) return null;

      const fromFeishu = text.startsWith("[Feishu] ");
      if (!fromFeishu && pendingConnectSetupRef.current) {
        const lower = text.toLowerCase();
        const pending = pendingConnectSetupRef.current;
        if (lower === "/cancel" || lower === "cancel") {
          pendingConnectSetupRef.current = null;
          pending.reject(new Error(t("handlers.feishu.setupCancelled")));
          log.pushInfo(t("handlers.feishu.setupCancelled"));
          return { handled: true, fromFeishu, text, shouldClearInput: true };
        }

        if (pending.step === "appId") {
          pending.appId = text;
          pending.step = "appSecret";
          log.pushInfo(formatFeishuSetupPrompt("appSecret"));
          return { handled: true, fromFeishu, text, shouldClearInput: true };
        }

        pending.appSecret = text;
        pendingConnectSetupRef.current = null;
        void completeConnect({
          appId: pending.appId ?? "",
          appSecret: pending.appSecret ?? "",
          ownerOpenId: pending.ownerOpenId,
          allowlist: pending.allowlist,
        }).then(pending.resolve, (err) => pending.reject(err as Error));
        return { handled: true, fromFeishu, text, shouldClearInput: true };
      }
      if (fromFeishu) {
        text = text.slice(9).trimStart() || text;
        if (consumeSlashReply(text) || consumePauseReply(text)) {
          return { handled: true, fromFeishu, text };
        }
      }

      return { handled: false, fromFeishu, text };
    },
    [completeConnect, consumePauseReply, consumeSlashReply, log],
  );

  const handleRemoteSlashResult = useCallback(
    ({
      result,
      codeMode: codeModeOn,
      sessions,
      checkpoints,
      models,
      restoreCodeOnlyMessage,
    }: RemoteSlashHandlingArgs): boolean => {
      if (result.openSessionsPicker) {
        beginSessionsPicker(sessions);
        return true;
      }
      if (result.openCheckpointPicker) {
        if (!codeModeOn) {
          sendInfo(restoreCodeOnlyMessage);
          return true;
        }
        beginCheckpointPicker(checkpoints);
        return true;
      }
      if (result.openMcpHub) {
        notifyTerminalOnly("`/mcp` interactive management is currently terminal-only.");
        return true;
      }
      if (result.openModelPicker) {
        beginModelPicker(buildModelChoices(models));
        return true;
      }
      if (result.openThemePicker) {
        beginThemePicker(buildThemeChoices());
        return true;
      }
      if (result.openCopyMode) {
        notifyTerminalOnly("`/copy` follow-up interaction is currently terminal-only.");
        return true;
      }
      if (result.openArgPickerFor) {
        notifyTerminalOnly(
          `\`/${result.openArgPickerFor}\` needs terminal-side argument completion.`,
        );
        return true;
      }
      return false;
    },
    [
      beginCheckpointPicker,
      beginModelPicker,
      beginSessionsPicker,
      beginThemePicker,
      buildModelChoices,
      buildThemeChoices,
      notifyTerminalOnly,
      sendInfo,
    ],
  );

  useEffect(() => {
    return () => {
      pendingConnectSetupRef.current = null;
    };
  }, []);

  return useMemo(
    () => ({
      channelRef,
      connect,
      disconnect,
      status,
      sendInfo,
      sendText,
      resetInteractions,
      clearSlashInteraction,
      canBypassBusy,
      consumeSlashReply,
      consumePauseReply,
      beginSessionsPicker,
      beginCheckpointPicker,
      beginModelPicker,
      beginThemePicker,
      notifyTerminalOnly,
      noteTurnFromFeishu,
      maybeSendFinalReply,
      clearTurnReply,
      handlePauseRequest,
      buildModelChoices,
      buildThemeChoices,
      parseSubmit,
      handleRemoteSlashResult,
    }),
    [
      beginCheckpointPicker,
      beginModelPicker,
      beginSessionsPicker,
      beginThemePicker,
      buildModelChoices,
      buildThemeChoices,
      canBypassBusy,
      clearSlashInteraction,
      clearTurnReply,
      connect,
      consumePauseReply,
      consumeSlashReply,
      disconnect,
      handlePauseRequest,
      handleRemoteSlashResult,
      maybeSendFinalReply,
      noteTurnFromFeishu,
      notifyTerminalOnly,
      parseSubmit,
      resetInteractions,
      sendInfo,
      sendText,
      status,
    ],
  );
}
