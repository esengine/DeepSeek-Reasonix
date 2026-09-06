import assert from "node:assert/strict";
import { executeTerminalOutputInsertion } from "../app-runtime/sessionRuntimeOwner";

const target = { tabId: "A", sessionKey: "A:1" };
let owned = true;
const authority = { checkpoint() { if (!owned) throw new Error("stale"); }, ownsUI: () => owned };
const calls: string[] = [];
const inserted = await executeTerminalOutputInsertion(target, "term-1", {
  read: async (tabId, sessionId) => { calls.push(`read:${tabId}:${sessionId}`); return "output"; },
  apply: (text) => calls.push(`apply:${text}`),
}, (value) => value.toUpperCase(), authority);
assert.equal(inserted, true);
assert.deepEqual(calls, ["read:A:term-1", "apply:OUTPUT"]);
calls.length = 0;
owned = false;
await assert.rejects(executeTerminalOutputInsertion(target, "term-2", {
  read: async () => "stale-output",
  apply: () => calls.push("apply"),
}, value => value, authority), /stale/);
assert.deepEqual(calls, [], "stale source cannot insert terminal output");
console.log("terminal output owner: source/UI ownership passed");
