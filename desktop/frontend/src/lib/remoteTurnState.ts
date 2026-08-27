import { t } from "./i18n";
import type { Item, State } from "./useController";

export function withRemoteProviderUnreachable(state: State, detail: string): State {
  const notice: Item = { kind: "notice", id: "provider-unreachable", level: "warn", text: t("notice.remoteProviderUnreachable", { detail }) };
  return { ...state, items: [...state.items.filter((item) => item.id !== notice.id), notice] };
}

export function withRemoteTurnInterrupted(state: State): State {
  if (!state.running && !state.turnActive) return state;
  const items = state.items.map((item): Item => {
    if (item.kind === "assistant" && state.live && item.id === state.live.id) {
      return { ...item, text: state.live.text, reasoning: state.live.reasoning, streaming: false };
    }
    if (item.kind === "assistant" && item.streaming) return { ...item, streaming: false };
    if (item.kind === "tool" && item.status === "running") return { ...item, status: "stopped" };
    return item;
  });
  const notice: Item = { kind: "notice", id: `n${state.seq}`, level: "warn", text: t("notice.remoteTurnInterrupted") };
  return {
    ...state,
    items: [...items, notice],
    running: false,
    turnActive: false,
    pendingPrompt: false,
    cancelRequested: false,
    cancellable: false,
    pendingUser: undefined,
    pendingSubmissionId: undefined,
    deliveryRecoveryActive: false,
    retry: undefined,
    approval: undefined,
    ask: undefined,
    live: undefined,
    currentAssistant: undefined,
    seq: state.seq + 1,
  };
}
