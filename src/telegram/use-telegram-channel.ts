import { useCallback, useEffect, useMemo, useRef } from "react";
import type { PlanConfirmChoice } from "../cli/ui/PlanConfirm.js";
import type { ReviseChoice } from "../cli/ui/PlanReviseConfirm.js";
import type { ThemeChoice } from "../cli/ui/ThemePicker.js";
import type { SlashResult } from "../cli/ui/slash/types.js";
import { listThemeNames } from "../cli/ui/theme/tokens.js";
import { type CheckpointMeta, fmtAgo, restoreCheckpoint } from "../code/checkpoints.js";
import { loadTelegramConfig, resolveThemePreference, saveTelegramConfig } from "../config.js";
import { t } from "../i18n/index.js";
import { type SessionInfo, freshSessionName } from "../memory/session.js";
import type { ChoiceOption } from "../tools/choice.js";
import type { PlanStep } from "../tools/plan.js";
import type { TelegramAccessConfig } from "./access.js";
import type { TelegramInlineButton } from "./bot.js";
import { TelegramChannel } from "./channel.js";
import {
  type TelegramSetupStep,
  formatTelegramAccessSummary,
  formatTelegramModeLabel,
  formatTelegramSetupPrompt,
  formatTelegramSetupWaiting,
} from "./strings.js";

type TelegramInteractionKind =
  | "run_command"
  | "run_background"
  | "path_access"
  | "plan_proposed"
  | "plan_checkpoint"
  | "plan_revision"
  | "choice";

type TelegramSlashInteractionKind =
  | "sessions_picker"
  | "checkpoint_picker"
  | "model_picker"
  | "theme_picker";

interface TelegramInteractionState {
  kind: TelegramInteractionKind | null;
  payload: unknown;
  confirmationId: string | null;
}

interface TelegramSlashInteractionState {
  kind: TelegramSlashInteractionKind | null;
  payload: unknown;
}

interface PendingTelegramConnectSetup {
  step: TelegramSetupStep;
  botToken?: string;
  ownerUserId?: string;
  allowlist?: readonly string[];
  resolve: (message: string) => void;
  reject: (error: Error) => void;
  promise: Promise<string>;
}

interface TelegramLogger {
  pushInfo: (text: string) => void;
  pushWarning: (title: string, detail: string) => void;
}

interface UseTelegramChannelArgs {
  codeMode: boolean;
  initialChannel?: TelegramChannel;
  log: TelegramLogger;
  setQueuedSubmit: (text: string) => void;
  telegramSubmitRef?: { current: ((text: string) => void) | null };
  telegramErrorRef?: { current: ((msg: string) => void) | null };
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

function telegramCallbackData(confirmationId: string, choice: string): string {
  return `tg:${confirmationId}:${choice}`;
}

function parseTelegramCallbackData(
  text: string,
): { confirmationId: string; choice: string } | null {
  const match = /^tg:([A-Za-z0-9_-]+):(.+)$/.exec(text);
  if (!match) return null;
  return { confirmationId: match[1] ?? "", choice: match[2] ?? "" };
}

export function useTelegramChannel({
  codeMode,
  initialChannel,
  log,
  setQueuedSubmit,
  telegramSubmitRef,
  telegramErrorRef,
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
}: UseTelegramChannelArgs) {
  const channelRef = useRef<TelegramChannel | null>(initialChannel ?? null);
  const interactionRef = useRef<TelegramInteractionState>({
    kind: null,
    payload: null,
    confirmationId: null,
  });
  const slashInteractionRef = useRef<TelegramSlashInteractionState>({
    kind: null,
    payload: null,
  });
  const replyThisTurnRef = useRef(false);
  const nextConfirmationIdRef = useRef(0);
  const pendingConnectSetupRef = useRef<PendingTelegramConnectSetup | null>(null);

  const sendText = useCallback(
    (message: string, buttons?: TelegramInlineButton[][]) => {
      const send = channelRef.current?.sendResponse(message, buttons);
      send?.catch((err) => {
        log.pushWarning("Telegram", `sendResponse error: ${(err as Error).message}`);
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

  const persistTelegramConfig = useCallback(
    (config: {
      botToken: string;
      enabled: boolean;
      ownerUserId?: string;
      allowlist?: readonly string[];
    }) => {
      saveTelegramConfig({
        botToken: config.botToken,
        enabled: config.enabled,
        ownerUserId: config.ownerUserId,
        allowlist: config.allowlist ? [...config.allowlist] : undefined,
      });
    },
    [],
  );

  const completeConnect = useCallback(
    async ({
      botToken,
      ownerUserId,
      allowlist,
    }: {
      botToken: string;
      ownerUserId?: string;
      allowlist?: readonly string[];
    }) => {
      if (!botToken) {
        throw new Error(t("handlers.telegram.credentialsRequired"));
      }

      persistTelegramConfig({
        botToken,
        enabled: false,
        ownerUserId,
        allowlist,
      });
      if (channelRef.current) {
        persistTelegramConfig({
          botToken,
          enabled: true,
          ownerUserId,
          allowlist,
        });
        channelRef.current.refreshAccessConfig();
        return t("handlers.telegram.alreadyConnected", {
          mode: formatTelegramModeLabel(codeMode),
        });
      }

      const channel = new TelegramChannel({
        onSubmitMessage: (message) => setQueuedSubmit(message),
        onError: (message) => log.pushWarning("Telegram", message),
      });
      await channel.start();
      channelRef.current = channel;
      persistTelegramConfig({
        botToken,
        enabled: true,
        ownerUserId,
        allowlist,
      });
      return t("handlers.telegram.connected", {
        mode: formatTelegramModeLabel(codeMode),
      });
    },
    [codeMode, log, persistTelegramConfig, setQueuedSubmit],
  );

  const beginConnectSetup = useCallback(
    ({
      botToken,
      ownerUserId,
      allowlist,
    }: {
      botToken?: string;
      ownerUserId?: string;
      allowlist?: readonly string[];
    }): Promise<string> => {
      const current = pendingConnectSetupRef.current;
      if (current) {
        log.pushInfo(formatTelegramSetupWaiting(current.step));
        return current.promise;
      }

      let resolveSetup: ((message: string) => void) | null = null;
      let rejectSetup: ((error: Error) => void) | null = null;
      const promise = new Promise<string>((resolve, reject) => {
        resolveSetup = resolve;
        rejectSetup = reject;
      });
      const step: TelegramSetupStep = "botToken";
      pendingConnectSetupRef.current = {
        step,
        botToken,
        ownerUserId,
        allowlist,
        resolve: (message) => resolveSetup?.(message),
        reject: (error) => rejectSetup?.(error),
        promise,
      };
      log.pushInfo(formatTelegramSetupPrompt(step));
      return promise;
    },
    [log],
  );

  const connect = useCallback(
    async (args: readonly string[]): Promise<string> => {
      const existing = loadTelegramConfig();
      const botToken = args[0]?.trim() || existing.botToken || "";

      if (!botToken) {
        return beginConnectSetup({
          botToken: botToken || undefined,
          ownerUserId: existing.ownerUserId,
          allowlist: existing.allowlist,
        });
      }

      return completeConnect({
        botToken,
        ownerUserId: existing.ownerUserId,
        allowlist: existing.allowlist,
      });
    },
    [beginConnectSetup, completeConnect],
  );

  const disconnect = useCallback(async (): Promise<string> => {
    const pendingSetup = pendingConnectSetupRef.current;
    if (pendingSetup) {
      pendingConnectSetupRef.current = null;
      pendingSetup.reject(new Error(t("handlers.telegram.setupCancelled")));
    }
    const existing = loadTelegramConfig();
    const current = channelRef.current;
    channelRef.current = null;
    if (current) await current.stop();
    saveTelegramConfig({ ...existing, enabled: false });
    return t("handlers.telegram.disconnected");
  }, []);

  const status = useCallback((): string => {
    const config = loadTelegramConfig();
    const configured = !!config.botToken;
    const connected = !!channelRef.current;
    const enabled = !!config.enabled;
    const botToken = config.botToken
      ? `${config.botToken.slice(0, 6)}...`
      : t("handlers.telegram.none");
    const access = channelRef.current
      ? formatTelegramAccessSummary({
          ownerUserId: config.ownerUserId,
          allowlist: config.allowlist,
          runtimeBoundUserId: channelRef.current.getRuntimeBoundUserId(),
        } satisfies TelegramAccessConfig)
      : formatTelegramAccessSummary({
          ownerUserId: config.ownerUserId,
          allowlist: config.allowlist,
        });
    const pendingSetup = pendingConnectSetupRef.current;
    if (pendingSetup) {
      return t("handlers.telegram.statusSetup", {
        step: formatTelegramSetupWaiting(pendingSetup.step),
      });
    }
    return t("handlers.telegram.status", {
      connected: connected
        ? t("handlers.telegram.stateConnected")
        : t("handlers.telegram.stateDisconnected"),
      enabled: enabled ? t("handlers.telegram.stateEnabled") : t("handlers.telegram.stateDisabled"),
      configured: configured
        ? t("handlers.telegram.stateConfigured")
        : t("handlers.telegram.stateNotConfigured"),
      botToken,
      access,
      mode: formatTelegramModeLabel(codeMode),
    });
  }, [codeMode]);

  const resetInteractions = useCallback(() => {
    interactionRef.current = { kind: null, payload: null, confirmationId: null };
    slashInteractionRef.current = { kind: null, payload: null };
    replyThisTurnRef.current = false;
  }, []);

  const clearSlashInteraction = useCallback(() => {
    slashInteractionRef.current = { kind: null, payload: null };
  }, []);

  const canBypassBusy = useCallback(
    (queuedSubmit: string) =>
      queuedSubmit.startsWith("[TG] ") &&
      interactionRef.current.kind !== null &&
      pendingGateIdRef.current !== null,
    [pendingGateIdRef],
  );

  const bindTransportRefs = useCallback(() => {
    if (!telegramSubmitRef || !telegramErrorRef) return () => undefined;
    telegramSubmitRef.current = setQueuedSubmit;
    telegramErrorRef.current = (msg) => log.pushWarning("Telegram", msg);
    return () => {
      telegramSubmitRef.current = null;
      telegramErrorRef.current = null;
    };
  }, [log, telegramErrorRef, telegramSubmitRef, setQueuedSubmit]);

  useEffect(() => bindTransportRefs(), [bindTransportRefs]);

  const beginSessionsPicker = useCallback(
    (sessions: SessionInfo[]) => {
      slashInteractionRef.current = {
        kind: "sessions_picker",
        payload: sessions,
      };
      const lines = sessions.map((s, idx) => `${idx + 1}. ${s.name}`);
      lines.push("N. New session");
      lines.push("Q. Cancel");
      sendText(`Choose a session:\n\n${lines.join("\n")}`);
    },
    [sendText],
  );

  const beginCheckpointPicker = useCallback(
    (checkpoints: CheckpointMeta[]) => {
      slashInteractionRef.current = {
        kind: "checkpoint_picker",
        payload: checkpoints,
      };
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
      const lowerText = text.toLowerCase();
      const pickedIndex = parseIndexedChoice(text);
      switch (slashInteractionRef.current.kind) {
        case "sessions_picker": {
          const sessions = (slashInteractionRef.current.payload as SessionInfo[]) ?? [];
          slashInteractionRef.current = { kind: null, payload: null };
          if (isCancelText(text)) {
            return true;
          }
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
            if (!target) return true;
            if (onSelectSession) {
              onSelectSession(target.name);
              sendText(`Switched to session: ${target.name}`);
            } else {
              sendText(`Switch to session in the terminal: ${target.name}`);
            }
          }
          return true;
        }
        case "checkpoint_picker": {
          const checkpoints = (slashInteractionRef.current.payload as CheckpointMeta[]) ?? [];
          slashInteractionRef.current = { kind: null, payload: null };
          if (isCancelText(text)) {
            return true;
          }
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
          if (isCancelText(text)) {
            return true;
          }
          if (pickedIndex >= 0 && pickedIndex < choices.length) {
            const target = choices[pickedIndex];
            if (!target) return true;
            const message = onModelPick(target);
            log.pushInfo(message);
            sendText(message);
          }
          return true;
        }
        case "theme_picker": {
          const choices = (slashInteractionRef.current.payload as ThemeChoice[]) ?? [];
          slashInteractionRef.current = { kind: null, payload: null };
          if (isCancelText(text)) {
            return true;
          }
          if (pickedIndex >= 0 && pickedIndex < choices.length) {
            const target = choices[pickedIndex];
            if (!target) return true;
            const message = onThemePick(target);
            log.pushInfo(message);
            sendText(message);
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
      const callback = parseTelegramCallbackData(text);
      if (interactionRef.current.kind === null || pendingGateIdRef.current === null) {
        if (callback) {
          sendText("That Telegram confirmation has expired.");
          return true;
        }
        return false;
      }
      if (callback && callback.confirmationId !== interactionRef.current.confirmationId) {
        sendText("That Telegram confirmation has expired.");
        return true;
      }
      const choiceText = callback?.choice ?? text;
      replyThisTurnRef.current = true;
      const followup = stripFollowupPrefix(choiceText);
      const interaction = interactionRef.current;
      interactionRef.current = { kind: null, payload: null, confirmationId: null };

      switch (interaction.kind) {
        case "run_command":
        case "run_background":
          onShellConfirmRef.current(parseRunPermissionChoice(choiceText));
          return true;
        case "path_access":
          onPathConfirmRef.current(parseRunPermissionChoice(choiceText));
          return true;
        case "plan_proposed": {
          const payload = (interaction.payload as { plan?: string }) ?? {};
          const choice = parsePlanChoice(choiceText);
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
          const choice = parseCheckpointChoice(choiceText);
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
          onPlanRevisionRef.current(parseRevisionChoice(choiceText));
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
            if (selected)
              onChoiceResolveRef.current({
                type: "pick",
                optionId: selected.id,
              });
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
      sendText,
    ],
  );

  const noteTurnFromTelegram = useCallback((fromTelegram: boolean) => {
    replyThisTurnRef.current = fromTelegram;
  }, []);

  const maybeSendFinalReply = useCallback(
    (lastAssistantText: string) => {
      if (channelRef.current && lastAssistantText && replyThisTurnRef.current) {
        channelRef.current.sendResponse(lastAssistantText).catch((err) => {
          log.pushWarning("Telegram", `sendResponse error: ${(err as Error).message}`);
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
      const confirmationId = String(++nextConfirmationIdRef.current);
      interactionRef.current = {
        kind: kind as TelegramInteractionKind,
        payload,
        confirmationId,
      };

      let telegramMessage = "";
      let telegramButtons: TelegramInlineButton[][] | undefined;
      switch (kind) {
        case "run_command":
        case "run_background": {
          const p = payload as { command: string };
          telegramMessage = `Need confirmation\n\nCommand: \`${p.command}\``;
          telegramButtons = [
            [
              { text: "✅ Run once", callbackData: telegramCallbackData(confirmationId, "1") },
              { text: "✅ Always allow", callbackData: telegramCallbackData(confirmationId, "2") },
              { text: "❌ Deny", callbackData: telegramCallbackData(confirmationId, "3") },
            ],
          ];
          break;
        }
        case "path_access": {
          const p = payload as {
            path: string;
            intent: "read" | "write";
            toolName: string;
          };
          const intentText = p.intent === "read" ? "Read" : "Write";
          telegramMessage = `Need file access confirmation\n\nAction: ${intentText}\nPath: ${p.path}\nTool: ${p.toolName}`;
          telegramButtons = [
            [
              { text: "✅ Run once", callbackData: telegramCallbackData(confirmationId, "1") },
              { text: "✅ Always allow", callbackData: telegramCallbackData(confirmationId, "2") },
              { text: "❌ Deny", callbackData: telegramCallbackData(confirmationId, "3") },
            ],
          ];
          break;
        }
        case "plan_proposed": {
          const p = payload as { plan: string };
          telegramMessage = `Plan confirmation\n\n${p.plan}`;
          telegramButtons = [
            [
              { text: "Approve", callbackData: telegramCallbackData(confirmationId, "1") },
              { text: "Refine", callbackData: telegramCallbackData(confirmationId, "2") },
              { text: "Cancel", callbackData: telegramCallbackData(confirmationId, "3") },
            ],
          ];
          break;
        }
        case "plan_checkpoint": {
          const p = payload as { title?: string; result: string };
          const completed = completedStepIdsRef.current.size;
          const total = planStepsRef.current?.length ?? 0;
          telegramMessage = `Step complete (${completed}/${total})\n\n${p.title ? `Step: ${p.title}\n` : ""}Result: ${p.result}`;
          telegramButtons = [
            [
              { text: "Continue", callbackData: telegramCallbackData(confirmationId, "1") },
              { text: "Revise", callbackData: telegramCallbackData(confirmationId, "2") },
              { text: "Stop", callbackData: telegramCallbackData(confirmationId, "3") },
            ],
          ];
          break;
        }
        case "plan_revision": {
          const p = payload as { reason: string };
          telegramMessage = `Plan revision proposed\n\n${p.reason}`;
          telegramButtons = [
            [
              { text: "Accept", callbackData: telegramCallbackData(confirmationId, "1") },
              { text: "Reject", callbackData: telegramCallbackData(confirmationId, "2") },
              { text: "Cancel", callbackData: telegramCallbackData(confirmationId, "3") },
            ],
          ];
          break;
        }
        case "choice": {
          const p = payload as {
            question: string;
            options: ChoiceOption[];
            allowCustom: boolean;
          };
          const optionsList = p.options.map((opt, idx) => `${idx + 1}. ${opt.title}`).join("\n");
          telegramMessage = `Please choose\n\n${p.question}\n\nOptions:\n${optionsList}${p.allowCustom ? "\n\n(You can also reply with custom text.)" : ""}`;
          break;
        }
      }
      if (telegramMessage) sendText(telegramMessage, telegramButtons);
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
        : ["deepseek-v4-flash", "deepseek-v4-pro"]) as string[]),
    ],
    [],
  );

  const buildThemeChoices = useCallback((): ThemeChoice[] => ["auto", ...listThemeNames()], []);

  const parseSubmit = useCallback(
    (raw: string) => {
      let text = raw.trim();
      if (!text) return null;

      const fromTelegram = text.startsWith("[TG] ");
      if (!fromTelegram && pendingConnectSetupRef.current) {
        const lower = text.toLowerCase();
        const pending = pendingConnectSetupRef.current;
        if (lower === "/cancel" || lower === "cancel") {
          pendingConnectSetupRef.current = null;
          pending.reject(new Error(t("handlers.telegram.setupCancelled")));
          log.pushInfo(t("handlers.telegram.setupCancelled"));
          return { handled: true, fromTelegram, text };
        }

        pending.botToken = text;
        pendingConnectSetupRef.current = null;
        void completeConnect({
          botToken: pending.botToken ?? "",
          ownerUserId: pending.ownerUserId,
          allowlist: pending.allowlist,
        }).then(pending.resolve, (err) => pending.reject(err as Error));
        return { handled: true, fromTelegram, text };
      }
      if (fromTelegram) {
        text = text.slice(5).trimStart() || text;
        if (consumeSlashReply(text) || consumePauseReply(text)) {
          return { handled: true, fromTelegram, text };
        }
      }

      return { handled: false, fromTelegram, text };
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
      noteTurnFromTelegram,
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
      noteTurnFromTelegram,
      notifyTerminalOnly,
      parseSubmit,
      resetInteractions,
      sendInfo,
      sendText,
      status,
    ],
  );
}
