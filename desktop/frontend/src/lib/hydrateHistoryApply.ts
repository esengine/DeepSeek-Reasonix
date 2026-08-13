/** Live-turn markers that a lagging history snapshot must not replace. */
export type HydrateLiveState = {
  running?: boolean;
  turnActive?: boolean;
  live?: unknown;
  currentAssistant?: unknown;
  pendingUser?: unknown;
  items: ReadonlyArray<{ kind: string; streaming?: boolean; status?: string }>;
};

export function hasCachedLiveTurn(state: HydrateLiveState | undefined): boolean {
  if (!state?.running && !state?.turnActive) return false;
  if (state.live || state.currentAssistant || state.pendingUser !== undefined) return true;
  return state.items.some((item) =>
    (item.kind === "assistant" && item.streaming) ||
    (item.kind === "tool" && item.status === "running"),
  );
}

// Skip replace when a live transcript is already on screen; an empty
// surface still has to apply history or switch-back shows Welcome.
export function shouldApplyHydratedHistory(
  skipHistory: boolean,
  hasProjection: boolean,
  foregroundTurnActive: boolean,
  state: HydrateLiveState | undefined,
): boolean {
  if (skipHistory || !hasProjection) return false;
  if (!foregroundTurnActive) return true;
  return (state?.items.length ?? 0) === 0 && !hasCachedLiveTurn(state);
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
