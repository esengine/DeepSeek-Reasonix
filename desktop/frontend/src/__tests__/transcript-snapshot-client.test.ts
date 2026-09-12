import assert from "node:assert/strict";
import type { TranscriptSnapshot } from "../lib/transcriptProtocol";
import type { SnapshotTransport } from "../lib/transcriptSnapshotClient";
import { installDesktopHostStub } from "./desktopHostStub";

Object.defineProperty(globalThis, "window", { configurable: true, value: {} });
installDesktopHostStub({});
const [{ TurnEventProjector }, { TranscriptSnapshotClient, resolveSnapshotItems, resolveSnapshotTool, rebaseSnapshotContentPatches }, { initialState, reducer, historyMessagesToItems }] = await Promise.all([
  import("../lib/turnEventProjection"), import("../lib/transcriptSnapshotClient"), import("../lib/useController"),
]);
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}
function cut(overrides: Partial<TranscriptSnapshot> = {}): TranscriptSnapshot {
  return { protocolVersion: 1, snapshotId: "cut-1", identity: { sessionId: "session", headId: "head", rewriteEpoch: 0, runtimeEpoch: "epoch" },
    projectionRevision: 4, coveredThroughSeq: 4, records: [
      { id: "m:user", order: 0, message: { role: "user", messageId: "user", submissionId: "submit", content: "same question", historyTurn: 1 }, refs: [] },
      { id: "m:assistant", order: 1, message: { role: "assistant", messageId: "assistant", content: "prefix", pending: true }, refs: [] },
    ], activeRecords: [], runtime: { turnId: "turn", submissionId: "submit", status: "in_progress", pendingEvents: [] },
    activeAttempts: [{ id: "attempt", messageId: "assistant" }], before: 0, hasOlder: false, totalRecords: 2, totalTurns: 1, stale: false, ...overrides };
}
const quietTransport = { replay: async () => ({ events: [], floorSeq: 1, latestSeq: 4, nextAfterSeq: 4, hasMore: false, resetRequired: false, runtimeEpoch: "epoch" }) };
const transport: SnapshotTransport = { snapshot: async () => cut(), page: async () => cut(), content: async () => ({ data: "", nextOffset: 0, done: true, stale: false }) };

// Hydration suspends the live suffix, installs a full prefix, then advances the
// cursor only after reducer commits. Equal user text is never the correlation key.
{
  let state = reducer(initialState, { type: "user", text: "same question", seq: 0, submissionId: "submit" });
  const userID = state.items[0].id;
  const fetched = deferred<TranscriptSnapshot>();
  const projector = new TurnEventProjector(quietTransport);
  const client = new TranscriptSnapshotClient({ ...transport, snapshot: () => fetched.promise }, projector);
  projector.bind((event) => { client.observeEvent("tab", event); state = reducer(state, { type: "event", e: event }); });
  const loading = client.load("tab", (snapshot) => { state = reducer(state, { type: "transcript_snapshot", snapshot }); });
  projector.receiveLive("tab", { kind: "text", seq: 5, runtimeEpoch: "epoch", sessionId: "session", messageId: "assistant", text: " suffix" });
  fetched.resolve(cut());
  assert.equal(await loading, true);
  for (let i = 0; i < 30; i++) await Promise.resolve();
  assert.equal(state.live?.text, "prefix suffix");
  assert.equal(state.items.filter((item) => item.kind === "user").length, 1);
  assert.equal(state.items.find((item) => item.kind === "user")?.id, userID);
}

// A full active prefix is fetched from the same immutable cut before install.
{
  const content = deferred<{ data: string; nextOffset: number; done: boolean; stale: boolean }>();
  const snapshot = cut();
  snapshot.records[1].refs = [{ snapshotId: "cut-1", recordId: "m:assistant", path: ["content"], bytes: 11 }];
  let committed: TranscriptSnapshot | undefined;
  const projector = new TurnEventProjector(quietTransport);
  projector.bind(() => {});
  const client = new TranscriptSnapshotClient({ ...transport, snapshot: async () => snapshot, content: () => content.promise }, projector);
  const loading = client.load("tab", (value) => { committed = value; });
  await Promise.resolve();
  assert.equal(committed, undefined);
  content.resolve({ data: "full prefix", nextOffset: 11, done: true, stale: false });
  await loading;
  assert.equal((committed as TranscriptSnapshot | undefined)?.records[1].message.content, "full prefix");
}

// Session replacement fences both snapshot responses and content reads.
{
  const old = deferred<TranscriptSnapshot>();
  const projector = new TurnEventProjector(quietTransport);
  projector.bind(() => {});
  const client = new TranscriptSnapshotClient({ ...transport, snapshot: () => old.promise }, projector);
  let commits = 0;
  const loading = client.load("tab", () => { commits++; });
  client.release("tab");
  old.resolve(cut());
  assert.equal(await loading, false);
  assert.equal(commits, 0);
}

// Older pages cannot resurrect a discarded active attempt or replace a live
// tool result. Rows split across page boundaries merge by tool call identity.
{
  let state = initialState;
  const projector = new TurnEventProjector(quietTransport);
  const newest = cut({ records: [{ id: "tool:call", order: 2, message: { role: "tool", content: "result", toolCallId: "call", toolName: "read_file" }, refs: [] }],
    activeRecords: [cut().records[1]], totalRecords: 3, before: 2, hasOlder: true });
  const older = cut({ records: [cut().records[0], { ...cut().records[1], message: { ...cut().records[1].message,
    toolCalls: [{ id: "call", name: "read_file", arguments: "args" }] } }], totalRecords: 3 });
  const client = new TranscriptSnapshotClient({ ...transport, snapshot: async () => newest, page: async () => older }, projector);
  projector.bind((event) => { client.observeEvent("tab", event); state = reducer(state, { type: "event", e: event }); });
  await client.load("tab", (snapshot) => { state = reducer(state, { type: "transcript_snapshot", snapshot }); });
  await client.older("tab", (snapshot) => { state = reducer(state, { type: "transcript_page", snapshot }); });
  const tool = state.items.find((item) => item.kind === "tool");
  assert.equal(tool?.kind === "tool" && tool.args, "args");
  assert.equal(tool?.kind === "tool" && tool.output, "result");
  assert.equal(state.items.filter((item) => item.id === "call").length, 1);
}

// A delayed content response cannot overwrite a text mutation after the cut.
{
  const snapshot = cut({ activeAttempts: [], runtime: { status: "completed", pendingEvents: [] } });
  snapshot.records[1].message.pending = false;
  snapshot.records[1].refs = [{ snapshotId: "cut-1", recordId: "m:assistant", path: ["content"], bytes: 4 }];
  const content = deferred<{ data: string; nextOffset: number; done: boolean; stale: boolean }>();
  const projector = new TurnEventProjector(quietTransport);
  let state = initialState;
  const client = new TranscriptSnapshotClient({ ...transport, snapshot: async () => snapshot, content: () => content.promise }, projector);
  projector.bind((event) => { client.observeEvent("tab", event); state = reducer(state, { type: "event", e: event }); });
  await client.load("tab", (value) => { state = reducer(state, { type: "transcript_snapshot", snapshot: value }); });
  const pending = resolveSnapshotItems(client, "tab", "m:assistant", () => state, historyMessagesToItems,
    (patches) => { state = reducer(state, { type: "history_items_patch", patches }); });
  projector.receiveLive("tab", { kind: "text", seq: 5, runtimeEpoch: "epoch", messageId: "assistant", text: " new" });
  content.resolve({ data: "old!", nextOffset: 4, done: true, stale: false });
  await pending;
  assert.equal(state.live?.text, "prefix new");
  assert.notEqual(state.items.find((item) => item.kind === "assistant")?.text, "old!");
}
// A pinned active user can precede the newest page by hundreds of records.
// Loading the middle must insert below that user, never move body above it.
{
  const record = (id: string, order: number) => ({ id: `m:${id}`, order, message: { role: "assistant", messageId: id, content: id }, refs: [] });
  const newest = cut({ records: [record("tail", 4)], activeRecords: [cut().records[0]], totalRecords: 5, before: 4, hasOlder: true, activeAttempts: [] });
  const page = cut({ records: [record("middle1", 2), record("middle2", 3)], activeRecords: [cut().records[0]], totalRecords: 5, before: 2, hasOlder: true });
  let state = initialState;
  const projector = new TurnEventProjector(quietTransport);
  projector.bind(() => {});
  const client = new TranscriptSnapshotClient({ ...transport, snapshot: async () => newest, page: async () => page }, projector);
  await client.load("tab", (snapshot) => { state = reducer(state, { type: "transcript_snapshot", snapshot }); });
  await client.older("tab", (snapshot) => { state = reducer(state, { type: "transcript_page", snapshot }); });
  assert.deepEqual(state.items.map((item) => item.id), ["m:user", "m:middle1", "m:middle2", "m:tail"]);
}

// A failed attempt removed by the suffix is a tombstone for older pages.
{
  let state = initialState;
  const newest = cut({ records: [], activeRecords: cut().records, before: 2, hasOlder: true });
  const projector = new TurnEventProjector(quietTransport);
  const client = new TranscriptSnapshotClient({ ...transport, snapshot: async () => newest, page: async () => cut() }, projector);
  projector.bind((event) => { client.observeEvent("tab", event); state = reducer(state, { type: "event", e: event }); });
  await client.load("tab", (snapshot) => { state = reducer(state, { type: "transcript_snapshot", snapshot }); });
  projector.receiveLive("tab", { kind: "stream_attempt", messageId: "assistant", seq: 5, runtimeEpoch: "epoch", streamAttempt: { id: "attempt", action: "discard" } });
  await client.older("tab", (snapshot) => { state = reducer(state, { type: "transcript_page", snapshot }); });
  assert.equal(state.items.some((item) => item.id === "m:assistant"), false);
}
// Preserving an optimistic bubble's mounted key must not orphan its content
// reference, including when callers still hold that original key.
{
  const full = "q".repeat(70000);
  let state = reducer(initialState, { type: "user", text: full, seq: 0, submissionId: "submit" });
  const mounted = state.items[0].id;
  const snapshot = cut({ records: [{ id: "m:user", order: 0,
    message: { role: "user", messageId: "user", submissionId: "submit", content: full.slice(0, 4096) },
    refs: [{ snapshotId: "cut-1", recordId: "m:user", path: ["content"], bytes: full.length }] }],
    activeAttempts: [], runtime: { status: "completed", pendingEvents: [] } });
  const projector = new TurnEventProjector(quietTransport);
  const client = new TranscriptSnapshotClient({ ...transport, snapshot: async () => snapshot,
    content: async (_, req) => ({ data: full.slice(req.offset, req.offset + 65536), nextOffset: Math.min(full.length, req.offset + 65536),
      done: req.offset + 65536 >= full.length, stale: false }) }, projector);
  await client.load("tab", (snapshot) => { state = reducer(state, { type: "transcript_snapshot", snapshot }); });
  assert.equal(state.items[0].id, mounted);
  await resolveSnapshotItems(client, "tab", mounted, () => state, historyMessagesToItems,
    (patches) => { state = reducer(state, { type: "history_items_patch", patches }); });
  assert.ok(state.items[0].kind === "user" && state.items[0].text === full, "complete user body replaces preview");
  assert.equal(state.items[0].id, mounted);
}

// A replay too large for the wire is recovered by an authoritative cut. The
// old replay must not reset the cursor installed by its own reset handler.
{
  let loads = 0;
  let state = initialState;
  const resetDone = deferred<void>();
  const projector = new TurnEventProjector({ replay: async () => ({ events: [], floorSeq: 1, latestSeq: 5,
    nextAfterSeq: 4, hasMore: false, resetRequired: true, runtimeEpoch: "epoch" }) });
  const client = new TranscriptSnapshotClient({ ...transport, snapshot: async () => {
    loads++;
    return loads === 1 ? cut() : cut({ snapshotId: "cut-2", coveredThroughSeq: 5, projectionRevision: 5 });
  } }, projector);
  const commit = (snapshot: TranscriptSnapshot) => { state = reducer(state, { type: "transcript_snapshot", snapshot }); };
  projector.bind(event => { state = reducer(state, { type: "event", e: event }); });
  projector.bindReset(async () => { const loaded = await client.load("tab", commit); resetDone.resolve(); return loaded; });
  await client.load("tab", commit);
  projector.refresh("tab");
  await resetDone.promise;
  projector.receiveLive("tab", { kind: "text", seq: 6, runtimeEpoch: "epoch", sessionId: "session", messageId: "assistant", text: " suffix" });
  assert.equal(loads, 2);
  assert.equal(state.live?.text, "prefix suffix");
}

// Full body loading must not eagerly resolve thought content from the same record.
{
  const requests: string[] = [];
  const snapshot = cut({ activeAttempts: [], runtime: { pendingEvents: [] }, records: [{ id: "m:finished", order: 0,
    message: { role: "assistant", messageId: "finished", content: "body preview", reasoning: "thought preview" },
    refs: ["content", "reasoning"].map(field => ({ snapshotId: "cut-1", recordId: "m:finished", path: [field], bytes: 4 })) }] });
  let state = initialState;
  const projector = new TurnEventProjector(quietTransport);
  const client = new TranscriptSnapshotClient({ ...transport, snapshot: async () => snapshot,
    content: async (_, ref) => { requests.push(ref.path[0]); return { data: ref.path[0] === "content" ? "body" : "mind", nextOffset: 4, done: true, stale: false }; } }, projector);
  await client.load("lazy", snapshot => { state = reducer(state, { type: "transcript_snapshot", snapshot }); });
  const id = state.items[0].id;
  const resolve = (field: string) => resolveSnapshotItems(client, "lazy", id, () => state, historyMessagesToItems,
    patches => { state = reducer(state, { type: "history_items_patch", patches }); }, field);
  await resolve("content");
  assert.deepEqual(requests, ["content"]);
  assert.ok(state.items[0].kind === "assistant" && state.items[0].text === "body" && state.items[0].reasoning === "thought preview");
  await resolve("reasoning");
  assert.deepEqual(requests, ["content", "reasoning"]);
  assert.ok(state.items[0].kind === "assistant" && state.items[0].text === "body" && String(state.items[0].reasoning) === "mind");
  client.release("lazy");
}
// Full tool details bypass archived Item previews, never mutate them, and remain
// available after closing/reopening without caching the fetched body in the cut.
for (const referenced of [false, true]) {
  const args = JSON.stringify({ command: "x".repeat(70000) });
  const output = "result".repeat(14000);
  const snapshot = cut({ activeAttempts: [], runtime: { pendingEvents: [] }, records: [
    { id: "m:call", order: 0, message: { role: "assistant", messageId: "call", content: "body preview", toolCalls: [
      { id: "tool-full", name: "bash", arguments: referenced ? "args preview" : args }, { id: "unopened-tool", name: "read_file", arguments: "other preview" }] },
      refs: referenced ? [
        { snapshotId: "cut-1", recordId: "m:call", path: ["toolCalls", "0", "arguments"], bytes: args.length },
        { snapshotId: "cut-1", recordId: "m:call", path: ["toolCalls", "1", "arguments"], bytes: 4 },
        { snapshotId: "cut-1", recordId: "m:call", path: ["content"], bytes: 4 },
      ] : [] },
    { id: "m:result", order: 1, message: { role: "tool", toolCallId: "tool-full", content: referenced ? "output preview" : output },
      refs: referenced ? [{ snapshotId: "cut-1", recordId: "m:result", path: ["content"], bytes: output.length }] : [] },
  ] });
  let state = initialState;
  let reads = 0;
  const client = new TranscriptSnapshotClient({ ...transport, snapshot: async () => snapshot,
    content: async (_, ref) => { reads++; const text = ref.path[0] === "content" ? output : args;
      return { data: text, nextOffset: text.length, done: true, stale: false }; } }, new TurnEventProjector(quietTransport));
  await client.load("tools", snapshot => { state = reducer(state, { type: "transcript_snapshot", snapshot }); });
  const original = state.items.find(item => item.id === "tool-full");
  for (let attempt = 0; attempt < 2; attempt++) {
    const value = JSON.parse((await resolveSnapshotTool(client, "tools", "tool-full", () => state))!);
    assert.equal(value.args, args);
    assert.equal(value.output, output);
    assert.equal(state.items.find(item => item.id === "tool-full"), original, "details do not expand or replace source Items");
  }
  assert.equal(reads, referenced ? 4 : 0, "reopening rereads only this tool's references, without resolving other calls or assistant content");
  client.release("tools");
}
// Independent fields must survive both immediate local commits and batched
// remote React commits, regardless of the other field changing Item identity.
for (const batched of [false, true]) {
  const body = deferred<import("../lib/transcriptProtocol").TranscriptContentChunk>();
  const thought = deferred<import("../lib/transcriptProtocol").TranscriptContentChunk>();
  const snapshot = cut({ activeAttempts: [], runtime: { pendingEvents: [] }, records: [{ id: "m:parallel", order: 0,
    message: { role: "assistant", messageId: "parallel", content: "body preview", reasoning: "thought preview" },
    refs: ["content", "reasoning"].map(field => ({ snapshotId: "cut-1", recordId: "m:parallel", path: [field], bytes: 4 })) }] });
  let state = initialState;
  const queued: Array<() => void> = [];
  const client = new TranscriptSnapshotClient({ ...transport, snapshot: async () => snapshot,
    content: async (_, ref) => ref.path[0] === "content" ? body.promise : thought.promise }, new TurnEventProjector(quietTransport));
  await client.load("parallel", snapshot => { state = reducer(state, { type: "transcript_snapshot", snapshot }); });
  const read = (field: string) => resolveSnapshotItems(client, "parallel", "m:parallel", () => state, historyMessagesToItems, patches => {
    const commit = () => { state = reducer(state, { type: "history_items_patch", patches: rebaseSnapshotContentPatches(state, patches, field) }); };
    if (batched) queued.push(commit); else commit();
  }, field);
  const bodyRead = read("content"), thoughtRead = read("reasoning");
  body.resolve({ data: "body", nextOffset: 4, done: true, stale: false }); await bodyRead;
  thought.resolve({ data: "mind", nextOffset: 4, done: true, stale: false }); await thoughtRead;
  queued.forEach(commit => commit());
  assert.ok(state.items[0].kind === "assistant" && state.items[0].text === "body" && state.items[0].reasoning === "mind",
    batched ? "remote commits retain both complete fields" : "local field resolution survives an unrelated Item update");
  client.release("parallel");
}
console.log("transcript snapshot client races, independent concurrent fields and complete tool details: ok");
