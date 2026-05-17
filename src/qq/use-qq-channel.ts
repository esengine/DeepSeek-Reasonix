import { createInterface } from "node:readline";
import { useCallback, useEffect, useMemo, useRef } from "react";
import type { PlanConfirmChoice } from "../cli/ui/PlanConfirm.js";
import type { ReviseChoice } from "../cli/ui/PlanReviseConfirm.js";
import type { ThemeChoice } from "../cli/ui/ThemePicker.js";
import { requestPromptInput } from "../cli/ui/scene/prompt-input-store.js";
import { isIntegratedRendererRequested } from "../cli/ui/scene/trace.js";
import type { SlashResult } from "../cli/ui/slash/types.js";
import { listThemeNames } from "../cli/ui/theme/tokens.js";
import { type CheckpointMeta, fmtAgo, restoreCheckpoint } from "../code/checkpoints.js";
import { loadQQConfig, resolveThemePreference, saveQQConfig } from "../config.js";
import { type SessionInfo, freshSessionName } from "../memory/session.js";
import type { ChoiceOption } from "../tools/choice.js";
import type { PlanStep } from "../tools/plan.js";
import { QQChannel } from "./channel.js";

type QQInteractionKind =
  | "run_command"
  | "run_background"
  | "path_access"
  | "plan_proposed"
  | "plan_checkpoint"
  | "plan_revision"
  | "choice";

type QQSlashInteractionKind =
  | "sessions_picker"
  | "checkpoint_picker"
  | "model_picker"
  | "theme_picker";

interface QQInteractionState {
  kind: QQInteractionKind | null;
  payload: unknown;
}

interface QQSlashInteractionState {
  kind: QQSlashInteractionKind | null;
  payload: unknown;
}

interface QQLogger {
  pushInfo: (text: string) => void;
  pushWarning: (title: string, detail: string) => void;
}

interface UseQQChannelArgs {
  codeMode: boolean;
  initialChannel?: QQChannel;
  log: QQLogger;
  isRawModeSupported: boolean;
  setRawMode: (enabled: boolean) => void;
  setQueuedSubmit: (text: string) => void;
  qqSubmitRef?: { current: ((text: string) => void) | null };
  qqErrorRef?: { current: ((msg: string) => void) | null };
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

export function useQQChannel({
  codeMode,
  initialChannel,
  log,
  isRawModeSupported,
  setRawMode,
  setQueuedSubmit,
  qqSubmitRef,
  qqErrorRef,
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
}: UseQQChannelArgs) {
  const channelRef = useRef<QQChannel | null>(initialChannel ?? null);
  const interactionRef = useRef<QQInteractionState>({ kind: null, payload: null });
  const slashInteractionRef = useRef<QQSlashInteractionState>({ kind: null, payload: null });
  const replyThisTurnRef = useRef(false);

  const promptLine = useCallback(
    async (prompt: string, fallback?: string, opts?: { secret?: boolean }): Promise<string> => {
      // Under REASONIX_RENDERER_INTEGRATED=1 the rust child owns the
      // alt-screen and the keyboard reader on the same fd. readline
      // would collide with it; route through the scene-driven prompt
      // overlay instead — rust composer intercepts Enter and posts the
      // answer back via `prompt-response`.
      if (isIntegratedRendererRequested()) {
        const answer = await requestPromptInput({
          label: prompt.replace(/[:：]\s*$/u, "").trim(),
          defaultValue: fallback,
          secret: opts?.secret,
        });
        if (answer === null) throw new Error("input cancelled");
        return answer.trim() || fallback || "";
      }
      if (isRawModeSupported) setRawMode(false);
      const rl = createInterface({ input: process.stdin, output: process.stdout });
      try {
        const answer = await new Promise<string>((resolve) => {
          rl.question(prompt, resolve);
        });
        return answer.trim() || fallback || "";
      } finally {
        rl.close();
        if (isRawModeSupported) setRawMode(true);
      }
    },
    [isRawModeSupported, setRawMode],
  );

  const sendText = useCallback(
    (message: string) => {
      const send = channelRef.current?.sendResponse(message);
      send?.catch((err) => {
        log.pushWarning("QQ", `sendResponse error: ${(err as Error).message}`);
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

  const connect = useCallback(
    async (args: readonly string[]): Promise<string> => {
      const existing = loadQQConfig();
      const appId =
        args[0]?.trim() ||
        existing.appId ||
        (await promptLine("QQ Open Platform App ID: ", existing.appId));
      const appSecret =
        args[1]?.trim() ||
        existing.appSecret ||
        (await promptLine("QQ Open Platform App Secret: ", existing.appSecret, {
          secret: true,
        }));
      const sandboxArg = args[2]?.trim().toLowerCase();
      const sandbox =
        sandboxArg === "sandbox" ||
        sandboxArg === "true" ||
        sandboxArg === "1" ||
        sandboxArg === "yes"
          ? true
          : sandboxArg === "prod" ||
              sandboxArg === "false" ||
              sandboxArg === "0" ||
              sandboxArg === "no"
            ? false
            : (existing.sandbox ?? false);

      if (!appId || !appSecret) {
        throw new Error("QQ App ID and App Secret are required.");
      }

      saveQQConfig({ appId, appSecret, sandbox, enabled: false });
      if (channelRef.current) {
        saveQQConfig({ appId, appSecret, sandbox, enabled: true });
        return `QQ is already connected (${codeMode ? "code" : "chat"} mode). Auto-start is enabled.`;
      }

      const channel = new QQChannel({
        onSubmitMessage: (message) => setQueuedSubmit(message),
        onError: (message) => log.pushWarning("QQ", message),
      });
      await channel.start();
      channelRef.current = channel;
      saveQQConfig({ appId, appSecret, sandbox, enabled: true });
      return `QQ connected in ${codeMode ? "code" : "chat"} mode. It will auto-start on future launches.`;
    },
    [codeMode, log, promptLine, setQueuedSubmit],
  );

  const disconnect = useCallback(async (): Promise<string> => {
    const existing = loadQQConfig();
    const current = channelRef.current;
    channelRef.current = null;
    if (current) await current.stop();
    saveQQConfig({ ...existing, enabled: false });
    return "QQ disconnected. Auto-start is disabled.";
  }, []);

  const status = useCallback((): string => {
    const config = loadQQConfig();
    const configured = config.appId && config.appSecret ? "configured" : "not configured";
    const connected = channelRef.current ? "connected" : "disconnected";
    const enabled = config.enabled ? "enabled" : "disabled";
    const appId = config.appId ? `${config.appId.slice(0, 6)}...` : "none";
    const sandbox = config.sandbox ? "sandbox" : "production";
    return `QQ: ${connected}, auto-start ${enabled}, credentials ${configured}, appId ${appId}, ${sandbox}, current mode ${codeMode ? "code" : "chat"}.`;
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
      queuedSubmit.startsWith("[QQ] ") &&
      interactionRef.current.kind !== null &&
      pendingGateIdRef.current !== null,
    [pendingGateIdRef],
  );

  const bindTransportRefs = useCallback(() => {
    if (!qqSubmitRef || !qqErrorRef) return () => undefined;
    qqSubmitRef.current = setQueuedSubmit;
    qqErrorRef.current = (msg) => log.pushWarning("QQ", msg);
    return () => {
      qqSubmitRef.current = null;
      qqErrorRef.current = null;
    };
  }, [log, qqErrorRef, qqSubmitRef, setQueuedSubmit]);

  useEffect(() => bindTransportRefs(), [bindTransportRefs]);

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

  const noteTurnFromQQ = useCallback((fromQQ: boolean) => {
    replyThisTurnRef.current = fromQQ;
  }, []);

  const maybeSendFinalReply = useCallback(
    (lastAssistantText: string) => {
      if (channelRef.current && lastAssistantText && replyThisTurnRef.current) {
        channelRef.current.sendResponse(lastAssistantText).catch((err) => {
          log.pushWarning("QQ", `sendResponse error: ${(err as Error).message}`);
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
      interactionRef.current = { kind: kind as QQInteractionKind, payload };

      let qqMessage = "";
      switch (kind) {
        case "run_command":
        case "run_background": {
          const p = payload as { command: string };
          qqMessage = `Need confirmation\n\nCommand: \`${p.command}\`\n\nReply with:\n1. Run once\n2. Always allow\n3. Deny`;
          break;
        }
        case "path_access": {
          const p = payload as { path: string; intent: "read" | "write"; toolName: string };
          const intentText = p.intent === "read" ? "Read" : "Write";
          qqMessage = `Need file access confirmation\n\nAction: ${intentText}\nPath: ${p.path}\nTool: ${p.toolName}\n\nReply with:\n1. Run once\n2. Always allow\n3. Deny`;
          break;
        }
        case "plan_proposed": {
          const p = payload as { plan: string };
          qqMessage = `Plan confirmation\n\n${p.plan}\n\nReply with:\n1. Approve\n2. Refine\n3. Cancel`;
          break;
        }
        case "plan_checkpoint": {
          const p = payload as { title?: string; result: string };
          const completed = completedStepIdsRef.current.size;
          const total = planStepsRef.current?.length ?? 0;
          qqMessage = `Step complete (${completed}/${total})\n\n${p.title ? `Step: ${p.title}\n` : ""}Result: ${p.result}\n\nReply with:\n1. Continue\n2. Revise\n3. Stop`;
          break;
        }
        case "plan_revision": {
          const p = payload as { reason: string };
          qqMessage = `Plan revision proposed\n\n${p.reason}\n\nReply with:\n1. Accept\n2. Reject\n3. Cancel`;
          break;
        }
        case "choice": {
          const p = payload as { question: string; options: ChoiceOption[]; allowCustom: boolean };
          const optionsList = p.options.map((opt, idx) => `${idx + 1}. ${opt.title}`).join("\n");
          qqMessage = `Please choose\n\n${p.question}\n\nOptions:\n${optionsList}${p.allowCustom ? "\n\n(You can also reply with custom text.)" : ""}`;
          break;
        }
      }
      if (qqMessage) sendText(qqMessage);
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

      const fromQQ = text.startsWith("[QQ] ");
      if (fromQQ) {
        text = text.slice(5).trimStart() || text;
        if (consumeSlashReply(text) || consumePauseReply(text)) {
          return { handled: true, fromQQ, text };
        }
      }

      return { handled: false, fromQQ, text };
    },
    [consumePauseReply, consumeSlashReply],
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
      noteTurnFromQQ,
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
      noteTurnFromQQ,
      notifyTerminalOnly,
      parseSubmit,
      resetInteractions,
      sendInfo,
      sendText,
      status,
    ],
  );
}
