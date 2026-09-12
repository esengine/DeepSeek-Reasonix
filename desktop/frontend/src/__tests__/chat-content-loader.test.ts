import assert from "node:assert/strict";
import { ChatContentLoader } from "../lib/chatContentLoader";
import type { Item } from "../lib/useController";
const waits: Array<(text: string) => void> = [];
let count = 0, peak = 0;
const loader = new ChatContentLoader(undefined, async () => {
  peak = Math.max(peak, ++count);
  const result = await new Promise<string>(resolve => waits.push(resolve)); count--; return result;
});
const item = (index: number): Item => ({ kind: "assistant", id: `a${index}`, text: "preview", reasoning: "thought", streaming: false });
const requests = Array.from({ length: 8 }, (_, index) => loader.load(item(index), "content"));
assert.equal(loader.load(item(0), "content"), requests[0], "identical requests share a promise");
assert.equal(waits.length, 4, "only four requests start");
for (let index = 0; index < 8; index++) { waits[index](String(index)); await requests[index]; await new Promise(resolve => setImmediate(resolve)); }
assert.equal(peak, 4);
const stale = loader.load(item(0), "reasoning");
const rejection = assert.rejects(stale, /closed/);
loader.dispose(); loader.activate();
const fresh = loader.load(item(0), "reasoning");
waits[8]("old"); await rejection;
assert.equal(loader.load(item(0), "reasoning"), fresh, "old cleanup cannot remove a new generation request");
waits[9]("new"); assert.equal(await fresh, "new");
const oldRevision = loader.load(item(1), "content");
const newRevision = loader.load({ ...item(1), text: "changed" } as Item, "content");
assert.notEqual(newRevision, oldRevision, "different source revisions cannot reuse the old content request");
waits[10]("old content"); waits[11]("new content");
assert.equal(await oldRevision, "old content"); assert.equal(await newRevision, "new content");
loader.dispose();
console.log("chat content: concurrency, deduplication, generation and cleanup races passed");
