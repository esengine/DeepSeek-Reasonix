// Decision logic for the floating launcher card (DockLauncher) and its
// top-right toggle. The card is on screen while the transcript surface is wide
// enough for it to occupy space and the user has not dismissed it; it is
// independent of the dock panel's own open state — the panel has its own
// toggle, and this one only ever shows or hides the card. Keeping this as a
// pure function lets the toggle's pressed state and the card's render
// condition share one source of truth that is unit-tested directly (see
// src/__tests__/launcher-card-state.test.ts).
export type SpaceMode = "full" | "hidden";

export interface LauncherCardInput {
  /** Space-yield mode reported by the card itself via onSpaceModeChange. */
  spaceMode: SpaceMode;
  /** True when the user dismissed the card with the launcher toggle. */
  dismissed: boolean;
}

export interface LauncherCardState {
  /** The card could be on screen if not dismissed — the toggle is actionable. */
  renderable: boolean;
  /** The card is actually on screen — the toggle shows its pressed state. */
  visible: boolean;
}

export function resolveLauncherCardState({ spaceMode, dismissed }: LauncherCardInput): LauncherCardState {
  const renderable = spaceMode === "full";
  return { renderable, visible: renderable && !dismissed };
}
