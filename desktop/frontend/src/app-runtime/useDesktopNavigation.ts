import { useLayoutEffect, useRef, type Dispatch, type SetStateAction } from "react";
import type { Translator } from "../lib/i18n";
import type { useToast } from "../lib/toast";
import type { SessionMeta, TabMeta } from "../lib/types";
import type { RemoteNavigationCommand } from "../lib/remoteNavigationCommands";
import { CommandCancelled, type CommandAuthority, type CommandOutcome } from "../lib/commandOutcome";
import { useCommittedAsyncCommand } from "../lib/useCommittedAsyncCommand";
import { refreshHistoryProjection, type HistoryViewState } from "./historyViewProjection";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { enqueueNavigationRequest, type NavigationCoalescingRefs } from "../lib/openTopicCoalescing";
import { useResourceOperations, type SessionResource, type SessionOperationAuthority } from "./useResourceOperations";
import { executeDesktopNavigation, type DesktopNavigationCapture, type DesktopNavigationIntent, type DesktopNavigationPorts, type NavigationNotice } from "./desktopNavigationOwner";

type QueueInput = { capture: DesktopNavigationCapture; authority: SessionOperationAuthority; result: { error?: unknown; tab?: TabMeta } };
async function runQueuedRequest(request: QueueInput) {
  try { request.result.tab = await executeDesktopNavigation(request.capture, request.authority); }
  catch (error) { request.result.error = error; }
}
async function executeQueuedNavigation(input: { capture: DesktopNavigationCapture; queue: NavigationCoalescingRefs<QueueInput> }, authority: SessionOperationAuthority) {
  const result: QueueInput["result"] = {};
  await enqueueNavigationRequest(input.queue, { capture: input.capture, authority, result }, runQueuedRequest);
  if (result.error) throw result.error;
  return result.tab;
}
async function startRemoteNavigation(input: {
  intent: DesktopNavigationIntent;
  noteIntent(): number; showChat(): void;
  execute(intent: DesktopNavigationIntent, seq: number): Promise<CommandOutcome<TabMeta | undefined>>;
}, authority: CommandAuthority) {
  authority.checkpoint();
  input.showChat();
  const outcome = await input.execute(input.intent, input.noteIntent());
  if (outcome.status === "failed") throw outcome.error;
  if (outcome.status === "cancelled") throw new CommandCancelled(outcome.reason);
  return outcome.value;
}

/** Owns the existing last-click-wins queue; no App render or view model is queued. */
export function useDesktopNavigation(input: {
  visible: SessionResource;
  singleSurface: boolean;
  ports: Omit<DesktopNavigationPorts, "reveal" | "projectChanged" | "closeHistory" | "notice" | "applyHistorySessions">;
  setTabRevealSignal: Dispatch<SetStateAction<number>>;
  setTranscriptRevealSignal: Dispatch<SetStateAction<number>>;
  setProjectRevision: Dispatch<SetStateAction<number>>;
  setHistory: Dispatch<SetStateAction<HistoryViewState | null>>;
  t: Translator;
  showToast: ReturnType<typeof useToast>["showToast"];
  noteIntent(): number;
  beginSurface(seq: number): void;
  settleSurface(seq: number): void;
  showChat(): void;
}) {
  const operations = useResourceOperations({ visible: input.visible });
  const reveal = useCommittedCommand(() => { input.setTabRevealSignal(value => value + 1); input.setTranscriptRevealSignal(value => value + 1); });
  const projectChanged = useCommittedCommand(() => input.setProjectRevision(value => value + 1));
  const closeHistory = useCommittedCommand(() => input.setHistory(null));
  const applyHistorySessions = useCommittedCommand((sessions: SessionMeta[]) => input.setHistory(current => refreshHistoryProjection(current, sessions)));
  const notice = useCommittedCommand((notice: NavigationNotice) => {
    input.showToast("key" in notice ? input.t(notice.key, notice.params) : notice.message, notice.tone, { durationMs: notice.durationMs });
  });
  const queueRef = useRef<NavigationCoalescingRefs<QueueInput> | null>(null);
  if (!queueRef.current) queueRef.current = { seqRef: { current: 0 }, runningRef: { current: false }, pendingRef: { current: null } };
  const queue = queueRef.current;
  const settle = useCommittedCommand(input.settleSurface);
  const executeWithIntent = useCommittedCommand(async (intent: DesktopNavigationIntent, navigationIntentSeq: number) => {
    input.beginSurface(navigationIntentSeq);
    try {
      return await operations({ kind: "application" }, "navigation", {
        queue, capture: { intent, navigationIntentSeq, singleSurface: input.singleSurface,
          ports: { ...input.ports, reveal, projectChanged, closeHistory, notice, applyHistorySessions } },
      }, executeQueuedNavigation);
    } finally { settle(navigationIntentSeq); }
  });
  const enqueueNavigationWithIntent = useCommittedCommand(async (intent: DesktopNavigationIntent, seq: number): Promise<void> => { await executeWithIntent(intent, seq); });
  const enqueueNavigation = useCommittedCommand((intent: DesktopNavigationIntent) => {
    input.showChat();
    return enqueueNavigationWithIntent(intent, input.noteIntent());
  });
  const openRemoteProject: RemoteNavigationCommand = useCommittedAsyncCommand(
    (...[remote, options]: Parameters<RemoteNavigationCommand>) => ({
      intent: { kind: "remote-project", remote: { ...remote }, options: { ...options } } as DesktopNavigationIntent,
      showChat: input.showChat, noteIntent: input.noteIntent, execute: executeWithIntent,
    }), startRemoteNavigation);
  useLayoutEffect(() => () => {
    queue.seqRef.current++;
    queue.pendingRef.current?.resolve();
    queue.pendingRef.current = null;
  }, [queue]);
  return { enqueueNavigation, enqueueNavigationWithIntent, openRemoteProject };
}
