import assert from "node:assert/strict";
import { summarizeProjectTreeSessions } from "../lib/projectTreeDiagnostics";
import type { ProjectNode } from "../lib/types";

const topic = (id: string, extra: Partial<ProjectNode> = {}): ProjectNode => ({
  key: `topic-${id}`,
  kind: "topic",
  label: id,
  topicId: id,
  ...extra,
});

const tree: ProjectNode[] = [{
  key: "project-a",
  kind: "project",
  label: "Project A",
  root: "private-root",
  children: [
    topic("one"),
    topic("two"),
    topic("three"),
    topic("four"),
    topic("five"),
    topic("six", { runtimeOnly: true, recoveryCopyCount: 2, running: true }),
  ],
}, {
  key: "global",
  kind: "global_folder",
  label: "Global",
  children: [topic("global-one", { kind: "global_topic", topicId: "global-one", pinned: true })],
}];

const summary = summarizeProjectTreeSessions({
  tree,
  visibleTree: tree,
  expanded: new Set(["project-a"]),
  showAllTopics: new Set(),
  classicTruncationActive: true,
  queryActive: false,
  timeFilterActive: false,
  projectNodeKey: (node) => node.key,
  isActive: (node) => node.topicId === "one",
  isUnread: (node) => node.topicId === "two",
});

assert.equal(summary.workspaceSessions, 7);
assert.equal(summary.visibleSessions, 5, "classic preview plus collapsed global folder hides two rows");
assert.equal(summary.hiddenSessions, 2);
assert.equal(summary.hiddenByTruncation, 1);
assert.equal(summary.hiddenByCollapsed, 1);
assert.equal(summary.runtimeOnlySessions, 1);
assert.equal(summary.recoveryCopySessions, 1);
assert.equal(summary.recoveryCopies, 2);
assert.equal(summary.runningSessions, 1);
assert.equal(summary.unreadSessions, 1);
assert.equal(summary.pinnedSessions, 1);
assert.equal(summary.activeSessions, 1);
assert.equal(summary.activeVisibleSessions, 1);

const filtered = summarizeProjectTreeSessions({
  tree,
  visibleTree: [{ ...tree[0], children: [tree[0].children?.[0] ?? topic("one")] }],
  expanded: new Set(["project-a"]),
  showAllTopics: new Set(["project-a"]),
  classicTruncationActive: false,
  queryActive: true,
  timeFilterActive: false,
  projectNodeKey: (node) => node.key,
});
assert.equal(filtered.workspaceSessions, 7);
assert.equal(filtered.visibleSessions, 1);
assert.equal(filtered.hiddenByFilter, 6);
assert.equal(filtered.hiddenByCollapsed, 0);
assert.equal(filtered.hiddenByTruncation, 0);

console.log("project tree diagnostics tests passed");
