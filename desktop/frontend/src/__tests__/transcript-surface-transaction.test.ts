import assert from "node:assert/strict";
import { createTranscriptSurfaceTransactions } from "../lib/transcriptSurfaceTransaction";

const events: Array<{ type: string; fields: Record<string, unknown> }> = [];
const previous = (globalThis as { window?: Window }).window;
const windowStub = {
  __REASONIX_TRANSCRIPT_SCROLL_DIAGNOSTIC__: (type: string, fields: Record<string, unknown>) => events.push({ type, fields }),
} as unknown as Window;
(globalThis as { window?: Window }).window = windowStub;

const transactions = createTranscriptSurfaceTransactions();
const first = transactions.begin({
  kind: "reader-prepend",
  surfaceGeneration: 3,
  ownershipEpoch: 8,
  geometryRevision: 13,
  mutationSeq: 21,
  anchor: { rowKey: "row-a", logicalIndex: 44, viewportOffset: 16 },
});
assert.equal(first.token, 1);
assert.equal(transactions.isCurrent(first.token), true);
assert.equal(transactions.update(first.token, { phase: "mutating", mutationSeq: 22 }, "prepend"), true);
assert.equal(transactions.update(first.token - 1, { phase: "settling" }), false);
assert.equal(transactions.finish(first.token - 1, "committed"), false);
assert.equal(transactions.finish(first.token, "committed"), true);
assert.equal(transactions.current(), null);
assert.deepEqual(events.map((event) => event.fields.result), ["begin", "prepend", "committed"]);
assert.equal(events[0]?.fields.anchorIndex, 44);
assert.equal(events[0]?.fields.anchorOffset, 16);

(globalThis as { window?: Window }).window = previous;
console.log("transcript surface transaction tests passed");
