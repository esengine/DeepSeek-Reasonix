import assert from "node:assert/strict";
import { executeCancelRuntimeJob } from "../app-runtime/sessionRuntimeOwner";

const target = { tabId: "A", sessionKey: "A:1" };
let owned = true;
const authority = { checkpoint() { if (!owned) throw new Error("stale"); }, ownsUI: () => owned };
const calls: string[] = [];
const result = await executeCancelRuntimeJob(target, "job-1", {
  cancelForTab: async (tabId, jobId) => { calls.push(`cancel:${tabId}:${jobId}`); return true; },
  refresh: async () => { calls.push("refresh"); },
}, authority);
assert.equal(result, true);
assert.deepEqual(calls, ["cancel:A:job-1", "refresh"]);
calls.length = 0;
owned = false;
await assert.rejects(executeCancelRuntimeJob(target, "job-2", {
  cancelForTab: async () => { calls.push("cancel"); return true; },
  refresh: async () => { calls.push("refresh"); },
}, authority), /stale/);
assert.deepEqual(calls, [], "stale source cannot cancel a replacement session");
console.log("runtime job owner: source/UI ownership passed");
