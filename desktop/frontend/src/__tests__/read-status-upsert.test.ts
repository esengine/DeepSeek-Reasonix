import { initialState, reducer } from "../lib/useController";
import { applyReadStatusFrame, readStatusLabel } from "../lib/readStatus";
import { readPauseItem, upsertReadPause, type WireReadPause } from "../lib/readPause";

function equal(actual: unknown, expected: unknown, message: string) {
  if (actual !== expected) throw new Error(`${message}: got ${String(actual)}, want ${String(expected)}`);
}

const frame = (seq: number, state: string, extra: Record<string, unknown> = {}) => ({
  kind: "read_status" as const,
  turnId: "turn-1",
  readStatus: {
    readId: "ir-1", seq, path: "/w/a.go", state, active: true,
    covered: [[1, 10]] as [number, number][],
    ...extra,
  },
});

let s = reducer(initialState, { type: "event", e: frame(1, "needs_more") });
equal(Object.keys(s.readStatuses ?? {}).length, 1, "one logical read keeps one status entry");
s = reducer(s, { type: "event", e: frame(2, "needs_more") });
equal(Object.keys(s.readStatuses ?? {}).length, 1, "a later page upserts the same entry");
equal(s.readStatuses?.["ir-1"]?.seq, 2, "the newest sequence wins");

const stale = reducer(s, { type: "event", e: frame(1, "satisfied", { active: false }) });
equal(stale.readStatuses?.["ir-1"]?.state, "needs_more", "a re-ordered frame never moves a read backwards");

s = reducer(s, {
  type: "event",
  e: { kind: "read_status", turnId: "turn-1", readStatus: { readId: "ir-2", seq: 1, path: "/w/b.go", state: "needs_more", active: true } },
});
equal(Object.keys(s.readStatuses ?? {}).length, 2, "independent reads keep independent entries");

const next = reducer(s, { type: "event", e: { kind: "turn_started", turnId: "turn-2", status: "in_progress" } });
equal(next.readStatuses, undefined, "a new turn starts from no live read status");

// A hundred pages of one read still leave exactly one status entry.
let many = initialState;
for (let page = 1; page <= 100; page++) {
  many = reducer(many, { type: "event", e: frame(page, "needs_more", { covered: [[1, page * 10]] as [number, number][] }) });
}
equal(Object.keys(many.readStatuses ?? {}).length, 1, "a hundred pages still render one status");
equal(many.readStatuses?.["ir-1"]?.seq, 100, "the latest page wins");

console.log("read status upsert tests passed");

const current = { readId: "r", generation: 2, seq: 1, path: "a.go", state: "needs_more", active: true };
const scoped = { readStatuses: { r: current } };
equal(applyReadStatusFrame(scoped, { ...current, generation: 1, seq: 99 }).readStatuses.r.generation, 2, "old generation cannot overwrite new state");
equal(applyReadStatusFrame(scoped, { ...current, generation: 3, seq: 0 }).readStatuses.r.generation, 3, "new generation may restart its sequence");
const label = readStatusLabel({ r: { ...current, hasMore: true, covered: [[0, 10], [100, 110]] } }, (_key, vars) => String(vars?.range));
equal(label, "1–10, 101–110", "coverage gaps are not displayed as read");
const paused = readStatusLabel({ r: { ...current, state: "blocked", reason: "no_progress" } }, (key) => key);
equal(paused.includes("composer.readStatusStalled"), true, "pause explains the cause");
equal(paused.includes("composer.readStatusRecovery"), true, "pause gives an action");
const started = reducer(initialState, { type: "event", e: { kind: "turn_started", turnId: "current", status: "in_progress" } });
const live = reducer(started, { type: "event", e: { ...frame(1, "needs_more"), turnId: "current" } });
equal(reducer(live, { type: "event", e: { ...frame(99, "blocked"), turnId: "old" } }).readStatuses?.["ir-1"].seq, 1, "another turn cannot update read status");
const done = reducer(live, { type: "event", e: { kind: "turn_done", turnId: "current" } });
equal(done.readStatuses, undefined, "completion clears live status");
equal(reducer(done, { type: "event", e: { ...frame(99, "blocked"), turnId: "current" } }).readStatuses, undefined, "late result cannot revive a completed turn");

const receipt: WireReadPause = { id: "receipt", reads: [{ readId: "r", path: "file", reason: "page_budget", covered: [[0, 10]], missing: [[10, 20]] }] };
const readDone = reducer(live, { type: "event", e: { kind: "turn_done", turnId: "current", outcome: "incomplete_read", readPause: receipt } });
equal(readDone.turnActive, false, "a read pause releases the composer");
equal(readDone.readStatuses, undefined, "live progress is cleared after retaining the receipt");
equal(readDone.items.filter(it => it.kind === "notice" && it.code === "incomplete_read").length, 1, "one durable read pause");
equal(upsertReadPause(readDone.items, receipt, "unused").length, readDone.items.length, "replayed pause is idempotent");
const historyNotice = readPauseItem(receipt, "unused");
equal(historyNotice.kind === "notice" && historyNotice.text.includes("budget"), true, "pause cause is visible without expanding details");
equal(JSON.stringify(readDone.items.find(it => it.id === historyNotice.id)), JSON.stringify(historyNotice), "live and history use identical presentation");
const nextTurn = reducer(readDone, { type: "event", e: { kind: "turn_started", turnId: "next", status: "in_progress" } });
equal(reducer(nextTurn, { type: "event", e: { kind: "turn_done", turnId: "current", outcome: "incomplete_read", readPause: receipt } }), nextTurn, "stale terminal result cannot stop a new turn");
