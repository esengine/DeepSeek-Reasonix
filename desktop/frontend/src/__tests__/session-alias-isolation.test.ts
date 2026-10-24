import assert from "node:assert/strict";
import { test } from "node:test";
import { sameProjectSession, projectSessionExcluded, projectSessionIdentity } from "../lib/projectSessionIdentity";
import { mergeProjectTopicPage, mergeIncompleteProjectTopicPage } from "../lib/projectTreeTopic";
import { createSessionLifecycleFences } from "../lib/sessionLifecycleFences";
import { projectTreeApplyRuntimeTopics } from "../lib/projectTreeRuntime";
import type { ProjectNode } from "../lib/types";

const alias = "path\0/shared.jsonl";
const row = (id: string, hostId = "local"): ProjectNode => ({ key: id, kind: "topic", label: id,
  session: { hostId, sessionId: id }, identityAliases: [alias], lifecycleGeneration: 10 });
const a = row("original"), b = row("reviewed");
const source: ProjectNode = { key: "source", kind: "topic", label: "Source", sessionPath: "/shared.jsonl" };
const ids = (rows: ProjectNode[]) => rows.map(projectSessionIdentity);

test("canonical identities survive colliding source aliases across pages and hosts", () => {
  for (const sibling of [b, row("original", "other-host")]) {
    assert.equal(sameProjectSession(a, sibling), false);
    assert.deepEqual(ids(mergeProjectTopicPage([a], [sibling], true)), ids([a, sibling]));
    assert.deepEqual(ids(mergeIncompleteProjectTopicPage([a], [sibling])), ids([a, sibling]));
    assert.deepEqual(ids(mergeProjectTopicPage([{ ...a, pinned: true }], [sibling], false)), ids([sibling, a]));
  }
});

test("unique source adoption survives stale pages but ambiguous sources do not pick an owner", () => {
  assert.deepEqual(ids(mergeProjectTopicPage([source], [a], true)), ids([a]));
  assert.deepEqual(mergeProjectTopicPage([a], [source], true), [a]);
  assert.deepEqual(ids(mergeProjectTopicPage([a, b], [source], true)), ids([a, b, source]));
  assert.deepEqual(ids(mergeProjectTopicPage([], [source, a], false)), ids([a]));
});

test("runtime projection cannot attach a shared source to an arbitrary canonical owner", () => {
  const tree: ProjectNode[] = [{ key: "p", kind: "project", root: "/repo", label: "Project", children: [a, b, source] }];
  const projected = projectTreeApplyRuntimeTopics(tree, [
    { scope: "project", workspaceRoot: "/repo", node: { ...a, running: true } },
    { scope: "project", workspaceRoot: "/repo", node: { ...b, running: false } },
  ])[0].children!;
  assert.deepEqual(ids(projected), ids([a, b, source]));
  assert.deepEqual(projected.map(node => node.running), [true, false, undefined]);
});

test("archiving and restoring one owner cannot hide or restore a different owner", () => {
  const fences = createSessionLifecycleFences();
  fences.archive(a, { lifecycleGeneration: 10, operationId: "archive-a" });
  assert.equal(projectSessionExcluded(a, fences.keys()), true);
  assert.equal(projectSessionExcluded(b, fences.keys()), false);
  assert.equal(projectSessionExcluded(source, fences.keys()), true);
  fences.observeDirectory([{ ...b, lifecycleGeneration: 999 }, { ...source, lifecycleGeneration: 999 }]);
  assert.equal(projectSessionExcluded(a, fences.keys()), true);
  fences.archive(b, { lifecycleGeneration: 20, operationId: "archive-b" });
  fences.observeDirectory([{ ...a, lifecycleGeneration: 11 }]);
  assert.equal(projectSessionExcluded(a, fences.keys()), false);
  assert.equal(projectSessionExcluded(b, fences.keys()), true);
  assert.equal(projectSessionExcluded(source, fences.keys()), true, "another archive still owns the shared alias");
  fences.observeDirectory([{ ...b, lifecycleGeneration: 21 }]);
  assert.equal(fences.keys().size, 0);
});

test("source archive receipts retain independent canonical owners through restoration", () => {
  const fences = createSessionLifecycleFences();
  fences.archive(source, { lifecycleGeneration: 10, operationId: "archive-source",
    identityAliases: [projectSessionIdentity(a), projectSessionIdentity(b)] });
  fences.observeDirectory([{ ...row("unrelated"), lifecycleGeneration: 999 }]);
  assert.equal(projectSessionExcluded(a, fences.keys()), true);
  assert.equal(projectSessionExcluded(b, fences.keys()), true);
  fences.observeDirectory([{ ...a, lifecycleGeneration: 11 }]);
  assert.equal(projectSessionExcluded(a, fences.keys()), false);
  assert.equal(projectSessionExcluded(b, fences.keys()), true);
  assert.equal(projectSessionExcluded(source, fences.keys()), true);
});
