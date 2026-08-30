// Run: tsx src/__tests__/transcript-store.test.ts
//
// TranscriptStore unit tests over a fake slice backend: stable ids, page
// concatenation fidelity vs the single-shot conversion, weighted LRU
// eviction, generation-bound request discard, stale cursors, lazy content
// refs, and the markdown cache budget.

import { TranscriptStore } from "../lib/transcriptStore";
import { historyPageRequestBudget } from "../lib/historyPaging";
import { historyMessagesToItems, type Item } from "../lib/useController";
import type {
  HistoryContentChunk,
  HistoryContentRef,
  HistoryEntry,
  HistoryMessage,
  HistorySlice,
  HistorySliceRequest,
} from "../lib/types";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  ok(actual === expected, `${label}${actual === expected ? "" : `: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`}`);
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

// ── fake slice backend ──────────────────────────────────────────────────────

type RefTable = Map<string, string>; // `${entryId}:${field}` -> full content

class FakeBackend {
  sliceCalls: HistorySliceRequest[] = [];
  contentCalls: Array<{ ref: HistoryContentRef; chunk: number }> = [];
  sliceGate: ReturnType<typeof deferred<HistorySlice>> | undefined;
  contentGate: ReturnType<typeof deferred<HistoryContentChunk>> | undefined;
  staleNextCursor = false;
  revision = 1;
  digest = "digest-1";

  constructor(
    private readonly messages: HistoryMessage[],
    private readonly refs: RefTable = new Map(),
    private readonly sessionId = "s1",
  ) {}

  private entryId(index: number): string {
    return `${this.sessionId}:r0:m${index}:o0`;
  }

  private entriesFor(lo: number, hi: number): HistoryEntry[] {
    let turn = 0;
    const turnsOf: number[] = [];
    for (const message of this.messages) {
      if (message.role === "user") turn += 1;
      turnsOf.push(turn);
    }
    return this.messages.slice(lo, hi).map((message, offset) => {
      const index = lo + offset;
      const entryId = this.entryId(index);
      const refs: HistoryContentRef[] = [];
      let msg = message;
      const full = this.refs.get(`${entryId}:content`);
      if (full !== undefined) {
        msg = { ...message, content: full.slice(0, 16) };
        refs.push({ entryId, field: "content", size: full.length, chunks: 2, revision: 1, digest: "d" });
      }
      return { entryId, turn: turnsOf[index], order: index, message: msg, refs };
    });
  }

  slice(lo: number, hi: number): HistorySlice {
    const entries = this.entriesFor(lo, hi);
    const turns = entries.map((entry) => entry.turn).filter((value) => value > 0);
    return {
      entries,
      nextCursor: lo > 0 ? btoa(JSON.stringify({ v: 1, before: lo })) : "",
      hasOlder: lo > 0,
      totalTurns: this.messages.filter((message) => message.role === "user").length,
      startTurn: turns.length > 0 ? Math.min(...turns) : 0,
      endTurn: turns.length > 0 ? Math.max(...turns) : 0,
      stale: false,
      revision: this.revision,
      revisionKnown: true,
      digest: this.digest,
    };
  }

  // Turn- and entry-budgeted windowing, mirroring the Go slice semantics for
  // the compact stress fixtures used below.
  async HistorySliceForTab(_tabID: string, req: HistorySliceRequest): Promise<HistorySlice> {
    this.sliceCalls.push(req);
    if (this.sliceGate) {
      const gate = this.sliceGate;
      this.sliceGate = undefined;
      return gate.promise;
    }
    let before = this.messages.length;
    if (req.cursor) {
      if (this.staleNextCursor) return { entries: [], nextCursor: "", hasOlder: false, totalTurns: 0, startTurn: 0, endTurn: 0, stale: true, revision: this.revision, revisionKnown: true, digest: this.digest };
      const decoded = JSON.parse(atob(req.cursor)) as { before?: number };
      before = Math.min(before, decoded.before ?? before);
    }
    if (before <= 0 || this.messages.length === 0) return this.slice(0, 0);
    let turn = 0;
    const turnsOf: number[] = [];
    for (const message of this.messages) {
      if (message.role === "user") turn += 1;
      turnsOf.push(turn);
    }
    const turns = Math.max(1, Math.floor(req.turns || 12));
    const newestTurn = turnsOf[before - 1];
    const oldestTurn = newestTurn > 0 ? Math.max(newestTurn - turns + 1, 1) : 0;
    let lo = 0;
    if (oldestTurn > 1) {
      lo = before;
      for (let i = 0; i < before; i += 1) {
        if (turnsOf[i] >= oldestTurn) { lo = i; break; }
      }
    }
    const entries = Math.max(1, Math.floor(req.entries || 120));
    lo = Math.max(lo, before - entries);
    return this.slice(lo, before);
  }

  async HistoryContentForTab(_tabID: string, ref: HistoryContentRef, chunkIndex: number): Promise<HistoryContentChunk> {
    this.contentCalls.push({ ref, chunk: chunkIndex });
    if (this.contentGate) {
      const gate = this.contentGate;
      this.contentGate = undefined;
      return gate.promise;
    }
    const full = this.refs.get(`${ref.entryId}:${ref.field}`) ?? "";
    const half = Math.ceil(full.length / 2);
    const data = chunkIndex === 0 ? full.slice(0, half) : full.slice(half);
    return { entryId: ref.entryId, field: ref.field, chunk: chunkIndex, chunks: 2, data, done: chunkIndex >= 1, stale: false };
  }
}

// ── fixtures ────────────────────────────────────────────────────────────────

function bigTranscript(turns: number): HistoryMessage[] {
  const messages: HistoryMessage[] = [];
  for (let i = 0; i < turns; i += 1) {
    messages.push({ role: "user", content: `prompt ${i}` });
    messages.push({ role: "assistant", content: `answer ${i}`, reasoning: `think ${i}` });
    messages.push({
      role: "assistant",
      content: "",
      toolCalls: [
        { id: `c${i}a`, name: "read_file", arguments: `{"path":"f${i}"}` },
        { id: `c${i}b`, name: "bash", arguments: `echo ${i}` },
      ],
    });
    messages.push({ role: "tool", toolCallId: `c${i}a`, toolName: "read_file", content: `read result ${i}` });
    messages.push({ role: "tool", toolCallId: `c${i}b`, toolName: "bash", content: `bash output ${i}` });
    if (i % 4 === 1) {
      // Positional (id-less) call/result pair.
      messages.push({ role: "assistant", content: "", toolCalls: [{ id: "", name: "grep", arguments: `needle ${i}` }] });
      messages.push({ role: "tool", toolName: "grep", content: `grep output ${i}` });
    }
    if (i % 3 === 0) messages.push({ role: "phase", content: `phase ${i}` });
    if (i % 5 === 2) messages.push({ role: "notice", level: "info", content: `note ${i}` });
    if (i % 7 === 3) messages.push({ role: "compaction", content: "", trigger: "auto", messages: 12, summary: `sum ${i}`, archive: `arch ${i}` });
  }
  return messages;
}

function longArchivedTranscript(turns: number): HistoryMessage[] {
  const messages: HistoryMessage[] = [];
  for (let turn = 1; turn <= turns; turn += 1) {
    const callId = `archived-${turn}`;
    messages.push({ role: "user", content: `prompt ${turn}` });
    messages.push({
      role: "assistant",
      content: `answer ${turn}`,
      toolCalls: [{
        id: callId,
        name: "bash",
        arguments: "",
        argumentsArchived: true,
        subject: `command ${turn}`,
        summary: "1 line",
      }],
    });
    messages.push({
      role: "tool",
      toolCallId: callId,
      toolName: "bash",
      content: "",
      toolResultArchived: true,
    });
  }
  return messages;
}

// Canonical shape for cross-scheme equality (ids are scheme-dependent and
// verified separately).
function canon(items: Item[]): unknown[] {
  return items.map((it) => {
    switch (it.kind) {
      case "user": return ["user", it.text, it.submitText ?? null];
      case "assistant": return ["assistant", it.text, it.reasoning];
      case "phase": return ["phase", it.text];
      case "notice": return ["notice", it.level, it.text, it.detail ?? null];
      case "compaction": return ["compaction", it.trigger, it.summary, it.archive];
      case "tool": return ["tool", it.name, it.args, it.output ?? null, it.error ?? null, it.status, it.subject ?? null, it.summary ?? null];
      case "extension": return ["extension", it.surfaceKey];
    }
  });
}

function canonEqual(a: Item[], b: Item[]): boolean {
  return JSON.stringify(canon(a)) === JSON.stringify(canon(b));
}

async function drainOlder(store: TranscriptStore, tabId: string, path: string, turns: number): Promise<void> {
  for (let guard = 0; guard < 100; guard += 1) {
    const result = await store.loadOlder(tabId, path, { turns });
    if (!result || result.kind !== "prepend" || !result.hasOlder) return;
  }
  throw new Error("paging did not terminate");
}

console.log("\ntranscript store");

// ── page concatenation equals single-shot conversion ────────────────────────
{
  const messages = bigTranscript(46);
  const backend = new FakeBackend(messages);
  const store = new TranscriptStore(backend);
  const first = await store.loadLatest("tab-1", "/s/one.jsonl", { turns: 12 });
  ok(!!first && first.items.length > 0, "latest page projects items");
  eq(first?.hasOlder, true, "latest page reports older history");
  const projectedTurns = (first?.items ?? [])
    .filter((item): item is Extract<Item, { kind: "user" }> => item.kind === "user")
    .map((item) => item.historyTurn);
  eq(projectedTurns[projectedTurns.length - 1], 46, "history user items retain their absolute turn for complete-session navigation");
  ok(projectedTurns.every((turn) => Number.isInteger(turn) && (turn ?? 0) > 0), "every paged user item carries an absolute history turn");
  const firstIds = (first?.items ?? []).map((item) => item.id);
  await drainOlder(store, "tab-1", "/s/one.jsonl", 12);
  const full = store.peek("tab-1", "/s/one.jsonl");
  const singleShot = historyMessagesToItems(messages, "h").items;
  ok(canonEqual(full?.items ?? [], singleShot), `paged concatenation equals single-shot conversion (${singleShot.length} items from ${messages.length} messages)`);
  const fullIds = (full?.items ?? []).map((item) => item.id);
  eq(JSON.stringify(fullIds.slice(fullIds.length - firstIds.length)), JSON.stringify(firstIds), "newest page item ids are stable across prepends");
  const unique = new Set(fullIds);
  eq(unique.size, fullIds.length, "item ids are unique across the full projection");
}

// ── 10,000-turn targeted paging stays bounded ──────────────────────────────
{
  const messages = longArchivedTranscript(10_000);
  const backend = new FakeBackend(messages, new Map(), "stress");
  const store = new TranscriptStore(backend);
  const startedAt = performance.now();
  let projection = await store.loadLatest("tab-stress", "/s/stress.jsonl", { turns: 60 });
  let pages = 1;
  while (projection?.hasOlder && pages <= 40) {
    const budget = historyPageRequestBudget(projection.startTurn, projection.totalTurns, 1);
    const older = await store.loadOlder("tab-stress", "/s/stress.jsonl", budget);
    if (!older) break;
    projection = older;
    pages += 1;
  }
  const elapsedMs = performance.now() - startedAt;
  const users = (projection?.items ?? []).filter((item): item is Extract<Item, { kind: "user" }> => item.kind === "user");
  eq(projection?.hasOlder, false, "10,000-turn target paging reaches the first page");
  eq(pages, 32, "10,000-turn target paging respects both turn and production entry bounds");
  eq(backend.sliceCalls.length, 32, "10,000-turn target paging performs the expected bounded backend calls");
  ok(backend.sliceCalls.slice(1).every((request) => request.entries === 1000), "targeted pages use the backend's bounded 1000-entry capacity");
  eq(users.length, 10_000, "10,000-turn target paging preserves every question");
  eq(users[0]?.historyTurn, 1, "10,000-turn target paging lands on absolute turn one");
  eq(users[users.length - 1]?.historyTurn, 10_000, "10,000-turn target paging keeps the tail coordinate");
  ok(new Set(projection?.items.map((item) => item.id)).size === projection?.items.length, "10,000-turn target paging keeps item ids unique");
  const stats = store.stats();
  ok(stats.bodyBytes <= stats.bodyBudgetBytes, "10,000-turn transcript stays within the production history body budget");
  ok(elapsedMs < 10_000, `10,000-turn targeted paging completes within 10s (${elapsedMs.toFixed(1)}ms)`);
}

// ── cross-page tool call/result merge ───────────────────────────────────────
{
  const messages: HistoryMessage[] = [
    { role: "user", content: "p1" },
    { role: "assistant", content: "", toolCalls: [{ id: "call-1", name: "bash", arguments: "ls" }] },
    { role: "tool", toolCallId: "call-1", toolName: "bash", content: "/root" },
    { role: "user", content: "p2" },
    { role: "assistant", content: "done" },
  ];
  // Cut between the call and its result: newest page starts at the result row.
  const backend = new FakeBackend(messages);
  backend.HistorySliceForTab = async (tabID, req) => {
    void tabID;
    if (!req.cursor) return backend.slice(2, messages.length);
    const decoded = JSON.parse(atob(req.cursor)) as { before?: number };
    return backend.slice(0, Math.min(decoded.before ?? 0, 2));
  };
  const store = new TranscriptStore(backend);
  const first = await store.loadLatest("tab-x", "/s/x.jsonl", { turns: 12 });
  const standalone = (first?.items ?? []).filter((item) => item.kind === "tool");
  eq(standalone.length, 1, "result row converts standalone before its call pages in");
  eq(standalone[0]?.kind === "tool" && standalone[0].id, "call-1", "standalone result keeps the toolCallId item id");
  const older = await store.loadOlder("tab-x", "/s/x.jsonl", { turns: 12 });
  eq(older?.kind, "prepend", "older page prepends");
  eq(older?.removeIds.length, 1, "the standalone result item is superseded by the merged call item");
  const merged = (older?.items ?? []).filter((item) => item.kind === "tool");
  eq(merged.length, 1, "exactly one tool item after the merge (no duplicate)");
  const tool = merged[0]?.kind === "tool" ? merged[0] : undefined;
  eq(tool?.args, "ls", "merged tool item takes the call's args");
  eq(tool?.output, "/root", "merged tool item takes the result's output");
  eq(tool?.status, "done", "merged tool item is done");
  const singleShot = historyMessagesToItems(messages, "h").items;
  ok(canonEqual(older?.items ?? [], singleShot), "merged projection equals single-shot conversion");
}

// ── append (live tail) ──────────────────────────────────────────────────────
{
  const messages: HistoryMessage[] = [
    { role: "user", content: "p1" },
    { role: "assistant", content: "a1" },
  ];
  const backend = new FakeBackend(messages);
  const store = new TranscriptStore(backend);
  const first = await store.loadLatest("tab-a", "/s/a.jsonl", { turns: 12 });
  const baseIds = (first?.items ?? []).map((item) => item.id);
  const appended = store.appendEntries("tab-a", "/s/a.jsonl", [
    { entryId: "s1:r0:m2:o0", turn: 2, order: 2, message: { role: "user", content: "p2" }, refs: [] },
    { entryId: "s1:r0:m3:o0", turn: 2, order: 3, message: { role: "assistant", content: "a2" }, refs: [] },
  ]);
  eq(appended.length, 2, "append contributes the new rows' items");
  const projection = store.peek("tab-a", "/s/a.jsonl");
  eq(JSON.stringify((projection?.items ?? []).slice(0, baseIds.length).map((item) => item.id)), JSON.stringify(baseIds), "append keeps existing item ids");
  eq(projection?.items.length, baseIds.length + 2, "append grows the projection");
}

// ── weighted LRU: count, pin, byte budget, re-open ──────────────────────────
{
  const backend = new FakeBackend([{ role: "user", content: "u" }, { role: "assistant", content: "a" }]);
  const store = new TranscriptStore(backend, { maxResidentSessions: 3 });
  await store.loadLatest("tab-1", "/s/1.jsonl");
  await store.loadLatest("tab-2", "/s/2.jsonl");
  await store.loadLatest("tab-3", "/s/3.jsonl");
  eq(store.residentSessionCount(), 3, "three sessions resident at the cap");
  await store.loadLatest("tab-4", "/s/4.jsonl");
  eq(store.isResident("tab-1", "/s/1.jsonl"), false, "fourth session evicts the least-recently-used one");
  eq(store.isResident("tab-4", "/s/4.jsonl"), true, "new session stays resident");

  store.setPinned("tab-2", true); // live/running tab: pinned out of the LRU count
  await store.loadLatest("tab-5", "/s/5.jsonl");
  eq(store.isResident("tab-3", "/s/3.jsonl"), true, "pinned sessions do not count toward the resident cap");
  await store.loadLatest("tab-6", "/s/6.jsonl");
  eq(store.isResident("tab-2", "/s/2.jsonl"), true, "pinned live session survives eviction");
  eq(store.isResident("tab-3", "/s/3.jsonl"), false, "oldest unpinned session evicts instead");
  store.setPinned("tab-2", false);

  const callsBeforeReopen = backend.sliceCalls.length;
  const reopened = await store.loadLatest("tab-1", "/s/1.jsonl");
  ok(backend.sliceCalls.length > callsBeforeReopen, "evicted session re-opens via a fresh slice fetch");
  eq(reopened?.items.length, 2, "re-opened session restores its full projection");
}

{
  const big = "x".repeat(600);
  const backend = new FakeBackend([{ role: "user", content: big }, { role: "assistant", content: big }]);
  const store = new TranscriptStore(backend, { maxResidentSessions: 10, historyBodyBudgetBytes: 4096 });
  await store.loadLatest("tab-1", "/s/1.jsonl");
  await store.loadLatest("tab-2", "/s/2.jsonl");
  await store.loadLatest("tab-3", "/s/3.jsonl");
  ok(store.totalBodyBytes() <= 4096, "history body budget holds across sessions");
  eq(store.isResident("tab-1", "/s/1.jsonl"), false, "byte budget evicts the oldest by weight");
  eq(store.isResident("tab-3", "/s/3.jsonl"), true, "newest session survives byte-budget eviction");
}

// ── markdown cache budget + LRU ─────────────────────────────────────────────
{
  const store = new TranscriptStore(new FakeBackend([]), { markdownBudgetBytes: 120 });
  const parsed = (text: string) => ({
    source: text,
    blocks: [],
    selectionText: text,
    selectionRevision: 1,
    bytes: text.length * 2,
  });
  store.setMarkdown("e1", 1, parsed("a".repeat(20))); // 40 bytes
  store.setMarkdown("e2", 1, parsed("b".repeat(20)));
  store.setMarkdown("e3", 1, parsed("c".repeat(20)));
  eq(store.getMarkdown("e1", 1)?.source, "a".repeat(20), "markdown cache returns stored value");
  store.setMarkdown("e4", 1, parsed("d".repeat(20))); // 160 > 120 → evict oldest (e2: e1 was touched)
  eq(store.getMarkdown("e2", 1), undefined, "markdown LRU evicts the least-recently-used entry");
  ok(store.getMarkdown("e1", 1) !== undefined, "recently read markdown entry survives");
  eq(store.getMarkdown("e1", 2), undefined, "markdown entries key on entryId + revision");

  const release = store.pinMarkdown("e1", 1);
  store.setMarkdown("e5", 1, parsed("e".repeat(50)));
  ok(store.getMarkdown("e1", 1) !== undefined, "active selection pins its markdown projection");
  release();
}

// ── lazy content refs ───────────────────────────────────────────────────────
{
  const full = "FULL-".repeat(40); // 200 chars
  const refs: RefTable = new Map([["s1:r0:m1:o0:content", full]]);
  const backend = new FakeBackend(
    [{ role: "user", content: "p1" }, { role: "assistant", content: "placeholder" }],
    refs,
  );
  const store = new TranscriptStore(backend);
  const changes: string[] = [];
  store.subscribe("tab-c", (change) => changes.push(...Object.keys(change.patches)));
  const first = await store.loadLatest("tab-c", "/s/c.jsonl", { turns: 12 });
  // The newest page's refs auto-resolve (asynchronously after the projection).
  await new Promise((resolve) => setTimeout(resolve, 0));
  eq(backend.contentCalls.length, 2, "refs in the newest page auto-fetch their chunks");
  eq(first?.hasOlder, false, "fixture fits in one page");
  const assistant = (store.peek("tab-c", "/s/c.jsonl")?.items ?? []).find((item) => item.kind === "assistant");
  eq(assistant?.kind === "assistant" && assistant.text, full, "resolved full content replaces the inline preview");
  ok(changes.includes("he:s1:r0:m1:o0"), "content resolution notifies subscribers with item patches");
  const again = await store.requestFullContent("tab-c", "s1:r0:m1:o0", "content");
  eq(again, full, "resolved content is served from the record");
  eq(backend.contentCalls.length, 2, "resolved content is not re-fetched");
}

{
  // Stale content fetch: ref marked stale, preview kept.
  const refs: RefTable = new Map([["s1:r0:m1:o0:content", "z".repeat(100)]]);
  const backend = new FakeBackend(
    [{ role: "user", content: "p1" }, { role: "assistant", content: "placeholder" }],
    refs,
  );
  backend.HistoryContentForTab = async (_tab, ref, chunk) => ({ entryId: ref.entryId, field: ref.field, chunk, chunks: 2, data: "", done: false, stale: true });
  const store = new TranscriptStore(backend);
  const first = await store.loadLatest("tab-s", "/s/s.jsonl", { turns: 12 });
  await new Promise((resolve) => setTimeout(resolve, 0));
  const assistant = (store.peek("tab-s", "/s/s.jsonl")?.items ?? []).find((item) => item.kind === "assistant");
  eq(assistant?.kind === "assistant" && assistant.text, "z".repeat(16), "stale ref keeps the inline preview");
  eq(first !== undefined, true, "latest page still projects");
}

// ── generation: superseded / evicted loads discard late responses ───────────
{
  const backend = new FakeBackend([{ role: "user", content: "u" }, { role: "assistant", content: "a" }]);
  const store = new TranscriptStore(backend);
  backend.sliceGate = deferred<HistorySlice>();
  const firstGate = backend.sliceGate;
  const p1 = store.loadLatest("tab-g", "/s/g.jsonl");
  backend.sliceGate = deferred<HistorySlice>();
  const secondGate = backend.sliceGate;
  const p2 = store.loadLatest("tab-g", "/s/g.jsonl"); // supersedes: bumps generation
  firstGate.resolve(backend.slice(0, 2));
  eq(await p1, undefined, "superseded load discards its late response");
  secondGate.resolve(backend.slice(0, 2));
  const projection = await p2;
  eq(projection?.items.length, 2, "the latest load wins");

  backend.sliceGate = deferred<HistorySlice>();
  const gate = backend.sliceGate;
  const p3 = store.loadLatest("tab-h", "/s/h.jsonl");
  store.evictTab("tab-h"); // pruned/closed before the response lands
  gate.resolve(backend.slice(0, 2));
  eq(await p3, undefined, "evicted session discards its late response");
  eq(store.isResident("tab-h", "/s/h.jsonl"), false, "evicted records never land");
}

{
  // Content chunks arriving after a fresh load (session switch) are discarded.
  const full = "y".repeat(80);
  const refs: RefTable = new Map([["s1:r0:m1:o0:content", full]]);
  const backend = new FakeBackend(
    [{ role: "user", content: "p1" }, { role: "assistant", content: "placeholder" }],
    refs,
  );
  const store = new TranscriptStore(backend);
  backend.contentGate = deferred<HistoryContentChunk>();
  const staleGate = backend.contentGate;
  const first = store.loadLatest("tab-l", "/s/l.jsonl", { turns: 12 });
  await new Promise((resolve) => setTimeout(resolve, 0));
  eq(backend.contentCalls.length, 1, "auto-fetch of the first load is in flight");
  // A fresh load (session switch/rebind) bumps the generation while the first
  // load's content request is still awaiting its chunk.
  const reload = store.loadLatest("tab-l", "/s/l.jsonl", { turns: 12 });
  staleGate.resolve({ entryId: "s1:r0:m1:o0", field: "content", chunk: 0, chunks: 2, data: "STALE", done: true, stale: false });
  await first;
  await reload;
  await new Promise((resolve) => setTimeout(resolve, 0));
  const assistant = (store.peek("tab-l", "/s/l.jsonl")?.items ?? []).find((item) => item.kind === "assistant");
  eq(assistant?.kind === "assistant" && assistant.text, full, "late content chunk from a previous generation is discarded");
}

// ── stale cursor reloads from the latest page ───────────────────────────────
{
  const messages: HistoryMessage[] = [];
  for (let i = 0; i < 30; i += 1) {
    messages.push({ role: "user", content: `p${i}` });
    messages.push({ role: "assistant", content: `a${i}` });
  }
  const backend = new FakeBackend(messages);
  const store = new TranscriptStore(backend);
  await store.loadLatest("tab-r", "/s/r.jsonl", { turns: 10 });
  backend.staleNextCursor = true; // the session was rewritten behind the cursor
  const result = await store.loadOlder("tab-r", "/s/r.jsonl", { turns: 10 });
  eq(result?.kind, "reload", "stale cursor triggers a latest-page reload");
  eq(result?.items.length, 20, "reload replaces with the fresh newest page");
  backend.staleNextCursor = false;
  const older = await store.loadOlder("tab-r", "/s/r.jsonl", { turns: 10 });
  eq(older?.kind, "prepend", "paging resumes after the reload");
  eq(older?.prependItems.length, 20, "older page prepends after reload");
}

// ── same-path resident identity ────────────────────────────────────────────
{
  const backend = new FakeBackend([{ role: "user", content: "u" }, { role: "assistant", content: "a" }]);
  const store = new TranscriptStore(backend);
  await store.loadLatest("tab-fp", "/s/fp.jsonl", { expectedRevision: 1, expectedDigest: "digest-1" });
  const callsAfterFirstLoad = backend.sliceCalls.length;
  const resident = await store.loadLatest("tab-fp", "/s/fp.jsonl", {
    preferResident: true,
    expectedRevision: 1,
    expectedDigest: "digest-1",
  });
  eq(backend.sliceCalls.length, callsAfterFirstLoad, "matching canonical fingerprint reuses the resident projection");
  eq(resident?.revision, 1, "resident projection retains its canonical revision");

  backend.revision = 2;
  backend.digest = "digest-2";
  const refreshed = await store.loadLatest("tab-fp", "/s/fp.jsonl", {
    preferResident: true,
    expectedRevision: 2,
    expectedDigest: "digest-2",
  });
  eq(backend.sliceCalls.length, callsAfterFirstLoad + 1, "changed same-path fingerprint bypasses the resident projection");
  eq(refreshed?.digest, "digest-2", "fresh projection adopts the advanced canonical digest");

  backend.HistorySliceForTab = async () => ({ ...backend.slice(0, 2), revision: 3, revisionKnown: undefined, digest: "digest-3" });
  const compatible = await store.loadLatest("tab-fp", "/s/fp.jsonl", {
    preferResident: true,
    expectedRevision: 3,
    expectedDigest: "digest-3",
  });
  eq(compatible?.revisionKnown, true, "positive legacy slice revision implies a known canonical identity");
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
