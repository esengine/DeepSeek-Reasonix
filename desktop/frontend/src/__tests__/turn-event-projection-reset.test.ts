import assert from "node:assert/strict";
import type { AppBindings } from "../lib/bridge";
import type { TurnEventReplayView } from "../lib/types";

let resetEntered!: () => void;
const resetStarted = new Promise<void>((resolve) => { resetEntered = resolve; });
let releaseReset!: () => void;
const resetReleased = new Promise<void>((resolve) => { releaseReset = resolve; });

const replay: TurnEventReplayView = {
  events: [
    {
      turnId: "turn-reset",
      seq: 11,
      status: "in_progress",
      event: { kind: "turn_started", turnId: "turn-reset", status: "in_progress" },
    },
    {
      turnId: "turn-reset",
      seq: 12,
      status: "in_progress",
      event: { kind: "text", turnId: "turn-reset", status: "in_progress", text: "durable" },
    },
  ],
  floorSeq: 11,
  latestSeq: 12,
  nextAfterSeq: 12,
  hasMore: false,
  resetRequired: true,
  transcriptRevision: 7,
  transcriptDigest: "digest-7",
  runtimeEpoch: "epoch-a",
};

const binding: Partial<AppBindings> = {
  TurnEventsForTab: async () => replay,
};
Object.defineProperty(globalThis, "window", {
  configurable: true,
  value: { go: { main: { App: binding as AppBindings } } } as Window,
});

const [{ TurnEventProjector }, { initialState, reducer }] = await Promise.all([
  import("../lib/turnEventProjection"),
  import("../lib/useController"),
]);

const projected: number[] = [];
const projector = new TurnEventProjector();
projector.bind((event) => projected.push(event.seq ?? 0));
projector.bindReset(async (_tabId, view) => {
  assert.equal(view.transcriptRevision, 7);
  resetEntered();
  await resetReleased;
  return true;
});
projector.observeRuntime("tab", "epoch-a", 1, 1, true);
assert.equal(projector.acceptLive("tab", { kind: "turn_status", seq: 3, runtimeEpoch: "epoch-a" }, "epoch-a"), false);
await resetStarted;
assert.equal(projector.acceptLive("tab", { kind: "turn_done", seq: 13, runtimeEpoch: "epoch-a", status: "completed" }, "epoch-a"), false);
releaseReset();
for (let attempt = 0; attempt < 40; attempt += 1) await Promise.resolve();
assert.deepEqual(projected, [11, 12, 13], "checkpoint replay is projected before the queued live tail");

let resolveStaleReplay!: (view: TurnEventReplayView) => void;
binding.TurnEventsForTab = async () => new Promise<TurnEventReplayView>((resolve) => { resolveStaleReplay = resolve; });
const staleProjection: number[] = [];
const releasedProjector = new TurnEventProjector();
releasedProjector.bind((event) => staleProjection.push(event.seq ?? 0));
releasedProjector.observeRuntime("released-tab", "epoch-old", 1, 1, true);
releasedProjector.acceptLive("released-tab", { kind: "turn_status", seq: 3, runtimeEpoch: "epoch-old" }, "epoch-old");
await Promise.resolve();
releasedProjector.release("released-tab");
resolveStaleReplay({ ...replay, resetRequired: false, floorSeq: 2, events: [replay.events[1]] });
for (let attempt = 0; attempt < 20; attempt += 1) await Promise.resolve();
assert.deepEqual(staleProjection, [], "a replay response that returns after tab release is discarded");

const persistedAssistant = { kind: "assistant", id: "assistant-live", text: "partial", reasoning: "", streaming: false } as const;
const persistedUser = { kind: "user", id: "persisted-user-live", text: "persisted question" } as const;
const optimisticUser = { kind: "user", id: "optimistic-user", text: "new question" } as const;
const oldHistory = { kind: "user", id: "history-old", text: "old question" } as const;
const state = {
  ...initialState,
  items: [oldHistory, persistedUser, persistedAssistant, optimisticUser],
  historyPrefixCount: 1,
  historyRevision: 6,
};
const rebased = reducer(state, {
  type: "history_rebase",
  items: [
    { kind: "user", id: "history-new", text: "newest persisted question" },
    { kind: "user", id: "persisted-user-durable", text: "persisted question" },
    persistedAssistant,
  ],
  startTurn: 5,
  totalTurns: 7,
  hasOlder: true,
  revision: 7,
  digest: "digest-7",
});
assert.equal(rebased.historyPrefixCount, 3);
assert.equal(rebased.items.filter((item) => item.id === "assistant-live").length, 1, "transcript/live overlap is deduplicated");
assert.equal(rebased.items.find((item) => item.id === "optimistic-user"), optimisticUser, "optimistic user item identity survives rebase");
assert.equal(rebased.historyLayoutRevision, initialState.historyLayoutRevision + 1);

const older = reducer(rebased, {
  type: "history_rebase",
  items: [{ kind: "user", id: "stale", text: "stale" }],
  startTurn: 0,
  totalTurns: 1,
  hasOlder: false,
  revision: 6,
});
assert.equal(older, rebased, "an older transcript revision cannot replace the current projection");

const monotonicProjector = new TurnEventProjector();
monotonicProjector.observeRuntime("monotonic-tab", "epoch-stable", 12, 12, false);
assert.equal(
  monotonicProjector.acceptLive("monotonic-tab", { kind: "turn_started", seq: 1, runtimeEpoch: "epoch-stable" }, "epoch-stable"),
  false,
  "a stale seq=1 event cannot reset a ledger cursor whose sequence is defined to be monotonic",
);

console.log("turn event projection reset tests passed");
