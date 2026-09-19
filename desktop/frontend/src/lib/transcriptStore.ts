import type { HistoryPreparationWait } from "./historyPreparation";
// Bounded transcript records with stable ids, lazy content, generation-aware paging, and weighted LRU eviction.
import { asArray } from "./array";
import { canonicalHistoryContent, canonicalHistorySlice } from "./canonicalTranscriptBackend";
import { fetchPreparedHistorySlice } from "./transcriptHistoryFetch";
import { prepareTranscriptInstall } from "./transcriptStoreInstall";
import { registerTranscriptCacheDiagnostics } from "./sessionDiagnostics";
import { TranscriptMarkdownCache, type ParsedMarkdownValue } from "./transcriptMarkdownCache";
export type { ParsedMarkdownValue } from "./transcriptMarkdownCache";
import type { Item, State } from "./useController";
import { resolveTranscriptEntryAlias, TranscriptContentResolverRegistry } from "./transcriptContentResolver";
import { convertRecord, entryToRecord, type RecordConversion, type TranscriptRecord } from "./transcriptRecordProjection";
import { readTranscriptContent } from "./transcriptContentRead";
import { appendLivePageEntries, type TranscriptWindowPage } from "./transcriptLiveWindow";
import { RESOURCE_BUDGETS } from "./resourceBudgets";
import { bindTranscriptSession, boundSessionKey, detachTranscriptTab, type TranscriptTabBinding } from "./transcriptSessionBinding";
import type {
  HistoryEntry,
  HistorySlice,
  HistorySliceRequest,
} from "./types";

import type { TranscriptBackend, TranscriptStoreOptions, TranscriptProjection, PreparedTranscriptInstall, LoadOlderResult, LoadNewerResult, AppendEntriesResult, TranscriptContentChange, SessionTranscript, HistoryReadOptions } from "./transcriptStoreTypes";
export type { TranscriptBackend, TranscriptStoreOptions, TranscriptProjection, PreparedTranscriptInstall, LoadOlderResult, LoadNewerResult, AppendEntriesResult, TranscriptContentChange, SessionTranscript, HistoryReadOptions } from "./transcriptStoreTypes";

const DEFAULT_MAX_RESIDENT_SESSIONS = 3;
const DEFAULT_HISTORY_BODY_BUDGET = RESOURCE_BUDGETS.historyBodyBytes;
const DEFAULT_MARKDOWN_BUDGET = 16 << 20;
const DEFAULT_WINDOW_MAX_PAGES = RESOURCE_BUDGETS.historyWindowPages;
const DEFAULT_WINDOW_PAGE_ENTRIES = RESOURCE_BUDGETS.historyPageEntries;

function sliceRevisionKnown(slice: Pick<HistorySlice, "revision" | "revisionKnown">): boolean {
  // Compatibility with the first HistorySlice contract: positive revisions
  // were already canonical, but revisionKnown was not exposed yet.
  return slice.revisionKnown ?? (slice.revision ?? 0) > 0;
}

function compareRecords(a: Pick<TranscriptRecord, "order" | "entryId">, b: Pick<TranscriptRecord, "order" | "entryId">): number {
  if (a.order !== b.order) return a.order - b.order;
  return a.entryId < b.entryId ? -1 : a.entryId > b.entryId ? 1 : 0;
}

export class TranscriptStore {
  readonly states = new Map<string, State>();
  private readonly stateListeners = new Map<string, Set<() => void>>();

  subscribeState(tabId: string, listener: () => void): () => void {
    let listeners = this.stateListeners.get(tabId);
    if (!listeners) this.stateListeners.set(tabId, listeners = new Set());
    listeners.add(listener);
    return () => { listeners.delete(listener); if (!listeners.size) this.stateListeners.delete(tabId); };
  }

  setState(tabId: string, state: State): void {
    if (this.states.get(tabId) === state) return;
    this.states.set(tabId, state);
    for (const listener of this.stateListeners.get(tabId) ?? []) listener();
  }

  /** Install the page that belongs to a Follow cut, without another read. */
  installSlice(tabId: string, sessionPath: string, slice: HistorySlice): TranscriptProjection {
    const prepared = this.prepareInstallSlice(tabId, sessionPath, slice);
    prepared.commit();
    return prepared.projection;
  }

  /** Build a complete replacement without exposing it to readers. The caller
   * commits only after the reducer has accepted the matching snapshot. */
  prepareInstallSlice(tabId: string, sessionPath: string, slice: HistorySlice): PreparedTranscriptInstall {
    const key = this.sessionKeyFor(tabId, sessionPath);
    const previous = this.sessions.get(key);
    const session = this.newSession(key, tabId, sessionPath);
    return prepareTranscriptInstall(previous, session, slice, this.windowPageEntries,
      (candidate, entries) => this.replaceRecords(candidate, entries), candidate => this.projectionOf(candidate), installed => {
        this.sessions.set(key, installed);
        this.touch(installed);
        this.enforceBudgets();
      });
  }
  private readonly contentResolvers = new TranscriptContentResolverRegistry();
  registerContentResolver(tabId: string, resolve: (entryId: string, field: string) => Promise<string | undefined>, enabled: () => boolean = () => true): () => void {
    return this.contentResolvers.register(tabId, resolve, enabled);
  }
  private readonly backend: TranscriptBackend;
  private readonly preparationWait?: HistoryPreparationWait;
  private readonly maxResidentSessions: number;
  private readonly historyBodyBudgetBytes: number;
  private readonly windowMaxPages: number;
  private readonly windowPageEntries: number;
  /** Insertion-ordered (oldest first); touch re-inserts at the end. */
  private readonly sessions = new Map<string, SessionTranscript>();
  /** Ephemeral UI tab bindings; canonical resident ownership is session-based. */
  private readonly tabBindings = new Map<string, TranscriptTabBinding>();
  private readonly tabPins = new Map<string, { live: boolean; active: boolean }>();
  private readonly listeners = new Map<string, Set<(change: TranscriptContentChange) => void>>();
  private readonly markdown: TranscriptMarkdownCache;
  private historyEvictions = 0;

  constructor(backend: TranscriptBackend, options: TranscriptStoreOptions = {}) {
    this.backend = backend;
    this.preparationWait = options.preparationWait;
    this.maxResidentSessions = Math.max(1, options.maxResidentSessions ?? DEFAULT_MAX_RESIDENT_SESSIONS);
    this.historyBodyBudgetBytes = Math.max(0, options.historyBodyBudgetBytes ?? DEFAULT_HISTORY_BODY_BUDGET);
    this.windowMaxPages = Math.max(1, options.windowMaxPages ?? DEFAULT_WINDOW_MAX_PAGES);
    this.windowPageEntries = Math.max(1, options.windowPageEntries ?? DEFAULT_WINDOW_PAGE_ENTRIES);
    this.markdown = new TranscriptMarkdownCache(Math.max(0, options.markdownBudgetBytes ?? DEFAULT_MARKDOWN_BUDGET));
  }

  // ── session identity / LRU ────────────────────────────────────────────────

  private sessionKeyFor(tabId: string, sessionPath: string): string {
    return boundSessionKey(this.tabBindings, tabId, sessionPath);
  }

  /** Atomically bind an ephemeral tab to a stable canonical session owner. */
  noteSessionBinding(tabId: string, sessionPath: string, bindingKey: string): boolean {
    return bindTranscriptSession(
      this.tabBindings, this.sessions, tabId, sessionPath, bindingKey,
      key => this.newSession(key, tabId, sessionPath),
      session => this.evictSession(session),
      session => this.touch(session),
    );
  }

  private newSession(key: string, tabId: string, sessionPath: string): SessionTranscript {
    return {
      key,
      tabId,
      sessionPath,
      records: [],
      byId: new Map(),
      toolResultOwners: new Map(),
      contributions: new Map(),
      consumed: new Set(),
      consumedBy: new Map(),
      unresolvedCalls: new Map(),
      pendingPositional: new Map(),
      matchTables: new Map(),
      itemsCache: null,
      nextCursor: "",
      hasOlder: false,
      pages: [],
      newerCursor: "",
      hasNewer: false,
      reclaimedOlder: 0,
      reclaimedNewer: 0,
      totalTurns: 0,
      startTurn: 0,
      endTurn: 0,
      revision: 0,
      revisionKnown: false,
      digest: "",
      generation: 0,
      bodyBytes: 0,
      olderInFlight: false,
      newerInFlight: false,
      pendingContent: new Map(),
    };
  }

  private touch(session: SessionTranscript): void {
    this.sessions.delete(session.key);
    this.sessions.set(session.key, session);
  }

  private isPinned(session: SessionTranscript): boolean {
    return this.tabIsPinned(session.tabId);
  }

  tabIsPinned(tabId: string): boolean {
    const pins = this.tabPins.get(tabId);
    return Boolean(pins?.live || pins?.active);
  }

  /** Pin/unpin a tab with live or in-flight turn state out of the LRU. */
  setPinned(tabId: string, pinned: boolean): void {
    const pins = this.tabPins.get(tabId) ?? { live: false, active: false };
    if (pins.live === pinned) return;
    this.tabPins.set(tabId, { ...pins, live: pinned });
  }

  /**
   * The visible tab changed: pin the new active tab out of eviction and drop
   * the previous tab's active pin. Generations are NOT bumped here — a
   * background tab's in-flight load still completes into its own state (and
   * the store); generations move on session switch (fresh loadLatest), evict,
   * and unload only.
   */
  noteActiveTab(tabId: string | undefined, previousTabId?: string): void {
    if (previousTabId && previousTabId !== tabId) {
      const pins = this.tabPins.get(previousTabId) ?? { live: false, active: false };
      if (pins.active) this.tabPins.set(previousTabId, { ...pins, active: false });
    }
    if (tabId) {
      const pins = this.tabPins.get(tabId) ?? { live: false, active: false };
      if (!pins.active) this.tabPins.set(tabId, { ...pins, active: true });
    }
  }

  /** Detach a tab. Canonical sessions remain LRU-resident across tab IDs. */
  evictTab(tabId: string): void {
    detachTranscriptTab(this.tabBindings, this.sessions, tabId, session => this.evictSession(session));
    this.tabPins.delete(tabId);
  }

  private evictSession(session: SessionTranscript): void {
    session.generation += 1; // in-flight responses discard against a missing/stale session
    this.sessions.delete(session.key);
    this.historyEvictions += 1;
  }

  private enforceBudgets(): void {
    // The page budget applies to every session, pinned ones included: a live
    // session keeps its tail streaming but no longer holds its whole history.
    for (const session of this.sessions.values()) {
      if (session.pages.length > this.windowMaxPages) this.trimWindow(session, "newer");
    }
    const evictable = (): SessionTranscript[] =>
      Array.from(this.sessions.values()).filter((s) => s.records.length > 0 && !this.isPinned(s));
    let candidates = evictable();
    let resident = candidates.length;
    while (resident > this.maxResidentSessions && candidates.length > 0) {
      const victim = candidates.shift();
      if (!victim) break;
      this.evictSession(victim);
      resident -= 1;
    }
    let total = 0;
    for (const session of this.sessions.values()) total += session.bodyBytes;
    candidates = evictable();
    while (total > this.historyBodyBudgetBytes && candidates.length > 0) {
      const victim = candidates.shift();
      if (!victim) break;
      total -= victim.bodyBytes;
      this.evictSession(victim);
    }
  }

  // ── projection ────────────────────────────────────────────────────────────

  private rebuildProjection(session: SessionTranscript): Item[] {
    const items: Item[] = [];
    for (const rec of session.records) {
      const contribution = session.contributions.get(rec.entryId);
      if (contribution) items.push(...contribution);
    }
    session.itemsCache = items;
    return items;
  }

  exportObservation(tabId: string, sessionPath: string) {
    const session = this.sessions.get(this.sessionKeyFor(tabId, sessionPath));
    return session ? { capturedAt: new Date().toISOString(), tabId, sessionPath, generation: session.generation, appliedSequence: session.revision, hasOlder: session.hasOlder, hasNewer: session.hasNewer, residentRecords: session.records.length } : { capturedAt: new Date().toISOString(), tabId, sessionPath, unavailable: "No resident transcript binding" };
  }

  private projectionOf(session: SessionTranscript): TranscriptProjection {
    return {
      items: session.itemsCache ?? this.rebuildProjection(session),
      startTurn: session.startTurn,
      endTurn: session.endTurn,
      totalTurns: session.totalTurns,
      hasOlder: session.hasOlder,
      hasNewer: session.hasNewer,
      revision: session.revision,
      revisionKnown: session.revisionKnown,
      digest: session.digest,
    };
  }

  /** Synchronous projection for an LRU-resident session; undefined on a miss
   *  or when the caller's authoritative fingerprint cannot prove the resident
   *  cut belongs to the selected session generation. */
  peek(
    tabId: string,
    sessionPath: string,
    expected?: { revision?: number; digest?: string },
  ): TranscriptProjection | undefined {
    const session = this.sessions.get(this.sessionKeyFor(tabId, sessionPath));
    if (!session || session.records.length === 0
      || (expected !== undefined && !this.matchesExpectedFingerprint(session, expected.revision, expected.digest))) return undefined;
    this.touch(session);
    return this.projectionOf(session);
  }

  /** Test/diagnostic introspection. */
  isResident(tabId: string, sessionPath: string): boolean {
    const session = this.sessions.get(this.sessionKeyFor(tabId, sessionPath));
    return Boolean(session && session.records.length > 0);
  }

  residentSessionCount(): number {
    let count = 0;
    for (const session of this.sessions.values()) if (session.records.length > 0) count += 1;
    return count;
  }

  totalBodyBytes(): number {
    let total = 0;
    for (const session of this.sessions.values()) total += session.bodyBytes;
    return total;
  }

  private reclaimedPages(): number {
    let total = 0;
    for (const session of this.sessions.values()) total += session.reclaimedOlder + session.reclaimedNewer;
    return total;
  }

  /** Messages held across every resident window; the bounded reading cost. */
  residentWindowEntries(): number {
    let total = 0;
    for (const session of this.sessions.values()) total += session.records.length;
    return total;
  }

  /** Cache-weight snapshot for diagnostics (sessionDiagnostics/crash context). */
  stats() {
    return {
      residentSessions: this.residentSessionCount(),
      maxResidentSessions: this.maxResidentSessions,
      bodyBytes: this.totalBodyBytes(),
      bodyBudgetBytes: this.historyBodyBudgetBytes,
      markdownBytes: this.markdown.bytes,
      markdownBudgetBytes: this.markdown.budgetBytes,
      historyEvictions: this.historyEvictions,
      markdownEvictions: this.markdown.evictions,
      windowMaxPages: this.windowMaxPages,
      reclaimedPages: this.reclaimedPages(),
      residentWindowEntries: this.residentWindowEntries(),
    };
  }

  // fetchSlice times one backend page request and records the content-free
  // page stats (entries, inline bytes, duration, stale, read-path source).
  private async fetchSlice(tabId: string, req: HistorySliceRequest, current: () => boolean): Promise<HistorySlice | undefined> {
    return fetchPreparedHistorySlice(() => this.backend.HistorySliceForTab(tabId, req), current, this.preparationWait);
  }

  generationOf(tabId: string, sessionPath: string): number | undefined {
    return this.sessions.get(this.sessionKeyFor(tabId, sessionPath))?.generation;
  }

  // ── record merge ops ──────────────────────────────────────────────────────

  private viewOf(records: TranscriptRecord[]): {
    records: TranscriptRecord[];
    indexOf: Map<string, number>;
    toolResultOwners: Map<string, string>;
  } {
    const indexOf = new Map<string, number>();
    const toolResultOwners = new Map<string, string>();
    records.forEach((rec, index) => {
      indexOf.set(rec.entryId, index);
      const toolCallId = rec.message.role === "tool" ? rec.message.toolCallId : undefined;
      if (toolCallId && !toolResultOwners.has(toolCallId)) toolResultOwners.set(toolCallId, rec.entryId);
    });
    return { records, indexOf, toolResultOwners };
  }

  private trackConversion(session: SessionTranscript, rec: TranscriptRecord, conversion: RecordConversion): void {
    session.contributions.set(rec.entryId, conversion.items);
    session.matchTables.set(rec.entryId, conversion.matches);
    for (const claimed of conversion.claims) session.consumedBy.set(claimed, rec.entryId);
    for (const toolCallId of conversion.unresolvedIds) session.unresolvedCalls.set(toolCallId, rec.entryId);
    if (conversion.pendingPositional.length > 0) session.pendingPositional.set(rec.entryId, conversion.pendingPositional);
    else session.pendingPositional.delete(rec.entryId);
  }

  private replaceRecords(session: SessionTranscript, entries: HistoryEntry[]): void {
    const records = entries.map(entryToRecord);
    const view = this.viewOf(records);
    const consumed = new Set<string>();
    session.records = records;
    session.byId = new Map(records.map((rec) => [rec.entryId, rec]));
    session.toolResultOwners = view.toolResultOwners;
    session.contributions = new Map();
    session.consumed = consumed;
    session.consumedBy = new Map();
    session.unresolvedCalls = new Map();
    session.pendingPositional = new Map();
    session.matchTables = new Map();
    session.bodyBytes = 0;
    for (const rec of records) {
      session.bodyBytes += rec.bytes;
      const conversion = convertRecord(rec, view, consumed);
      this.trackConversion(session, rec, conversion);
    }
    session.itemsCache = null;
    this.rebuildProjection(session);
  }

  /**
   * Prepend an older page. Returns the page's contributed items plus the ids
   * of existing standalone tool items now folded into a call from this page.
   */
  private prependRecords(session: SessionTranscript, entries: HistoryEntry[]): { items: Item[]; removeIds: string[] } {
    const fresh: TranscriptRecord[] = [];
    for (const entry of entries) {
      if (session.byId.has(entry.entryId)) continue; // contract guard: never duplicate
      fresh.push(entryToRecord(entry));
    }
    if (fresh.length === 0) return { items: [], removeIds: [] };
    const combined = [...fresh, ...session.records];
    if (session.records.length > 0 && compareRecords(fresh[fresh.length - 1], session.records[0]) > 0) {
      // Backend contract violation (pages must be contiguous prefixes): fall
      // back to one full sort rather than corrupting the order.
      combined.sort(compareRecords);
    }
    const view = this.viewOf(combined);
    const consumed = new Set(session.consumed);
    const removeIds: string[] = [];
    const prependItems: Item[] = [];
    for (const rec of fresh) {
      const before = new Set(consumed);
      const conversion = convertRecord(rec, view, consumed);
      for (const claimed of conversion.claims) {
        if (before.has(claimed)) continue;
        const existing = session.contributions.get(claimed);
        if (existing && existing.length > 0) {
          // An existing standalone tool row is now folded into this call.
          for (const item of existing) removeIds.push(item.id);
          session.contributions.set(claimed, []);
        }
      }
      this.trackConversion(session, rec, conversion);
      prependItems.push(...conversion.items);
      session.bodyBytes += rec.bytes;
    }
    session.records = combined;
    session.byId = new Map(combined.map((rec) => [rec.entryId, rec]));
    session.toolResultOwners = view.toolResultOwners;
    session.consumed = consumed;
    const removeSet = new Set(removeIds);
    const base = session.itemsCache ?? this.rebuildProjection(session);
    session.itemsCache = removeSet.size > 0 ? [...prependItems, ...base.filter((item) => !removeSet.has(item.id))] : [...prependItems, ...base];
    return { items: prependItems, removeIds };
  }

  /**
   * Append a live suffix into entry-bounded pages. Once the page window fills,
   * old records are reclaimed and their mounted item ids are returned.
   */
  appendEntries(tabId: string, sessionPath: string, entries: HistoryEntry[]): AppendEntriesResult | undefined {
    const session = this.sessions.get(this.sessionKeyFor(tabId, sessionPath));
    if (!session || session.records.length === 0) return undefined;
    const fresh = entries.filter((entry) => !session.byId.has(entry.entryId));
    this.appendRecords(session, fresh);
    appendLivePageEntries(session.pages, fresh.map((entry) => entry.entryId), this.windowPageEntries);
    if (fresh.length > 0) {
      session.hasNewer = false;
      session.newerCursor = "";
      session.totalTurns = Math.max(session.totalTurns, ...fresh.map((entry) => entry.turn));
      session.endTurn = Math.max(session.endTurn, ...fresh.map((entry) => entry.turn));
    }
    const removeIds = this.trimWindow(session, "newer");
    this.enforceBudgets();
    return this.sessions.get(session.key) === session ? { ...this.projectionOf(session), removeIds } : undefined;
  }

  upsertEntries(tabId: string, sessionPath: string, entries: HistoryEntry[], commitSeq?: number): AppendEntriesResult | undefined {
    const session = this.sessions.get(this.sessionKeyFor(tabId, sessionPath));
    if (!session) return undefined;
    session.latestSequence = Math.max(session.latestSequence ?? session.revision, commitSeq ?? 0);
    // The reader owns a contiguous window. A remote tail must not evict it or
    // create a false adjacency across an unloaded range. Accepted records stay
    // reachable through canonical pagination; active prefixes live in State.
    if (session.hasNewer) entries = entries.filter(entry => session.byId.has(entry.entryId));
    const fresh = entries.filter(entry => !session.byId.has(entry.entryId));
    const replacements = new Map(entries.map(entry => [entry.entryId, entry]));
    const combined = session.records.map(record => replacements.get(record.entryId) ?? {
      entryId: record.entryId, turn: record.turn, order: record.order, message: record.message, refs: record.refs,
    });
    combined.push(...fresh);
    this.replaceRecords(session, combined);
    appendLivePageEntries(session.pages, fresh.map(entry => entry.entryId), this.windowPageEntries);
    const removeIds = this.trimWindow(session, "newer");
    this.enforceBudgets();
    return { ...this.projectionOf(session), removeIds };
  }

  isReadingHistory(tabId: string, sessionPath: string): boolean {
    return Boolean(this.sessions.get(this.sessionKeyFor(tabId, sessionPath))?.hasNewer);
  }

  /** Append newer entries (live tail / fresh suffix). */
  private appendRecords(session: SessionTranscript, entries: HistoryEntry[]): Item[] {
    const fresh: TranscriptRecord[] = [];
    for (const entry of entries) {
      if (session.byId.has(entry.entryId)) continue;
      fresh.push(entryToRecord(entry));
    }
    if (fresh.length === 0) return [];
    const combined = [...session.records, ...fresh];
    if (session.records.length > 0 && compareRecords(session.records[session.records.length - 1], fresh[0]) > 0) {
      combined.sort(compareRecords);
    }
    const view = this.viewOf(combined);
    const consumed = new Set(session.consumed);
    const appendedItems: Item[] = [];
    let dirty = false;

    session.records = combined;
    session.byId = new Map(combined.map((rec) => [rec.entryId, rec]));
    session.toolResultOwners = view.toolResultOwners;

    // Resolve existing calls whose results only arrive now (a page cut between
    // a call and its result, or a live tail landing after the call).
    for (const rec of fresh) {
      const toolCallId = rec.message.role === "tool" ? rec.message.toolCallId : undefined;
      if (!toolCallId) continue;
      const owner = session.unresolvedCalls.get(toolCallId);
      if (!owner) continue;
      const ownerRec = session.byId.get(owner);
      if (!ownerRec) continue;
      session.unresolvedCalls.delete(toolCallId);
      const reconverted = convertRecord(ownerRec, view, consumed, session.matchTables.get(owner));
      this.trackConversion(session, ownerRec, reconverted);
      dirty = true;
    }
    for (const rec of fresh) {
      const conversion = convertRecord(rec, view, consumed);
      this.trackConversion(session, rec, conversion);
      appendedItems.push(...conversion.items);
      session.bodyBytes += rec.bytes;
    }
    session.consumed = consumed;
    if (dirty || session.itemsCache === null) {
      this.rebuildProjection(session);
    } else {
      session.itemsCache = [...session.itemsCache, ...appendedItems];
    }
    return appendedItems;
  }

  // ── bounded window ────────────────────────────────────────────────────────

  /** Replay every identity-keyed map over exactly the surviving records.
   * Reclaiming changes tool-call ownership, so the maps cannot be spliced.
   */
  private rebuildFromRecords(session: SessionTranscript, records: TranscriptRecord[]): void {
    const view = this.viewOf(records);
    session.records = records;
    session.byId = new Map(records.map((rec) => [rec.entryId, rec]));
    session.toolResultOwners = view.toolResultOwners;
    session.contributions = new Map();
    session.consumed = new Set();
    session.consumedBy = new Map();
    session.unresolvedCalls = new Map();
    session.pendingPositional = new Map();
    session.matchTables = new Map();
    session.bodyBytes = 0;
    for (const rec of records) {
      session.bodyBytes += rec.bytes;
      this.trackConversion(session, rec, convertRecord(rec, view, session.consumed));
    }
    session.itemsCache = null;
    this.rebuildProjection(session);
  }

  /** The page a freshly loaded batch of entries belongs to. */
  private pageFor(entries: HistoryEntry[], olderCursor: string, newerCursor: string): TranscriptWindowPage {
    return { entryIds: entries.map((entry) => entry.entryId), olderCursor, newerCursor };
  }

  private updateWindowTurnBounds(session: SessionTranscript): void {
    const turns = session.records.map((record) => record.turn).filter((turn) => turn > 0);
    session.startTurn = turns.length > 0 ? Math.min(...turns) : 0;
    session.endTurn = turns.length > 0 ? Math.max(...turns) : 0;
  }

  /** Reclaim one page from the given end; undefined when only one page is
   * left. Widens the page so a result is never stranded from its call.
   */
  private reclaimPage(session: SessionTranscript, end: "oldest" | "newest"): string[] | undefined {
    if (session.pages.length <= 1) return undefined;
    const page = end === "oldest" ? session.pages[0] : session.pages[session.pages.length - 1];
    const dropped = new Set(page.entryIds);
    if (end === "oldest") {
      // A result whose call is being reclaimed has to go with it, or the
      // reader is left with an output row that names a call they can no
      // longer see. Which calls survive is decided by the retained records
      // alone: collecting it from the reclaimed page would keep the calls
      // that are leaving and strand exactly the rows this guards.
      const survivingCalls = new Set<string>();
      for (const record of session.records) {
        if (dropped.has(record.entryId) || record.message.role !== "assistant") continue;
        for (const call of record.message.toolCalls ?? []) survivingCalls.add(call.id);
      }
      for (const record of session.records) {
        if (dropped.has(record.entryId)) continue;
        const callId = record.message.role === "tool" ? record.message.toolCallId : undefined;
        if (!callId || survivingCalls.has(callId)) break;
        dropped.add(record.entryId);
      }
    }
    const retained = session.records.filter((record) => !dropped.has(record.entryId));
    if (end === "oldest") {
      session.pages.shift();
      // The reclaimed page's own older cursor is now the window's head, so the
      // reader can page straight back into the range that was just dropped.
      const anchor = [...session.records].reverse().find(record => dropped.has(record.entryId) && record.message.messageId);
      session.nextCursor = session.pages[0]?.olderCursor || (anchor ? `reasonix:message:${encodeURIComponent(anchor.message.messageId!)}:${session.latestSequence ?? session.revision}:${encodeURIComponent(session.digest)}:older` : page.olderCursor);
      session.hasOlder = true;
      session.reclaimedOlder += 1;
    } else {
      session.pages.pop();
      const anchor = session.records.find(record => dropped.has(record.entryId) && record.message.messageId);
      session.newerCursor = session.pages[session.pages.length - 1]?.newerCursor || (anchor ? `reasonix:message:${encodeURIComponent(anchor.message.messageId!)}:${session.latestSequence ?? session.revision}:${encodeURIComponent(session.digest)}:newer` : page.newerCursor);
      session.hasNewer = true;
      session.reclaimedNewer += 1;
    }
    const before = new Set((session.itemsCache ?? []).map((item) => item.id));
    this.rebuildFromRecords(session, retained);
    this.updateWindowTurnBounds(session);
    // Reclaiming can also fold a retained result into a call that survived, so
    // the caller is told which ids it must drop rather than assuming the
    // difference is exactly the reclaimed page.
    const after = new Set((session.itemsCache ?? []).map((item) => item.id));
    return [...before].filter((id) => !after.has(id));
  }

  /** Reclaim from the end opposite the one being paged, returning the item
   * ids the caller must drop from its own list.
   */
  private trimWindow(session: SessionTranscript, growing: "older" | "newer"): string[] {
    if (session.pages.length === 0) return [];
    const give = growing === "older" ? "newest" : "oldest";
    const removed: string[] = [];
    while (session.pages.length > this.windowMaxPages) {
      const dropped = this.reclaimPage(session, give);
      if (dropped === undefined) break;
      removed.push(...dropped);
    }
    return removed;
  }

  // ── paging API ────────────────────────────────────────────────────────────

  /**
   * Load the newest page. preferResident serves an LRU-resident session
   * synchronously-equivalent projection without a backend round trip.
   * Returns undefined when the load was superseded/evicted mid-flight.
   */
  async loadLatest(
    tabId: string,
    sessionPath: string,
    options: HistoryReadOptions & { preferResident?: boolean; expectedRevision?: number; expectedDigest?: string } = {},
  ): Promise<TranscriptProjection | undefined> {
    const key = this.sessionKeyFor(tabId, sessionPath);
    const existing = this.sessions.get(key);
    if (options.preferResident && existing && existing.records.length > 0 &&
      this.matchesExpectedFingerprint(existing, options.expectedRevision, options.expectedDigest)) {
      this.touch(existing);
      return this.projectionOf(existing);
    }
    const session = existing ?? this.newSession(key, tabId, sessionPath);
    session.tabId = tabId;
    session.sessionPath = sessionPath;
    // A fresh load supersedes every in-flight request of the previous load.
    session.generation += 1;
    const generation = session.generation;
    let settleGeneration!: () => void;
    const generationSettled = new Promise<void>((resolve) => { settleGeneration = resolve; });
    session.generationSettlement = { generation, promise: generationSettled };
    this.sessions.set(key, session);
    this.touch(session);

    try {
      const { turns, entries, bytes } = options;
      const current = () => this.sessions.get(key) === session && session.generation === generation && (options.current?.() ?? true);
      let slice = await this.fetchSlice(tabId, { cursor: "", turns, entries, bytes }, current);
      if (!slice || !current()) return undefined;
      if (slice.stale) {
        // cursor "" cannot bind a stale identity, but a concurrent rewrite may
        // still report one — retry once against the settled revision.
        slice = await this.fetchSlice(tabId, { cursor: "", turns, entries, bytes }, current);
        if (!slice || !current()) return undefined;
      }
      const newestEntries = asArray<HistoryEntry>(slice.entries);
      this.replaceRecords(session, newestEntries);
      session.pages = [this.pageFor(newestEntries, slice.nextCursor ?? "", slice.newerCursor ?? "")];
      session.reclaimedOlder = 0;
      session.reclaimedNewer = 0;
      session.nextCursor = slice.nextCursor ?? "";
      session.hasOlder = Boolean(slice.hasOlder);
      session.newerCursor = slice.newerCursor ?? "";
      session.hasNewer = Boolean(slice.hasNewer);
      session.totalTurns = slice.totalTurns ?? 0;
      session.startTurn = slice.startTurn ?? 0;
      session.endTurn = slice.endTurn ?? 0;
      session.revision = slice.revision ?? 0;
      session.revisionKnown = sliceRevisionKnown(slice);
      session.digest = slice.digest ?? "";
      this.enforceBudgets();
      if (this.sessions.get(key) !== session) return undefined; // evicted by the budget
      return this.projectionOf(session);
    } finally {
      settleGeneration();
      if (session.generationSettlement?.generation === generation) {
        session.generationSettlement = undefined;
      }
    }
  }

  /**
   * Page toward older history. On a stale cursor the records are dropped and
   * the latest page is reloaded (kind "reload": callers replace, not prepend).
   */
  async loadOlder(
    tabId: string,
    sessionPath: string,
    options: HistoryReadOptions = {},
  ): Promise<LoadOlderResult | undefined> {
    const key = this.sessionKeyFor(tabId, sessionPath);
    const session = this.sessions.get(key);
    if (!session || session.records.length === 0) {
      // Evicted or never loaded: re-prime from the newest page; callers must
      // replace rather than prepend.
      const projection = await this.loadLatest(tabId, sessionPath, options);
      return projection ? { ...projection, kind: "reload", prependItems: [], removeIds: [] } : undefined;
    }
    if (!session.hasOlder || !session.nextCursor || session.olderInFlight) return undefined;
    session.olderInFlight = true;
    const generation = session.generation;
    const { current: _current, ...budget } = options;
    try {
      const slice = await this.fetchSlice(tabId, { cursor: session.nextCursor, ...budget }, () => this.sessions.get(key) === session && session.generation === generation && (options.current?.() ?? true));
      if (!slice || this.sessions.get(key) !== session || session.generation !== generation) return undefined;
      if (slice.stale) {
        if (session.canonicalV2) throw new Error("history snapshot expired");
        const projection = await this.loadLatest(tabId, sessionPath, options);
        return projection ? { ...projection, kind: "reload", prependItems: [], removeIds: [] } : undefined;
      }
      if (slice.source === "locator-reset") {
        this.replaceRecords(session, asArray<HistoryEntry>(slice.entries));
        session.nextCursor = slice.nextCursor ?? "";
        session.hasOlder = Boolean(slice.hasOlder);
        session.totalTurns = slice.totalTurns ?? 0;
        session.startTurn = slice.startTurn ?? 0;
        session.endTurn = slice.endTurn ?? 0;
        session.revision = slice.revision ?? 0;
        session.revisionKnown = sliceRevisionKnown(slice);
        session.digest = slice.digest ?? "";
        this.enforceBudgets();
        if (this.sessions.get(key) !== session) return undefined;
        return { ...this.projectionOf(session), kind: "reload", prependItems: [], removeIds: [] };
      }
      if (!this.sameFingerprint(session, slice)) {
        if (session.canonicalV2) throw new Error("history identity changed");
        // A backend that raced a rewrite may return a fresh page instead of a
        // stale marker. Never prepend rows from a different canonical state.
        const projection = await this.loadLatest(tabId, sessionPath, options);
        return projection ? { ...projection, kind: "reload", prependItems: [], removeIds: [] } : undefined;
      }
      const pageEntries = asArray<HistoryEntry>(slice.entries);
      const { items, removeIds } = this.prependRecords(session, pageEntries);
      session.pages.unshift(this.pageFor(pageEntries, slice.nextCursor ?? "", slice.newerCursor ?? ""));
      session.nextCursor = slice.nextCursor ?? "";
      session.hasOlder = Boolean(slice.hasOlder);
      session.totalTurns = slice.totalTurns ?? session.totalTurns;
      session.startTurn = slice.startTurn ?? session.startTurn;
      session.revision = slice.revision ?? session.revision;
      session.revisionKnown = sliceRevisionKnown(slice);
      session.digest = slice.digest ?? session.digest;
      // Reclaiming the far end yields ids the caller must drop alongside the
      // cross-page merge ids it already handles.
      const reclaimed = this.trimWindow(session, "older");
      // Settle the budget before reading the projection: a later trim would
      // leave the caller holding an item list the store has already released.
      this.enforceBudgets();
      const projection = this.projectionOf(session);
      if (this.sessions.get(key) !== session) return undefined;
      return { ...projection, kind: "prepend", prependItems: items, removeIds: reclaimed.length > 0 ? [...removeIds, ...reclaimed] : removeIds };
    } finally {
      session.olderInFlight = false;
    }
  }

  /** Page toward newer history. Needs a binding that reports a newer cursor;
   * a legacy one leaves the window on its newest page rather than faking it.
   */
  async loadNewer(
    tabId: string,
    sessionPath: string,
    options: HistoryReadOptions = {},
  ): Promise<LoadNewerResult | undefined> {
    const key = this.sessionKeyFor(tabId, sessionPath);
    const session = this.sessions.get(key);
    if (!session || session.records.length === 0) return undefined;
    if (!session.hasNewer || !session.newerCursor || session.newerInFlight) return undefined;
    session.newerInFlight = true;
    const generation = session.generation;
    const { current: _current, ...budget } = options;
    try {
      const slice = await this.fetchSlice(tabId, { cursor: session.newerCursor, newer: true, ...budget }, () => this.sessions.get(key) === session && session.generation === generation && (options.current?.() ?? true));
      if (!slice || this.sessions.get(key) !== session || session.generation !== generation) return undefined;
      if (slice.stale || !this.sameFingerprint(session, slice)) {
        // A newer page from a rebuilt projection cannot be appended to the
        // window the reader is holding; the window keeps its position and the
        // caller reports the reload instead of mixing two canonical states.
        return { ...this.projectionOf(session), kind: "stale", appendItems: [], removeIds: [] };
      }
      const pageEntries = asArray<HistoryEntry>(slice.entries);
      const appendItems = this.appendRecords(session, pageEntries);
      session.pages.push(this.pageFor(pageEntries, slice.nextCursor ?? "", slice.newerCursor ?? ""));
      session.newerCursor = slice.newerCursor ?? "";
      session.hasNewer = Boolean(slice.hasNewer) || session.newerCursor !== "";
      if (!session.hasNewer && (session.latestSequence ?? 0) > slice.revision && pageEntries.length) {
        const last = pageEntries[pageEntries.length - 1];
        if (last.message.messageId) {
          session.newerCursor = `reasonix:message:${encodeURIComponent(last.message.messageId)}:${session.latestSequence}:${encodeURIComponent(session.digest)}:newer`;
          session.hasNewer = true;
        }
      }
      session.endTurn = slice.endTurn ?? session.endTurn;
      session.totalTurns = slice.totalTurns ?? session.totalTurns;
      const reclaimed = this.trimWindow(session, "newer");
      this.enforceBudgets();
      const projection = this.projectionOf(session);
      if (this.sessions.get(key) !== session) return undefined;
      return { ...projection, kind: "append", appendItems, removeIds: reclaimed };
    } finally {
      session.newerInFlight = false;
    }
  }

  private matchesExpectedFingerprint(session: SessionTranscript, expectedRevision?: number, expectedDigest?: string): boolean {
    const digest = (expectedDigest ?? "").trim();
    const revisionKnown = typeof expectedRevision === "number" && expectedRevision > 0;
    if (digest !== "" && session.digest !== digest) return false;
    if (revisionKnown && (!session.revisionKnown || session.revision !== expectedRevision)) return false;
    if (!revisionKnown && digest === "") {
      // Metadata identity temporarily missing cannot prove a known resident
      // projection is current. A backend round trip is the safe fallback.
      return !session.revisionKnown && session.digest === "";
    }
    return true;
  }

  private sameFingerprint(session: SessionTranscript, slice: HistorySlice): boolean {
    if (session.canonicalV2 && session.digest !== "") return session.digest === (slice.digest ?? "");
    return session.revision === (slice.revision ?? 0) &&
      session.revisionKnown === sliceRevisionKnown(slice) &&
      session.digest === (slice.digest ?? "");
  }

  // ── lazy content ──────────────────────────────────────────────────────────

  private sessionForEntry(tabId: string, entryId: string): SessionTranscript | undefined {
    for (const session of this.sessions.values()) {
      if (session.tabId === tabId && session.byId.has(entryId)) return session;
    }
    return undefined;
  }

  /**
   * Fetch the full value of a ref-replaced field, chunk by chunk, and fold it
   * into the record. Late (generation-stale) responses are discarded; a stale
   * chunk marks the ref stale and keeps the inline preview.
   */
  hasContentResolver(tabId: string): boolean { return Boolean(this.contentResolvers.active(tabId)); }

  publishToolDetails(tabId: string, item: Extract<Item, { kind: "tool" }>, text: string): void {
    let value: Record<string, unknown>;
    try { value = JSON.parse(text); } catch { return; }
    if (!value || typeof value !== "object" || Array.isArray(value)) return;
    if (value.execution == null || typeof value.execution !== "object" || Array.isArray(value.execution)) return;
    const execution = value.execution as NonNullable<typeof item.execution>;
    if (typeof execution.state !== "string" || (execution.exitCode != null && typeof execution.exitCode !== "number")) return;
    // The reducer compares the exact requested item version. A newer event,
    // snapshot or session replacement always wins over this detached read.
    const patch = { ...item, execution };
    for (const listener of this.listeners.get(tabId) ?? []) {
      listener({ tabId, patches: { [item.id]: patch }, expected: { [item.id]: item } });
    }
  }

  hasContentReference(tabId: string, entryId: string, field: string): boolean {
    entryId = resolveTranscriptEntryAlias(this.sessions.values(), tabId, entryId);
    return Boolean(this.sessionForEntry(tabId, entryId)?.byId.get(entryId)?.refs.some(ref => ref.field === field || ref.field === "canonicalMessage"));
  }

  /** Detached legacy tool reads use the exact call reference, not a field-only
   * cache key shared by several calls. Full bodies belong to the drawer. */
  async requestToolContent(tabId: string, item: Extract<Item, { kind: "tool" }>, value: Record<string, unknown>): Promise<string | undefined> {
    const source = [...this.sessions.values()].find(session => session.tabId === tabId &&
      [...session.contributions.values()].some(items => items.some(candidate => candidate.id === item.id)));
    if (!source) return undefined;
    const generation = source.generation;
    const { readTranscriptToolContent } = await import("./transcriptToolContent");
    if (this.sessions.get(source.key) !== source || source.generation !== generation) throw new Error("Tool reference expired; retry");
    return readTranscriptToolContent({ sessions: this.sessions, backend: this.backend, requestFullContent: (tab, id, field) => this.requestFullContent(tab, id, field) }, tabId, item, value);
  }

  async requestFullContent(tabId: string, entryId: string, field: string): Promise<string | undefined> {
    const resolver = this.contentResolvers.active(tabId);
    if (resolver) return resolver.resolve(entryId, field);
    return readTranscriptContent({
      locate: id => {
        const resolvedId = resolveTranscriptEntryAlias(this.sessions.values(), tabId, id);
        const session = this.sessionForEntry(tabId, resolvedId);
        return session ? { session, entryId: resolvedId } : undefined;
      },
      resident: session => this.sessions.get(session.key) === session,
      read: (ref, index) => this.backend.HistoryContentForTab(tabId, ref, index),
      publish: (session, record) => {
        this.reconvertAndNotify(session, record);
        this.enforceBudgets();
      },
    }, entryId, field);
  }

  private reconvertAndNotify(session: SessionTranscript, rec: TranscriptRecord): void {
    // A resolved field on a CONSUMED tool-result row shows up in the claiming
    // call's tool item, so re-convert the claimer instead of the row.
    const targetId = session.consumedBy.get(rec.entryId) ?? rec.entryId;
    const target = session.byId.get(targetId);
    if (!target) return;
    // Re-convert with the record's established claims so tool results stay put.
    const view = this.viewOf(session.records);
    const consumed = new Set(session.consumed);
    for (const claimed of session.matchTables.get(targetId)?.values() ?? []) consumed.delete(claimed);
    const conversion = convertRecord(target, view, consumed, session.matchTables.get(targetId));
    session.consumed = consumed;
    this.trackConversion(session, target, conversion);
    session.itemsCache = null;
    this.rebuildProjection(session);
    const listeners = this.listeners.get(session.tabId);
    if (!listeners || listeners.size === 0) return;
    const patches: Record<string, Item> = {};
    for (const item of conversion.items) patches[item.id] = item;
    const change: TranscriptContentChange = { tabId: session.tabId, patches };
    for (const listener of listeners) listener(change);
  }

  // ── markdown cache (populated by the rendering/worker phase) ──────────────

  getMarkdown(entryId: string, revision: number): ParsedMarkdownValue | undefined {
    return this.markdown.get(entryId, revision);
  }

  setMarkdown(entryId: string, revision: number, value: ParsedMarkdownValue): void {
    this.markdown.set(entryId, revision, value);
  }

  pinMarkdown(entryId: string, revision: number): () => void {
    return this.markdown.pin(entryId, revision);
  }

  markdownCacheSize(): number {
    return this.markdown.size();
  }

  subscribe(tabId: string, listener: (change: TranscriptContentChange) => void): () => void {
    let set = this.listeners.get(tabId);
    if (!set) {
      set = new Set();
      this.listeners.set(tabId, set);
    }
    set.add(listener);
    return () => {
      set.delete(listener);
      if (set.size === 0) this.listeners.delete(tabId);
    };
  }
}

// Bridge-backed singleton: resolves the host bindings at call time through
// the app proxy, so test/dev mocks install whenever they appear.
let singleton: TranscriptStore | undefined;

export function getTranscriptStore(): TranscriptStore {
  if (!singleton) {
    singleton = new TranscriptStore({
      HistorySliceForTab: (tabID, req) => canonicalHistorySlice(tabID, req),
      HistoryContentForTab: canonicalHistoryContent,
    });
  }
  return singleton;
}

registerTranscriptCacheDiagnostics(() =>
  singleton?.stats() ?? {
    residentSessions: 0,
    maxResidentSessions: DEFAULT_MAX_RESIDENT_SESSIONS,
    bodyBytes: 0,
    bodyBudgetBytes: DEFAULT_HISTORY_BODY_BUDGET,
    markdownBytes: 0,
    markdownBudgetBytes: DEFAULT_MARKDOWN_BUDGET,
    historyEvictions: 0,
    markdownEvictions: 0,
    reclaimedPages: 0,
    residentWindowEntries: 0,
    windowMaxPages: DEFAULT_WINDOW_MAX_PAGES,
  },
);
