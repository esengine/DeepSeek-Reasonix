import type { HistoryPreparationWait } from "./historyPreparation";
import type { Item } from "./useController";
import type { TranscriptRecord } from "./transcriptRecordProjection";
import type { TranscriptWindowPage } from "./transcriptLiveWindow";
import type { HistoryContentChunk, HistoryContentRef, HistorySlice, HistorySliceRequest } from "./types";

export interface TranscriptBackend {
  HistorySliceForTab(tabID: string, req: HistorySliceRequest): Promise<HistorySlice>;
  HistoryContentForTab(tabID: string, ref: HistoryContentRef, chunkIndex: number): Promise<HistoryContentChunk>;
}

export interface TranscriptStoreOptions {
  /** Injectable preparation scheduler for deterministic lifecycle tests. */
  preparationWait?: HistoryPreparationWait;
  /** Resident sessions with records (unpinned). Default 3. */
  maxResidentSessions?: number;
  /** Total inline history body bytes across resident sessions. Default 32MiB. */
  historyBodyBudgetBytes?: number;
  /** Parsed-markdown cache budget. Default 16MiB. */
  markdownBudgetBytes?: number;
  /** Adjacent history pages retained per session, newest-side included. */
  windowMaxPages?: number;
  /** Entries grouped into one live-tail page before it becomes reclaimable. */
  windowPageEntries?: number;
}

export type HistoryReadOptions = { turns?: number; entries?: number; bytes?: number; current?: () => boolean };

export interface TranscriptProjection {
  items: Item[];
  startTurn: number;
  endTurn: number;
  totalTurns: number;
  hasOlder: boolean;
  /** More history exists past the newer edge of the resident window. */
  hasNewer: boolean;
  revision: number;
  revisionKnown: boolean;
  digest: string;
}

export interface PreparedTranscriptInstall {
  projection: TranscriptProjection;
  commit(): void;
}

export interface LoadOlderResult extends TranscriptProjection {
  /** "prepend": page older items; "reload": cursor went stale, full latest replace. */
  kind: "prepend" | "reload";
  /** Items contributed by the older page (kind === "prepend"). */
  prependItems: Item[];
  /**
   * Ids the caller must drop: items superseded by cross-page tool merges, plus
   * every item on a page reclaimed to keep the window at its page budget.
   */
  removeIds: string[];
}

export interface LoadNewerResult extends TranscriptProjection {
  /** "append": page newer items; "stale": the window predates a rebuild. */
  kind: "append" | "stale";
  /** Items contributed by the newer page (kind === "append"). */
  appendItems: Item[];
  /** Ids reclaimed from the older edge to keep the window bounded. */
  removeIds: string[];
}

export interface AppendEntriesResult extends TranscriptProjection {
  /** Ids reclaimed from the caller's mounted projection. */
  removeIds: string[];
}

export interface TranscriptContentChange {
  tabId: string;
  /** Re-converted items keyed by their stable item id. */
  patches: Record<string, Item>;
  expected?: Record<string, Item>;
}

export interface SessionTranscript {
  bindingKey?: string;
  canonicalV2?: boolean;
  latestSequence?: number;
  key: string;
  tabId: string;
  sessionPath: string;
  records: TranscriptRecord[];
  byId: Map<string, TranscriptRecord>;
  /** toolCallId -> result record entryId (first record wins, like resultByID). */
  toolResultOwners: Map<string, string>;
  /** entryId -> projected items of that record ([] when consumed). */
  contributions: Map<string, Item[]>;
  /** Result record entryIds folded into a call's tool item. */
  consumed: Set<string>;
  /** result entryId -> claimer (assistant) entryId. */
  consumedBy: Map<string, string>;
  /** toolCallId -> assistant record entryId whose call still lacks a result. */
  unresolvedCalls: Map<string, string>;
  /** assistant entryId -> unmatched positional call indexes. */
  pendingPositional: Map<string, number[]>;
  /** assistant entryId -> callIndex -> result entryId (for re-conversion). */
  matchTables: Map<string, Map<number, string>>;
  itemsCache: Item[] | null;
  nextCursor: string;
  hasOlder: boolean;
  /** Resident window pages, oldest first. Empty until a page is loaded. */
  pages: TranscriptWindowPage[];
  /** Cursor fetching the page immediately newer than the resident window. */
  newerCursor: string;
  hasNewer: boolean;
  /** Pages reclaimed from each end; diagnostics only. */
  reclaimedOlder: number;
  reclaimedNewer: number;
  totalTurns: number;
  startTurn: number;
  endTurn: number;
  revision: number;
  revisionKnown: boolean;
  digest: string;
  generation: number;
  /** Settles when the current fresh-page generation has installed or failed. */
  generationSettlement?: { generation: number; promise: Promise<void> };
  bodyBytes: number;
  olderInFlight: boolean;
  newerInFlight: boolean;
  pendingContent: Map<string, { generation: number; promise: Promise<string | undefined> }>;
}
