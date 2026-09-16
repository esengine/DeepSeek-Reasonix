import { addBreadcrumb } from "./breadcrumbs";
import { historyPageRequestBudget } from "./historyPaging";
import { getTranscriptStore, type TranscriptProjection } from "./transcriptStore";
import { hydrateIdentityCurrent, type SessionIdentity } from "./sessionIdentity";

export type HistoryWindowLoadOutcome = "loaded" | "empty" | "stale";
export type HistoryWindowDirection = "older" | "newer" | "latest";

export type HistoryWindowState = {
  transcriptProtocol?: number;
  historyStartTurn: number;
  historyTotalTurns: number;
  historyHasOlder: boolean;
  historyHasNewer: boolean;
  historyOlderLoading: boolean;
  historyNewerLoading: boolean;
  historyRevision?: number;
  historyDigest?: string;
  running: boolean;
  meta?: SessionIdentity & { sessionRevision?: number; sessionDigest?: string };
};

export type HistoryWindowAction =
  | { type: "history_older_start" }
  | { type: "history_older_error"; error?: string }
  | { type: "history_newer_start" }
  | { type: "history_newer_error"; error?: string }
  | ({ type: "history_replace"; items: TranscriptProjection["items"] } & HistoryWindowProjection)
  | ({ type: "history_prepend"; items: TranscriptProjection["items"]; removeIds: string[] } & HistoryWindowProjection)
  | ({ type: "history_append"; items: TranscriptProjection["items"] } & HistoryWindowProjection);

type HistoryWindowProjection = {
  startTurn: number;
  endTurn: number;
  totalTurns: number;
  hasOlder: boolean;
  hasNewer: boolean;
  revision?: number;
  digest?: string;
};

type LoadInput = {
  tabId: string;
  direction: HistoryWindowDirection;
  targetTurn?: number;
  trigger: string;
  state: HistoryWindowState;
  requestSeq: number;
  isCurrent: (requestSeq: number) => boolean;
  currentState: () => HistoryWindowState | undefined;
  dispatch: (action: HistoryWindowAction) => void;
};

function projectionFields(projection: TranscriptProjection): HistoryWindowProjection {
  return {
    startTurn: projection.startTurn,
    endTurn: projection.endTurn,
    totalTurns: projection.totalTurns,
    hasOlder: projection.hasOlder,
    hasNewer: projection.hasNewer,
    revision: projection.revisionKnown ? projection.revision : undefined,
    digest: projection.digest || undefined,
  };
}

function fingerprintMatches(expected: number | undefined, actual: number | undefined): boolean {
  return expected === undefined || expected <= 0 || actual === expected;
}

function digestMatches(expected: string | undefined, actual: string | undefined): boolean {
  return !expected || actual === expected;
}

/** Runs one protocol-7 window transition without owning React state or scroll. */
export async function loadHistoryWindow(input: LoadInput): Promise<HistoryWindowLoadOutcome> {
  const { tabId, direction, state } = input;
  if (state.running && state.transcriptProtocol !== 2) return "empty";
  if (direction === "older" && (!state.historyHasOlder || state.historyOlderLoading)) return "empty";
  if (direction === "newer" && (!state.historyHasNewer || state.historyNewerLoading)) return "empty";
  const sessionPath = state.meta?.sessionPath ?? "";
  const sessionIdentity = state.meta ?? {};
  const expectedRevision = state.transcriptProtocol === 2 ? undefined : state.meta?.sessionRevision ?? state.historyRevision;
  const expectedDigest = state.transcriptProtocol === 2 ? state.historyDigest : state.meta?.sessionDigest ?? state.historyDigest;
  const request = {
    ...historyPageRequestBudget(state.historyStartTurn, state.historyTotalTurns, input.targetTurn),
    current: () => input.isCurrent(input.requestSeq) && hydrateIdentityCurrent(sessionIdentity, input.currentState()?.meta),
  };
  input.dispatch({ type: direction === "older" ? "history_older_start" : "history_newer_start" });
  const startedAt = Date.now();
  try {
    const store = getTranscriptStore();
    if (direction === "older") {
      const result = await store.loadOlder(tabId, sessionPath, request);
      if (!input.isCurrent(input.requestSeq)) return "empty";
      const current = input.currentState();
      if (!current || !current.historyOlderLoading || !hydrateIdentityCurrent(sessionIdentity, current.meta) ||
        !fingerprintMatches(expectedRevision, current.meta?.sessionRevision ?? current.historyRevision) ||
        !digestMatches(expectedDigest, current.transcriptProtocol === 2 ? current.historyDigest : current.meta?.sessionDigest ?? current.historyDigest)) {
        input.dispatch({ type: "history_older_error", error: "history identity changed" });
        return "empty";
      }
      if (!result) { input.dispatch({ type: "history_older_error", error: "history page unavailable" }); return "empty"; }
      if (!fingerprintMatches(expectedRevision, result.revisionKnown ? result.revision : undefined) || !digestMatches(expectedDigest, result.digest)) {
        input.dispatch({ type: "history_older_error", error: "history identity changed" });
        return "empty";
      }
      if (result.kind === "reload") input.dispatch({ type: "history_replace", items: result.items, ...projectionFields(result) });
      else input.dispatch({ type: "history_prepend", items: result.prependItems, removeIds: result.removeIds, ...projectionFields(result) });
      addBreadcrumb("tab.hydrate", `history older ${tabId} trigger=${input.trigger} turns=${result.startTurn}-${result.endTurn}/${result.totalTurns} ms=${Date.now() - startedAt}`);
      return "loaded";
    }
    if (direction === "newer") {
      const result = await store.loadNewer(tabId, sessionPath, request);
      if (!input.isCurrent(input.requestSeq)) return "empty";
      const current = input.currentState();
      if (!current || !current.historyNewerLoading || !hydrateIdentityCurrent(sessionIdentity, current.meta) ||
        !fingerprintMatches(expectedRevision, current.meta?.sessionRevision ?? current.historyRevision) ||
        !digestMatches(expectedDigest, current.transcriptProtocol === 2 ? current.historyDigest : current.meta?.sessionDigest ?? current.historyDigest)) {
        input.dispatch({ type: "history_newer_error", error: "history identity changed" });
        return "empty";
      }
      if (!result) { input.dispatch({ type: "history_newer_error", error: "history page unavailable" }); return "empty"; }
      if (!fingerprintMatches(expectedRevision, result.revisionKnown ? result.revision : undefined) || !digestMatches(expectedDigest, result.digest)) {
        input.dispatch({ type: "history_newer_error", error: "history identity changed" });
        return "empty";
      }
      if (result.kind === "stale") {
        input.dispatch({ type: "history_newer_error", error: "history snapshot expired" });
        return "stale";
      }
      input.dispatch({ type: "history_append", items: result.items, ...projectionFields(result) });
      addBreadcrumb("tab.hydrate", `history newer ${tabId} trigger=${input.trigger} turns=${result.startTurn}-${result.endTurn}/${result.totalTurns} ms=${Date.now() - startedAt}`);
      return "loaded";
    }
    const result = await store.loadLatest(tabId, sessionPath, { ...request, preferResident: false });
    if (!input.isCurrent(input.requestSeq)) return "empty";
    const current = input.currentState();
    if (!current) return "empty";
    const currentRevision = current.meta?.sessionRevision ?? current.historyRevision;
    const currentDigest = current.transcriptProtocol === 2 ? current.historyDigest : current.meta?.sessionDigest ?? current.historyDigest;
    if (!current.historyNewerLoading || !hydrateIdentityCurrent(sessionIdentity, current.meta) ||
      !fingerprintMatches(expectedRevision, currentRevision) || !digestMatches(expectedDigest, currentDigest)) {
      input.dispatch({ type: "history_newer_error", error: "history identity changed" });
      return "empty";
    }
    if (!result) {
      input.dispatch({ type: "history_newer_error", error: "history page unavailable" });
      return "empty";
    }
    input.dispatch({ type: "history_replace", items: result.items, ...projectionFields(result) });
    addBreadcrumb("tab.hydrate", `history ${direction} ${tabId} trigger=${input.trigger} turns=${result.startTurn}-${result.endTurn}/${result.totalTurns} ms=${Date.now() - startedAt}`);
    return "loaded";
  } catch (error) {
    if (!input.isCurrent(input.requestSeq) || !input.currentState()) return "empty";
    const message = error instanceof Error ? error.message : String(error);
    input.dispatch({ type: direction === "older" ? "history_older_error" : "history_newer_error", error: message });
    addBreadcrumb("tab.hydrate", `history ${direction} failed ${tabId}: ${message}`);
    return "empty";
  }
}
