import assert from "node:assert/strict";
import { test } from "node:test";
import { mergeRemoteSessionsIntoTree } from "../components/ProjectTreeRemoteGroups";
import { markSessionRead, mergeReadStores, readActivityValues, seedSessionReads, type ReadStore } from "../lib/sessionReadActivity";
import { projectTreeReadActivityKey, projectTreeTopicHasUnreadActivity } from "../lib/projectTreeTopic";
import type { ProjectNode } from "../lib/types";

const empty = (): ReadStore => ({ version: 3, baselineAt: 5000, records: {} });
function remote(sequence: number | undefined, ready = true, hostId = "host"): ProjectNode {
  const row = { name: "a", sessionId: "a", title: "A", turns: 1, lastActivityAt: 1000, resultSequence: sequence, metadataReady: ready };
  return mergeRemoteSessionsIntoTree(
    [{ key: hostId, kind: "project", label: "Remote", remote: { hostId, workspace: "/repo" } }],
    { [`${hostId}\0/repo`]: [row] }, key => key,
  )[0].children![0];
}
const unread = (node: ProjectNode, store: ReadStore) => projectTreeTopicHasUnreadActivity(node, readActivityValues(store, [node]), undefined, undefined, undefined, undefined, store.baselineAt);

test("local and remote sessions use the same result-based read lifecycle", () => {
  const local: ProjectNode = { key: "local", kind: "topic", label: "A", session: { hostId: "local", sessionId: "a" }, turnsState: "ready", resultSequence: 3 };
  for (const first of [local, remote(3)]) {
    let store = seedSessionReads(empty(), [first]);
    const key = projectTreeReadActivityKey(first)!;
    assert.equal(store.records[key]?.metric, "result");
    assert.equal(unread(first, store), false);
    const next = { ...first, resultSequence: 6 };
    assert.equal(unread(next, store), true);
    assert.equal(unread({ ...next, running: true }, store), false);
    assert.equal(unread({ ...first, lastActivityAt: 99999, label: "Renamed" }, store), false);
    store = markSessionRead(store, next);
    assert.equal(store.records[key].value, 6);
    assert.equal(unread(next, store), false);
    assert.equal(markSessionRead(store, first), store, "late row cannot lower a read");
  }
});

test("zero is a ready baseline; pending metadata is never a read", () => {
  let store = seedSessionReads(empty(), [remote(0, false)]);
  assert.equal(Object.keys(store.records).length, 0);
  assert.equal(markSessionRead(store, remote(0, false)), store);
  store = seedSessionReads(store, [remote(0)]);
  assert.equal(store.records[projectTreeReadActivityKey(remote(0))!].value, 0);
  assert.equal(unread(remote(3), store), true);
});

test("legacy time records migrate only with ready authority and cannot downgrade", () => {
  const legacy = remote(undefined);
  const old = markSessionRead(empty(), legacy, 10000);
  const key = projectTreeReadActivityKey(legacy)!;
  assert.equal(seedSessionReads(old, [remote(0, false)]), old);
  const upgraded = seedSessionReads(old, [remote(3)]);
  assert.equal(upgraded.records[key].metric, "result");
  assert.equal(upgraded.records[key].value, 3);
  assert.equal(mergeReadStores(old, upgraded).records[key].value, 3);
  const read = markSessionRead(upgraded, remote(6));
  assert.equal(mergeReadStores(read, old).records[key].value, 6);
  assert.equal(markSessionRead(read, legacy), read);
  assert.equal(unread({ ...legacy, lastActivityAt: 99999 }, read), false, "old service cannot compare timestamps with a result count");
  assert.equal(unread(remote(9), read), true);
});

test("remote active identity suppresses blue dots without affecting another host", () => {
  const a = remote(3), other = remote(3, true, "other");
  const store = seedSessionReads(empty(), [a, other]);
  const activity = readActivityValues(store);
  const active = { hostId: "host", workspace: "/repo", sessionId: "a" };
  assert.equal(projectTreeTopicHasUnreadActivity({ ...a, resultSequence: 6 }, activity, undefined, undefined, undefined, undefined, 0, active), false);
  assert.equal(projectTreeTopicHasUnreadActivity({ ...other, resultSequence: 6 }, activity, undefined, undefined, undefined, undefined, 0, active), true);
});
