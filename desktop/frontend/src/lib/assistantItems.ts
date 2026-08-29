import type { Item, LiveStream, State } from "./useController";

type AssistantItem = Extract<Item, { kind: "assistant" }>;

export function assistantHasContent(item: AssistantItem | undefined, live?: LiveStream): boolean {
  return Boolean(
    `${live?.text ?? ""}${live?.reasoning ?? ""}${item?.text ?? ""}${item?.reasoning ?? ""}`.trim()
    || item?.memoryCitations?.length
    || item?.searchSources?.length
  );
}

export function removeEmptyAssistantItems(items: Item[]): Item[] {
  return items.filter((item) => item.kind !== "assistant" || assistantHasContent(item));
}

/** Allocate one provider sampling segment without changing the backend turn identity. */
export function ensureAssistant(s: State): State {
  if (s.currentAssistant && s.items.some((item) => item.kind === "assistant" && item.id === s.currentAssistant)) return s;
  const ordinal = s.assistantSegmentOrdinal;
  const id = s.activeTurnId ? `a:${s.activeTurnId}:${ordinal}` : `a${s.seq}`;
  const item: AssistantItem = { kind: "assistant", id, text: "", reasoning: "", streaming: true, wasStreamed: true, searchSources: s.pendingSearchSources?.length ? s.pendingSearchSources : undefined };
  return {
    ...s,
    items: [...s.items, item],
    currentAssistant: id,
    pendingSearchSources: undefined,
    seq: s.seq + 1,
    assistantSegmentOrdinal: ordinal + 1,
  };
}

export function ensureActiveAssistant(s: State): State {
  const active = ensureAssistant(s);
  const id = active.currentAssistant!;
  return active.live?.id === id ? active : { ...active, live: { id, text: "", reasoning: "", reasoningComplete: false } };
}
