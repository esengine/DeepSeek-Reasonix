import type { AppBindings } from "./bridge";
import { asArray } from "./array";
import { removeEmptyAssistantItems } from "./assistantItems";
import type { Item, State } from "./useController";

export function reduceSubmitFailure(
  state: State,
  submissionId: string,
  error: string,
  conservative: boolean,
  observedAt: number,
): State {
  if (state.pendingSubmissionId !== submissionId) return state;
  const index = state.items.findIndex((item) => item.kind === "user" && item.submissionId === submissionId);
  const items = index < 0
    ? state.items
    : state.items.map((item, itemIndex) => itemIndex === index ? { ...item, submissionId: undefined, failed: true } : item);
  const next = {
    ...state,
    pendingUser: undefined,
    pendingSubmissionId: undefined,
    deliveryRecoveryActive: false,
    cancelRequested: false,
    seq: state.seq + 1,
    items: [...removeEmptyAssistantItems(items), { kind: "notice", id: `n${state.seq}`, level: "warn", text: error } as Item],
  };
  return {
    ...next,
    running: conservative,
    turnActive: conservative,
    pendingPrompt: conservative && Boolean(state.approval || state.ask || state.mcpInteraction),
    cancellable: conservative,
    ...(conservative ? {} : {
      activeTurnId: undefined,
      currentAssistant: undefined,
      assistantSegmentOrdinal: 0,
      live: undefined,
      streamAttemptJournal: undefined,
      turnLifecycleObservedAt: observedAt,
    }),
  };
}

export function reduceManagementConfirmation(state: State, submissionId: string, observedAt: number): State {
  if (state.pendingSubmissionId !== submissionId) return state;
  return {
    ...state,
    items: state.items.filter((item) => !(item.kind === "user" && item.submissionId === submissionId)),
    pendingUser: undefined,
    pendingSubmissionId: undefined,
    running: false,
    turnActive: false,
    pendingPrompt: false,
    cancelRequested: false,
    cancellable: false,
    activeTurnId: undefined,
    currentAssistant: undefined,
    assistantSegmentOrdinal: 0,
    live: undefined,
    streamAttemptJournal: undefined,
    deliveryRecoveryActive: false,
    turnLifecycleObservedAt: observedAt,
  };
}

export async function findTabAfterSubmitFailure(
  binding: Pick<AppBindings, "ListTabs">,
  tabId: string,
  delays: readonly number[],
  clock: () => number,
) {
  for (const delay of delays) {
    if (delay) await new Promise((resolve) => setTimeout(resolve, delay));
    const snapshotAt = clock();
    try {
      const tab = asArray(await binding.ListTabs()).find((candidate) => candidate.id === tabId);
      return [tab, snapshotAt] as const;
    } catch {
      // The caller's stale-turn watchdog remains the long-tail backstop.
    }
  }
  return undefined;
}
