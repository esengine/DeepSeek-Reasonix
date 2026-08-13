/** Live-turn markers that a lagging history snapshot must not replace. */
export type HydrateLiveState = {
  running?: boolean;
  turnActive?: boolean;
  live?: unknown;
  currentAssistant?: unknown;
  pendingUser?: unknown;
  historyTotalTurns?: number;
  items: ReadonlyArray<{ kind: string; streaming?: boolean; status?: string }>;
};

export type HydratedHistoryApplyMode = "replace" | "prepend" | "skip";

// A live turn is only "cached" once a history page has landed behind it.
// Without that, a session opened mid-stream reports a cached turn, skips the
// fetch, and streams over a blank transcript.
export function hasCachedLiveTurn(state: HydrateLiveState | undefined): boolean {
  if (!state?.running && !state?.turnActive) return false;
  if ((state.historyTotalTurns ?? 0) === 0) return false;
  if (state.live || state.currentAssistant || state.pendingUser !== undefined) return true;
  return state.items.some((item) =>
    (item.kind === "assistant" && item.streaming) ||
    (item.kind === "tool" && item.status === "running"),
  );
}

// An empty surface has to apply history or switch-back shows Welcome. A turn
// that has already streamed rows keeps them — but a tab with no history page
// behind it still gets one, prepended, instead of a blank transcript above the
// live turn. Only an already-hydrated live turn is left alone.
export function hydratedHistoryApplyMode(
  skipHistory: boolean,
  hasProjection: boolean,
  foregroundTurnActive: boolean,
  state: HydrateLiveState | undefined,
): HydratedHistoryApplyMode {
  if (skipHistory || !hasProjection) return "skip";
  if (!foregroundTurnActive) return "replace";
  if ((state?.items.length ?? 0) === 0 && !hasCachedLiveTurn(state)) return "replace";
  return (state?.historyTotalTurns ?? 0) === 0 ? "prepend" : "skip";
}

type SignatureItem = {
  kind: string;
  id: string;
  text?: string;
  reasoning?: string;
  name?: string;
  level?: string;
  trigger?: string;
  messages?: number;
  surfaceKey?: string;
  generation?: number;
};

function itemSignature(item: SignatureItem): string {
  switch (item.kind) {
    case "tool": return `tool|${item.id}|${item.name ?? ""}`;
    case "extension": return `extension|${item.surfaceKey ?? ""}|${item.generation ?? 0}`;
    case "compaction": return `compaction|${item.trigger ?? ""}|${item.messages ?? 0}`;
    default: return `${item.kind}|${item.level ?? ""}|${item.text ?? ""}|${item.reasoning ?? ""}`;
  }
}

// A page read while its turn is live can already carry rows the live stream
// rendered. Only a suffix of the page can overlap a prefix of the live rows, so
// the longest such match is the duplicate set.
export function duplicateLiveItemIds(
  pageItems: readonly SignatureItem[],
  liveItems: readonly SignatureItem[],
): string[] {
  for (let k = Math.min(pageItems.length, liveItems.length); k > 0; k -= 1) {
    let same = true;
    for (let i = 0; i < k && same; i += 1) {
      same = itemSignature(pageItems[pageItems.length - k + i]) === itemSignature(liveItems[i]);
    }
    if (same) return liveItems.slice(0, k).map((item) => item.id);
  }
  return [];
}

export function sameSessionPlaceholderItems<T>(
  targetSessionPath: string | undefined,
  prev: { meta?: { sessionPath?: string }; items?: T[] } | undefined,
): T[] | undefined {
  const target = (targetSessionPath ?? "").trim();
  const current = (prev?.meta?.sessionPath ?? "").trim();
  if (!target || !current || target !== current) return undefined;
  return prev?.items;
}
