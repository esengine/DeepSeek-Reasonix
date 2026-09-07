import { useLayoutEffect, useRef } from "react";
import { useAppNavigationStore } from "../store/appNavigation";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { createSubscriptionScope } from "../lib/subscriptionScope";
import type { DesktopNavigationIntent } from "./desktopNavigationOwner";

async function finishAutomationNavigation(input: {
  intent: number; request: DesktopNavigationIntent;
  enqueue(request: DesktopNavigationIntent, intent: number): Promise<void>;
  finish(intent: number): void;
}) {
  try { await input.enqueue(input.request, input.intent); }
  finally { input.finish(input.intent); }
}

/** The management page owns its link until the navigation owner accepts it. */
export function useAutomationNavigation(input: {
  noteIntent(): number;
  enqueue(intent: DesktopNavigationIntent, seq: number): Promise<void>;
}) {
  const pending = useRef<{ intent: number; generation: number } | null>(null);
  const invalidate = useCommittedCommand(() => {
    if (!pending.current) return;
    pending.current = null;
    input.noteIntent();
  });
  useLayoutEffect(() => {
    const scope = createSubscriptionScope();
    scope.listen(listener => useAppNavigationStore.subscribe((next, previous) => {
      if (next.generation !== previous.generation) listener();
    }), invalidate);
    return () => { pending.current = null; scope.dispose(); };
  }, [invalidate]);
  const finish = useCommittedCommand((intent: number) => {
    if (pending.current?.intent === intent) pending.current = null;
  });
  const openAutomationTopic = useCommittedCommand((scope: string, workspaceRoot: string, topicId: string) => {
    const intent = input.noteIntent();
    pending.current = { intent, generation: useAppNavigationStore.getState().generation };
    return finishAutomationNavigation({ intent, request: { kind: "topic", scope, workspaceRoot, topicId }, enqueue: input.enqueue, finish });
  });
  const topicAccepted = useCommittedCommand((intent: number) => {
    const link = pending.current;
    if (!link || link.intent !== intent) return;
    pending.current = null;
    useAppNavigationStore.getState().returnFromAutomationLink(link.generation);
  });
  return { openAutomationTopic, topicAccepted };
}
