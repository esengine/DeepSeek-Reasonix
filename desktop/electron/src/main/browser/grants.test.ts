import assert from "node:assert/strict";
import { test } from "node:test";
import { RpcError } from "../rpc.js";
import { BROWSER_ERR_NO_GRANT } from "./errors.js";
import { GrantRegistry } from "./grants.js";

const noGrant = (error: unknown) => error instanceof RpcError && error.code === BROWSER_ERR_NO_GRANT;

test("grants are verified against the task and the service generation", () => {
  let generation = "g-1";
  const revoked: string[] = [];
  const grants = new GrantRegistry({ generation: () => generation, now: () => 5, onRevoked: (grant) => revoked.push(grant.grantId) });
  assert.throws(() => grants.verify("grant-a"), noGrant);
  const grant = grants.install({ grantId: "grant-a", taskId: "task-a", sessionId: "s" });
  assert.deepEqual(grant, { grantId: "grant-a", taskId: "task-a", sessionId: "s", generation: "g-1", createdAt: 5 });
  assert.equal(grants.verify("grant-a").taskId, "task-a");
  assert.equal(grants.verifyTab("grant-a", "task-a").grantId, "grant-a");
  assert.throws(() => grants.verifyTab("grant-a", "task-b"), noGrant);
  assert.throws(() => grants.verifyTab("grant-a", undefined), noGrant);
  assert.throws(() => grants.install({ grantId: "", taskId: "t", sessionId: "" }), noGrant);

  generation = "g-2";
  assert.throws(() => grants.verify("grant-a"), noGrant);
  assert.deepEqual(revoked, ["grant-a"]);
  assert.equal(grants.size, 0);
});

test("revoke removes the grant and a generation change revokes every grant", () => {
  let generation = "g-1";
  const revoked: string[] = [];
  const grants = new GrantRegistry({ generation: () => generation, onRevoked: (grant) => revoked.push(grant.grantId) });
  grants.install({ grantId: "a", taskId: "ta", sessionId: "" });
  grants.install({ grantId: "b", taskId: "tb", sessionId: "" });
  assert.equal(grants.revoke("a")?.taskId, "ta");
  assert.equal(grants.revoke("a"), null);
  assert.throws(() => grants.verify("a"), noGrant);
  grants.observeGeneration("g-1");
  assert.equal(grants.size, 1, "the same generation changes nothing");
  generation = "";
  grants.observeGeneration("");
  assert.equal(grants.size, 0);
  assert.deepEqual(revoked, ["a", "b"]);
  assert.throws(() => grants.install({ grantId: "c", taskId: "t", sessionId: "" }), noGrant);
});
