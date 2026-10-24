import assert from "node:assert/strict";
import { test } from "node:test";
import { createRemoteHydrationOwner } from "../lib/remoteHydrationOwner";

test("a failed baseline can retry the same revision and old completion cannot release the newer owner", async () => {
  const requests: Array<(success: boolean) => void> = [];
  const owner = createRemoteHydrationOwner(() => new Promise(resolve => requests.push(resolve)));
  const first = owner.load(1);
  assert.equal(owner.load(1), first);
  requests.shift()!(false);
  await first;
  const retry = owner.load(1);
  const replacement = owner.load(2);
  requests.shift()!(true);
  await retry;
  assert.equal(owner.load(2), replacement);
  requests.shift()!(true);
  await replacement;
  await owner.load(1);
  await owner.load(2);
  assert.equal(requests.length, 0);
  const manual = owner.load();
  assert.equal(requests.length, 1, "explicit retry remains available for an installed revision");
  requests.shift()!(true);
  await manual;
});

test("disconnect fences an old baseline and reconnect owns a new revision", async () => {
  const requests: Array<(success: boolean) => void> = [];
  const owner = createRemoteHydrationOwner(() => new Promise(resolve => requests.push(resolve)));
  const old = owner.load(1);
  assert.equal(owner.observe(2), true);
  assert.equal(owner.observe(1), false);
  requests.shift()!(true);
  await old;
  const next = owner.load(3);
  assert.equal(requests.length, 1);
  requests.shift()!(true);
  await next;
});
