import { useEffect, type Dispatch, type SetStateAction } from "react";
import { useCommittedCommand } from "./useCommittedCommand";
import { executeRemoteSend, executeComposerRuntime } from "../app-runtime/remoteComposerOwner";
import type { SessionResource, useSessionOperations } from "../app-runtime/useSessionOperations";
import type { RemoteNavigationCommand } from "./remoteNavigationCommands";
import { reconcileComposerProfile, type ComposerProfile, type ComposerProfilesByTab } from "./composerProfile";
import type { GoalAction } from "./goalAction";
import type { CollaborationMode, QualityFloor, RemoteTabRefView, ToolApprovalMode } from "./types";
import type { RemoteSessionApi } from "./useRemoteSession";

type RemoteProfile = RemoteSessionApi["composerProfile"];

export function remoteRuntimeCommand(input: string):
  | { method: "setModel" | "setEffort"; value: string }
  | { method: "newSession" | "clearSession" }
  | { method: "compact"; value: string }
  | { method: "runManagementCommand"; rehydrate?: boolean }
  | undefined {
  const trimmed = input.trim();
  if (trimmed === "/new") return { method: "newSession" };
  if (trimmed === "/clear") return { method: "clearSession" };
  const match = /^\/(model|effort)\s+(\S+)$/.exec(trimmed);
  if (match) return { method: match[1] === "model" ? "setModel" : "setEffort", value: match[2] };
  const verb = /^\/([^\s]+)/.exec(trimmed)?.[1]?.toLowerCase();
  if (verb === "compact") return { method: "compact", value: trimmed.slice("/compact".length).trim() };
  if (verb === "goal" && remoteGoalCommandStartsTurn(trimmed)) return undefined;
  // These controller verbs are synchronous management operations: they emit
  // notices or mutate session metadata but do not admit a conversational
  // turn. Custom commands, skills, docs queries, and MCP prompts deliberately
  // remain on the ordinary submit path because they can start model work.
  const management = new Set([
    "context", "goal", "memory", "remember", "migrate", "migration",
    "skill", "skills", "plugin", "plugins", "reload-cmd", "hooks", "mcp",
    "provider", "tree", "branch", "switch", "rewind",
  ]);
  if (!verb || !management.has(verb)) return undefined;
  return { method: "runManagementCommand", rehydrate: verb === "branch" || verb === "switch" || verb === "rewind" };
}

function remoteGoalCommandStartsTurn(input: string): boolean {
  const args = input.trim().slice("/goal".length).trim().split(/\s+/).filter(Boolean);
  const flags = new Set(["--strict", "--research", "--auto-research", "--deep", "--simple", "--no-research"]);
  while (args.length > 0 && flags.has(args[0].toLowerCase())) args.shift();
  if (args.length === 0) return false;
  const action = args.join(" ").toLowerCase();
  return !new Set(["status", "clear", "off", "stop", "done", "pause", "resume"]).has(action);
}

export function useRemoteComposerSend(
  activeRemote: RemoteTabRefView | undefined,
  activeTabId: string | undefined,
  collaborationMode: CollaborationMode,
  goal: string,
  session: RemoteSessionApi,
  send: (displayText: string, submitText?: string) => Promise<void>,
  applyGoal: (tabId: string, goal: string) => Promise<unknown>,
  requestClear: () => void,
  ownership: { target: SessionResource; operations: ReturnType<typeof useSessionOperations>; navigateRemote: RemoteNavigationCommand },
) {
  const ports = { compact: session.compact, runManagementCommand: session.runManagementCommand,
    setModel: session.setModel, setEffort: session.setEffort,
    send, applyGoal, requestClear, newSession: ownership.navigateRemote };
  return useCommittedCommand(async (displayText: string, submitText = displayText): Promise<void> => {
    const trimmed = (submitText || displayText).trim();
    const outcome = await ownership.operations(ownership.target, "send", {
      tabId: activeTabId ?? "", remote: activeRemote, display: displayText, submit: submitText, commandText: trimmed,
      command: remoteRuntimeCommand(trimmed), activateGoal: collaborationMode === "goal" && !goal.trim() && Boolean(trimmed), ports,
    }, executeRemoteSend);
    if (outcome.status === "failed") throw outcome.error;
  });
}

export function useRemoteComposerProfileSync(options: {
  activeTabId?: string;
  remote: boolean;
  remoteProfile: RemoteProfile;
  collaborationMode: CollaborationMode;
  toolApprovalMode: ToolApprovalMode;
  goal: string;
  qualityFloor: QualityFloor;
  pending: ComposerProfile["pending"];
  setProfiles: Dispatch<SetStateAction<ComposerProfilesByTab>>;
}): boolean {
  const { activeTabId, remote, remoteProfile, collaborationMode, toolApprovalMode, goal, qualityFloor, pending, setProfiles } = options;
  useEffect(() => {
    if (!activeTabId || !remote || !remoteProfile) return;
    setProfiles((current) => {
      const existing = current[activeTabId];
      const backend: ComposerProfile = {
        collaborationMode: remoteProfile.collaborationMode,
        goalDraftMode: false,
        toolApprovalMode: remoteProfile.toolApprovalMode,
        goal: remoteProfile.goal,
        qualityFloor: remoteProfile.qualityFloor,
        pending: {},
      };
      const next = reconcileComposerProfile(existing, backend);
      return existing === next ? current : { ...current, [activeTabId]: next };
    });
  }, [activeTabId, remote, remoteProfile, setProfiles]);

  return !remote || Boolean(remoteProfile
    && (pending.collaborationMode || collaborationMode === remoteProfile.collaborationMode)
    && (pending.toolApprovalMode || toolApprovalMode === remoteProfile.toolApprovalMode)
    && (pending.goal || goal === remoteProfile.goal)
    && (pending.qualityFloor || qualityFloor === remoteProfile.qualityFloor));
}

export function useRemoteComposerRuntimeActions(options: {
  target: SessionResource;
  operations: ReturnType<typeof useSessionOperations>;
  remote: boolean;
  session: RemoteSessionApi;
  runGoalAction: (action: GoalAction) => void;
  pauseLocal: (tabId: string) => Promise<unknown>;
  resumeLocal: (tabId: string) => Promise<unknown>;
  setLocalEffort: (tabId: string, level: string) => Promise<void>;
  showError: (message: string) => void;
}) {
  const { target, operations, remote, session, runGoalAction, pauseLocal, resumeLocal, setLocalEffort, showError } = options;
  const ports = { pauseGoal: session.pauseGoal, resumeGoal: session.resumeGoal, setEffort: session.setEffort,
    pauseLocal, resumeLocal, effortLocal: setLocalEffort };
  const execute = useCommittedCommand(async (action: "pause" | "resume" | "effort", level?: string) => {
    const outcome = await operations(target, action === "effort" ? "effort" : "goal-lifecycle",
      { tabId: target.tabId, remote, action, level, ports }, executeComposerRuntime);
    if (outcome.status === "failed") throw outcome.error;
  });
  const pauseGoal = useCommittedCommand(() => runGoalAction(() => execute("pause")));
  const resumeGoal = useCommittedCommand(() => runGoalAction(() => execute("resume")));
  const report = useCommittedCommand((error: unknown) => showError(error instanceof Error ? error.message : String(error)));
  const setEffort = useCommittedCommand((level: string) => { void execute("effort", level).catch(report); });
  return { pauseGoal, resumeGoal, setEffort };
}
