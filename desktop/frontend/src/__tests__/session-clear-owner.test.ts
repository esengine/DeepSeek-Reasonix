import assert from "node:assert/strict";
import { executeClearSession } from "../app-runtime/sessionActionOwner";

const target = { tabId: "A", sessionKey: "A:1" };
let owned = true;
const authority = { checkpoint() { if (!owned) throw new Error("stale"); }, ownsUI: () => owned };
const calls: string[] = [];
const ports = {
  clearSession: async () => { calls.push("local"); },
  clearRemoteSession: async (tabId: string) => { calls.push(`remote:${tabId}`); },
  retryRemoteHydration: async () => { calls.push("hydrate"); },
};

await executeClearSession(target, { remote: false }, ports, authority);
assert.deepEqual(calls, ["local"]);
calls.length = 0;
await executeClearSession(target, { remote: true }, ports, authority);
assert.deepEqual(calls, ["remote:A", "hydrate"]);
calls.length = 0;
owned = false;
await assert.rejects(executeClearSession(target, { remote: false }, ports, authority), /stale/);
assert.deepEqual(calls, [], "stale source cannot clear a replacement session");
console.log("session clear owner: source/UI ownership and local/remote paths passed");
