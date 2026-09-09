// Read progress is host-owned, keyed by read id, and upserted: a hundred pages
// of one logical read still render one status line, never a hundred notices.

/** WireReadStatus is one read's delivery state; zero-based half-open ranges, no text. */
export interface WireReadStatus {
  readId: string;
  generation?: number;
  seq?: number;
  path: string;
  intent?: string;
  state: string;
  covered?: [number, number][];
  missing?: [number, number][];
  sourceEnd?: number;
  hasMore?: boolean;
  reason?: string;
  recovery?: string;
  active?: boolean;
}

export type ReadStatusHost = { readStatuses?: Record<string, WireReadStatus>; readStatusClosed?: boolean };

export function applyReadStatusEvent<T extends ReadStatusHost & { activeTurnId?: string; turnActive: boolean }>(state: T, event: { turnId?: string; readStatus?: WireReadStatus }): T {
  if (state.readStatusClosed || (state.activeTurnId && !state.turnActive)) return state;
  if (event.turnId && state.activeTurnId && event.turnId !== state.activeTurnId) return state;
  return applyReadStatusFrame(state, event.readStatus);
}

/**
 * applyReadStatusFrame upserts one frame. A re-ordered frame never moves a read
 * backwards, and an unnamed frame is ignored.
 */
export function applyReadStatusFrame<T extends ReadStatusHost>(state: T, incoming: WireReadStatus | undefined): T {
  if (!incoming?.readId) return state;
  const previous = state.readStatuses?.[incoming.readId];
  if (previous) {
    const oldGeneration = previous.generation ?? 0;
    const generation = incoming.generation ?? 0;
    if (generation < oldGeneration) return state;
    if (generation === oldGeneration && (incoming.seq ?? 0) <= (previous.seq ?? 0)) return state;
  }
  return { ...state, readStatuses: { ...(state.readStatuses ?? {}), [incoming.readId]: incoming } };
}

/** ReadStatusKey is the closed set of labels one read status can produce. */
export type ReadStatusKey =
  | "composer.readStatusReading"
  | "composer.readStatusCovered"
  | "composer.readStatusDone"
  | "composer.readStatusPaused"
  | "composer.readStatusBudget"
  | "composer.readStatusSource"
  | "composer.readStatusStalled"
  | "composer.readStatusRecovery";

/** readStatusLabel renders every active read as one short status line. */
export function readStatusLabel(
  statuses: Record<string, WireReadStatus> | undefined,
  t: (key: ReadStatusKey, vars?: Record<string, string | number>) => string,
): string {
  const active = Object.values(statuses ?? {}).filter((status) => status.active);
  return active.map((status) => readStatusItemLabel(status, t)).join(" · ");
}

function readStatusItemLabel(first: WireReadStatus, t: (key: ReadStatusKey, vars?: Record<string, string | number>) => string): string {
  const file = first.path.split(/[\\/]/).pop() || first.path;
  const covered = first.covered?.length
    ? first.covered.map(([start, end]) => `${start + 1}–${end}`).join(", ")
    : "";
  if (first.state === "blocked" || first.state === "needs_scope") {
    const reason = first.reason === "no_progress" ? "composer.readStatusStalled"
      : ["page_budget", "time_budget", "no_headroom", "unknown_window"].includes(first.reason ?? "") ? "composer.readStatusBudget"
      : "composer.readStatusSource";
    return [t("composer.readStatusPaused", { file }), t(reason), t("composer.readStatusRecovery")].join(" · ");
  }
  if (first.hasMore) {
    return covered ? t("composer.readStatusCovered", { file, range: covered }) : t("composer.readStatusReading", { file });
  }
  return covered ? t("composer.readStatusDone", { file, range: covered }) : t("composer.readStatusReading", { file });
}

/** TurnPhaseKey is the closed set of phase labels the composer can show. */
export type TurnPhaseKey =
  | "composer.turnPhaseChecking"
  | "composer.turnPhaseVerifying"
  | "composer.turnPhaseReviewing"
  | "composer.turnPhaseWorking"
  | "composer.runAnnounceRunning";

/** turnPhaseStatusLabel renders the host turn phase for the status line. */
export function turnPhaseStatusLabel(
  turnPhase: string | undefined,
  t: (key: TurnPhaseKey, vars?: Record<string, string | number>) => string,
): string {
  switch ((turnPhase ?? "").trim()) {
    case "checking":
      return t("composer.turnPhaseChecking");
    case "verifying":
      return t("composer.turnPhaseVerifying");
    case "reviewing":
      return t("composer.turnPhaseReviewing");
    case "working":
      return t("composer.turnPhaseWorking");
    default:
      return t("composer.runAnnounceRunning");
  }
}
