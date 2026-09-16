// ── Windowed history paging (desktop/history_slice.go) ──────────────────────
// HistorySliceForTab pages toward older history with an opaque cursor; the
// first call uses cursor "" for the newest page. Entry IDs are stable for the
// life of a session revision (s<file>:r<epoch>:m<msgIndex>:o<subOrder>).
import type { HistoryMessage } from "./types";

export interface HistorySliceRequest {
  cursor: string; // "" = newest page; pass nextCursor to page older
  turns?: number;
  entries?: number;
  bytes?: number;
  /** Page toward newer history from `cursor` instead of older (window only). */
  newer?: boolean;
}

// HistoryContentRef marks a string field replaced inline by a ≤4KiB preview;
// the full value is fetchable in chunks via HistoryContentForTab.
export interface HistoryContentRef {
  transcriptRef?: import("./transcriptProtocol").TranscriptContentRef;
  entryId: string;
  field: string; // content|reasoning|submitText|detail|code|summary|archive|toolResultError|toolArguments|toolSubject|toolSummary|toolDiff
  size: number;
  chunks: number;
  toolCallId?: string;
  revision: number;
  revKnown?: boolean;
  digest: string;
  /** Canonical v4 content identity. Present on the unified locator protocol. */
  canonicalRef?: { digest: string; bytes: number; mediaType?: string; name?: string; indexDigest?: string; integrityBlockBytes?: number };
}

export interface HistoryEntry {
  entryId: string;
  turn: number; // 1-based visible turn (0 = before the first turn)
  order: number; // absolute provider-message index
  message: HistoryMessage;
  refs: HistoryContentRef[];
}

export interface SessionClearResult {
  sessionPath: string;
  sessionId?: string;
  session?: { hostId: string; sessionId: string } | null;
  sessionRevision?: number;
  sessionDigest?: string;
  sessionGeneration: number;
}

export interface HistorySlice {
  entries: HistoryEntry[];
  nextCursor: string; // toward older; empty when none
  hasOlder: boolean;
  /**
   * history-window-v1 only. Protocol 7 has no newer cursor: an old service
   * keeps the bounded newest page and its forward paging rather than being
   * asked to simulate a bidirectional window through full downloads.
   */
  newerCursor?: string;
  hasNewer?: boolean;
  totalTurns: number;
  startTurn: number;
  endTurn: number;
  stale: boolean; // cursor bound to an older session revision: discard + reload
  revision: number;
  revisionKnown?: boolean;
  digest?: string;
  // Diagnostic read path: index|scan|event-log|live-index|live-fallback.
  source?: string;
  error?: string; // failed read; empty entries alone are not an error
}

// ── history-window-v1 (Go session.ReadHistoryWindow) ────────────────────────
// One bounded page located around an anchor rather than walked from the newest
// position, plus the cursors that keep reading in both directions. Cursors pin
// a fixed snapshot: appends keep them valid, a storage replacement or
// projection rebuild answers stale_cursor.
export interface HistoryWindowRequestView {
  snapshotSequence?: number;
  generation?: string;
  anchor: "newest" | "message" | "turn" | "cursor";
  messageId?: string;
  turn?: number;
  cursor?: string;
  direction?: "older" | "newer";
  limit?: number;
}

/**
 * "unsupported" is the peer answering that it never negotiated
 * history-window-v1: the reader keeps its protocol-7 pages and the surface
 * offers the upgrade hint, rather than the tab losing its history.
 */
export type HistoryWindowStatus = "preparing" | "ready" | "failed" | "stale_cursor" | "not_found" | "unsupported";

export interface HistoryWindowPageView {
  entries: HistoryEntry[];
  status: HistoryWindowStatus;
  /** Cursor fetching the page immediately older than this one ("" when none). */
  olderCursor: string;
  /** Cursor fetching the page immediately newer than this one ("" when none). */
  newerCursor: string;
  hasOlder: boolean;
  hasNewer: boolean;
  totalTurns: number;
  startTurn: number;
  endTurn: number;
  revision: number;
  revisionKnown: boolean;
  digest: string;
}

/** history-window-v1 per-field body read: one bounded, aligned fragment. */
export interface MessageFieldView {
  status: "ready" | "not_found" | "preparing";
  messageId: string;
  version: number;
  field: string;
  /** Length of the field's JSON source, not of the decoded value. */
  totalBytes: number;
  offset: number;
  /** Body fragment; empty on a finished or absent field. */
  data: string;
  /** Next range start; 0 means the field is fully read. */
  nextOffset: number;
  encoding: string;
}

export interface HistoryContentChunk {
  entryId: string;
  field: string;
  chunk: number;
  chunks: number;
  data: string;
  done: boolean;
  stale: boolean;
}
