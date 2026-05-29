import { useCallback, useEffect, useMemo, useRef } from "react";
import type { ReviseChoice } from "../cli/ui/PlanReviseConfirm.js";
import type { ThemeChoice } from "../cli/ui/ThemePicker.js";
import type { SlashResult } from "../cli/ui/slash/types.js";
import { listThemeNames } from "../cli/ui/theme/tokens.js";
import { type CheckpointMeta, fmtAgo, restoreCheckpoint } from "../code/checkpoints.js";
import { loadWeixinConfig, resolveThemePreference, saveWeixinConfig } from "../config.js";
import { t } from "../i18n/index.js";
import { type SessionInfo, freshSessionName } from "../memory/session.js";
import type { ChoiceOption } from "../tools/choice.js";
import type { PlanStep } from "../tools/plan.js";
import type { WeixinAccessConfig } from "./access.js";
import { runWeixinQrLogin } from "./bot.js";
import { WeixinChannel } from "./channel.js";
import {
  type WeixinSetupStep,
  formatWeixinAccessSummary,
  formatWeixinModeLabel,
  formatWeixinSetupPrompt,
  formatWeixinSetupWaiting,
} from "./strings.js";

type WeixinInteractionKind =
  | "run_command"
  | "run_background"
  | "path_access"
  | "plan_proposed"
  | "plan_checkpoint"
  | "plan_revision"
  | "choice";

type WeixinSlashInteractionKind =
  | "sessions_picker"
  | "checkpoint_picker"
  | "model_picker"
  | "theme_picker";

interface WeixinInteractionState {
  kind: WeixinInteractionKind | null;
  payload: unknown;
  confirmationId: string | null;
}

interface WeixinSlashInteractionState {
  kind: WeixinSlashInteractionKind | null;
  payload: unknown;
}

interface PendingWeixinConnectSetup {
  step: WeixinSetupStep;
  token?: string;
  accountId?: string;
  baseUrl?: string;
  ownerUserId?: string;
  allowlist?: readonly string[];
  resolve: (message: string) => void;
  reject: (error: Error) => void;
  promise: Promise<string>;
}

interface WeixinLogger {
  pushInfo: (text: string) => void;
  pushWarning: (title: string, detail: string) => void;
}

interface UseWeixinChannelArgs {
  codeMode: boolean;
  initialChannel?: WeixinChannel;
  log: WeixinLogger;
  setQueuedSubmit: (text: string) => void;
  weixinSubmitRef?: { current: ((text: string) => void) | null };
  weixinErrorRef?: { current: ((msg: string) => void) | null };
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

export function useWeixinChannel({
  codeMode,
  initialChannel,
  log,
  setQueuedSubmit,
  weixinSubmitRef,
  weixinErrorRef,
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
}: UseWeixinChannelArgs) {
  const channelRef = useRef<WeixinChannel | null>(initialChannel ?? null);
  const interactionRef = useRef<WeixinInteractionState>({
    kind: null,
    payload: null,
    confirmationId: null,
  });
  const slashInteractionRef = useRef<WeixinSlashInteractionState>({
    kind: null,
    payload: null,
  });
  const replyThisTurnRef = useRef(false);
  const nextConfirmationIdRef = useRef(0);
  const pendingConnectSetupRef = useRef<PendingWeixinConnectSetup | null>(null);

  const sendText = useCallback(
    (message: string) => {
      const send = channelRef.current?.sendResponse(message);
      send?.catch((err) => {
        log.pushWarning("Weixin", `sendResponse error: ${(err as Error).message}`);
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

  const persistWeixinConfig = useCallback(
    (config: {
      token: string;
      accountId: string;
      baseUrl?: string;
      enabled: boolean;
      ownerUserId?: string;
      allowlist?: readonly string[];
    }) => {
      saveWeixinConfig({
        token: config.token,
        accountId: config.accountId,
        baseUrl: config.baseUrl,
        enabled: config.enabled,
        ownerUserId: config.ownerUserId,
        allowlist: config.allowlist ? [...config.allowlist] : undefined,
      });
    },
    [],
  );

  const completeConnect = useCallback(
    async ({
      token,
      accountId,
      baseUrl,
      ownerUserId,
      allowlist,
    }: {
      token: string;
      accountId: string;
      baseUrl?: string;
      ownerUserId?: string;
      allowlist?: readonly string[];
    }) => {
      if (!token || !accountId) {
        throw new Error(t("handlers.weixin.credentialsRequired"));
      }

      persistWeixinConfig({
        token,
        accountId,
        baseUrl,
        enabled: false,
        ownerUserId,
        allowlist,
      });
      if (channelRef.current) {
        persistWeixinConfig({
          token,
          accountId,
          baseUrl,
          enabled: true,
          ownerUserId,
          allowlist,
        });
        channelRef.current.refreshAccessConfig();
        return t("handlers.weixin.alreadyConnected", {
          mode: formatWeixinModeLabel(codeMode),
        });
      }

      const channel = new WeixinChannel({
        onSubmitMessage: (message) => setQueuedSubmit(message),
        onError: (message) => log.pushWarning("Weixin", message),
      });
      await channel.start();
      channelRef.current = channel;
      persistWeixinConfig({
        token,
        accountId,
        baseUrl,
        enabled: true,
        ownerUserId,
        allowlist,
      });
      return t("handlers.weixin.connected", {
        mode: formatWeixinModeLabel(codeMode),
      });
    },
    [codeMode, log, persistWeixinConfig, setQueuedSubmit],
  );

  const beginConnectSetup = useCallback(
    ({
      token,
      accountId,
      baseUrl,
      ownerUserId,
      allowlist,
    }: {
      token?: string;
      accountId?: string;
      baseUrl?: string;
      ownerUserId?: string;
      allowlist?: readonly string[];
    }): Promise<string> => {
      const current = pendingConnectSetupRef.current;
      if (current) {
        log.pushInfo(formatWeixinSetupWaiting(current.step));
        return current.promise;
      }

      let resolveSetup: ((message: string) => void) | null = null;
      let rejectSetup: ((error: Error) => void) | null = null;
      const promise = new Promise<string>((resolve, reject) => {
        resolveSetup = resolve;
        rejectSetup = reject;
      });
      const step: WeixinSetupStep = "credentials";
      pendingConnectSetupRef.current = {
        step,
        token,
        accountId,
        baseUrl,
        ownerUserId,
        allowlist,
        resolve: (message) => resolveSetup?.(message),
        reject: (error) => rejectSetup?.(error),
        promise,
      };
      log.pushInfo(formatWeixinSetupPrompt(step));
      return promise;
    },
    [log],
  );

  const connect = useCallback(
    async (args: readonly string[]): Promise<string> => {
      const existing = loadWeixinConfig();
      const manualArgs = args[0]?.toLowerCase() === "manual" ? args.slice(1) : args;
      const explicitManual = manualArgs.length >= 2;
      const token = explicitManual ? manualArgs[0]?.trim() || "" : existing.token || "";
      const accountId = explicitManual ? manualArgs[1]?.trim() || "" : existing.accountId || "";
      const baseUrl = explicitManual ? manualArgs[2]?.trim() || existing.baseUrl : existing.baseUrl;

      if (token && accountId) {
        return completeConnect({
          token,
          accountId,
          baseUrl,
          ownerUserId: existing.ownerUserId,
          allowlist: existing.allowlist,
        });
      }

      if (args[0]?.toLowerCase() === "manual") {
        return beginConnectSetup({
          token: token || undefined,
          accountId: accountId || undefined,
          baseUrl,
          ownerUserId: existing.ownerUserId,
          allowlist: existing.allowlist,
        });
      }

      const credentials = await runWeixinQrLogin({
        onInfo: (message) => log.pushInfo(message),
      });
      return completeConnect({
        token: credentials.token,
        accountId: credentials.accountId,
        baseUrl: credentials.baseUrl,
        ownerUserId: existing.ownerUserId ?? credentials.userId,
        allowlist: existing.allowlist,
      });
    },
    [beginConnectSetup, completeConnect, log],
  );

  const disconnect = useCallback(async (): Promise<string> => {
    const pendingSetup = pendingConnectSetupRef.current;
    if (pendingSetup) {
      pendingConnectSetupRef.current = null;
      pendingSetup.reject(new Error(t("handlers.weixin.setupCancelled")));
    }
    const existing = loadWeixinConfig();
    const current = channelRef.current;
    channelRef.current = null;
    if (current) await current.stop();
    saveWeixinConfig({ ...existing, enabled: false });
    return t("handlers.weixin.disconnected");
  }, []);

  const status = useCallback((): string => {
    const config = loadWeixinConfig();
    const configured = !!config.token && !!config.accountId;
    const connected = !!channelRef.current;
    const enabled = !!config.enabled;
    const token = config.token ? `${config.token.slice(0, 6)}...` : t("handlers.weixin.none");
    const access = channelRef.current
      ? formatWeixinAccessSummary({
          ownerUserId: config.ownerUserId,
          allowlist: config.allowlist,
          runtimeBoundUserId: channelRef.current.getRuntimeBoundUserId(),
        } satisfies WeixinAccessConfig)
      : formatWeixinAccessSummary({
          ownerUserId: config.ownerUserId,
          allowlist: config.allowlist,
        });
    const pendingSetup = pendingConnectSetupRef.current;
    if (pendingSetup) {
      return t("handlers.weixin.statusSetup", {
        step: formatWeixinSetupWaiting(pendingSetup.step),
      });
    }
    return t("handlers.weixin.status", {
      connected: connected
        ? t("handlers.weixin.stateConnected")
        : t("handlers.weixin.stateDisconnected"),
      enabled: enabled ? t("handlers.weixin.stateEnabled") : t("handlers.weixin.stateDisabled"),
      configured: configured
        ? t("handlers.weixin.stateConfigured")
        : t("handlers.weixin.stateNotConfigured"),
      token,
      accountId: config.accountId ?? t("handlers.weixin.none"),
      access,
      mode: formatWeixinModeLabel(codeMode),
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
      queuedSubmit.startsWith("[WX] ") &&
      interactionRef.current.kind !== null &&
      pendingGateIdRef.current !== null,
    [pendingGateIdRef],
  );

  const bindTransportRefs = useCallback(() => {
    if (!weixinSubmitRef || !weixinErrorRef) return () => undefined;
    weixinSubmitRef.current = setQueuedSubmit;
    weixinErrorRef.current = (msg) => log.pushWarning("Weixin", msg);
    return () => {
      weixinSubmitRef.current = null;
      weixinErrorRef.current = null;
    };
  }, [log, weixinErrorRef, weixinSubmitRef, setQueuedSubmit]);

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
      if (interactionRef.current.kind === null || pendingGateIdRef.current === null) {
        return false;
      }
      const choiceText = text;
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
    ],
  );

  const noteTurnFromWeixin = useCallback((fromWeixin: boolean) => {
    replyThisTurnRef.current = fromWeixin;
  }, []);

  const maybeSendFinalReply = useCallback(
    (lastAssistantText: string) => {
      if (channelRef.current && lastAssistantText && replyThisTurnRef.current) {
        channelRef.current.sendResponse(lastAssistantText).catch((err) => {
          log.pushWarning("Weixin", `sendResponse error: ${(err as Error).message}`);
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
        kind: kind as WeixinInteractionKind,
        payload,
        confirmationId,
      };

      let weixinMessage = "";
      switch (kind) {
        case "run_command":
        case "run_background": {
          const p = payload as { command: string };
          weixinMessage = `Need confirmation\n\nCommand: \`${p.command}\`\n\nReply with:\n1. ✅ Run once\n2. ✅ Always allow\n3. ❌ Deny`;
          break;
        }
        case "path_access": {
          const p = payload as {
            path: string;
            intent: "read" | "write";
            toolName: string;
          };
          const intentText = p.intent === "read" ? "Read" : "Write";
          weixinMessage = `Need file access confirmation\n\nAction: ${intentText}\nPath: ${p.path}\nTool: ${p.toolName}\n\nReply with:\n1. ✅ Run once\n2. ✅ Always allow\n3. ❌ Deny`;
          break;
        }
        case "plan_proposed": {
          const p = payload as { plan: string };
          weixinMessage = `Plan confirmation\n\n${p.plan}\n\nReply with:\n1. Approve\n2. Refine\n3. Cancel`;
          break;
        }
        case "plan_checkpoint": {
          const p = payload as { title?: string; result: string };
          const completed = completedStepIdsRef.current.size;
          const total = planStepsRef.current?.length ?? 0;
          weixinMessage = `Step complete (${completed}/${total})\n\n${p.title ? `Step: ${p.title}\n` : ""}Result: ${p.result}\n\nReply with:\n1. Continue\n2. Revise\n3. Stop`;
          break;
        }
        case "plan_revision": {
          const p = payload as { reason: string };
          weixinMessage = `Plan revision proposed\n\n${p.reason}\n\nReply with:\n1. Accept\n2. Reject\n3. Cancel`;
          break;
        }
        case "choice": {
          const p = payload as {
            question: string;
            options: ChoiceOption[];
            allowCustom: boolean;
          };
          const optionsList = p.options.map((opt, idx) => `${idx + 1}. ${opt.title}`).join("\n");
          weixinMessage = `Please choose\n\n${p.question}\n\nOptions:\n${optionsList}${p.allowCustom ? "\n\n(You can also reply with custom text.)" : ""}`;
          break;
        }
      }
      if (weixinMessage) sendText(weixinMessage);
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

      const fromWeixin = text.startsWith("[WX] ");
      if (!fromWeixin && pendingConnectSetupRef.current) {
        const lower = text.toLowerCase();
        const pending = pendingConnectSetupRef.current;
        if (lower === "/cancel" || lower === "cancel") {
          pendingConnectSetupRef.current = null;
          pending.reject(new Error(t("handlers.weixin.setupCancelled")));
          log.pushInfo(t("handlers.weixin.setupCancelled"));
          return { handled: true, fromWeixin, text };
        }

        const [token = pending.token ?? "", accountId = pending.accountId ?? "", baseUrl] =
          text.split(/\s+/);
        pendingConnectSetupRef.current = null;
        void completeConnect({
          token,
          accountId,
          baseUrl: baseUrl ?? pending.baseUrl,
          ownerUserId: pending.ownerUserId,
          allowlist: pending.allowlist,
        }).then(pending.resolve, (err) => pending.reject(err as Error));
        return { handled: true, fromWeixin, text };
      }
      if (fromWeixin) {
        text = text.slice(5).trimStart() || text;
        if (consumeSlashReply(text) || consumePauseReply(text)) {
          return { handled: true, fromWeixin, text };
        }
      }

      return { handled: false, fromWeixin, text };
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
      noteTurnFromWeixin,
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
      noteTurnFromWeixin,
      notifyTerminalOnly,
      parseSubmit,
      resetInteractions,
      sendInfo,
      sendText,
      status,
    ],
  );
}
