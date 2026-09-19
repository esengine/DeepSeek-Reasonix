import assert from "node:assert/strict";
import { sessionIdentityKey } from "../app-runtime/sessionTarget";
import { hasSessionGeneration, sameSessionIdentity, sessionIdentityStableKey } from "../lib/sessionIdentity";

const a = { session: { hostId: "local", sessionId: "canonical-a" }, sessionPath: "", sessionGeneration: 1 };
const b = { session: { hostId: "local", sessionId: "canonical-b" }, sessionPath: "", sessionGeneration: 1 };

assert.equal(hasSessionGeneration(0), true, "a newly bound host session starts at zero");
assert.equal(hasSessionGeneration(1), true);
for (const invalid of [undefined, null, -1, 0.5, NaN, Infinity, "0", Number.MAX_SAFE_INTEGER + 1]) {
  assert.equal(hasSessionGeneration(invalid), false, `invalid generation ${String(invalid)}`);
}

assert.notEqual(sessionIdentityStableKey(a), sessionIdentityStableKey(b), "empty-path canonical sessions own different cache keys");
assert.equal(sameSessionIdentity(a, { ...a }), true, "the same SessionRef is stable across snapshots");
assert.equal(sameSessionIdentity(a, b), false, "SessionRef prevents canonical transcript cross-wiring");
assert.equal(
  sessionIdentityKey({ ...a, tabId: "reused-tab", scope: "project", workspaceRoot: "/repo", topicId: "legacy-topic" }),
  sessionIdentityStableKey(a),
  "canonical SessionRef outranks reused tab, topic and compatibility-path identities",
);

console.log("session identity: canonical SessionRef owns caches, navigation and hydration fences");
