import assert from "node:assert/strict";
import { createProjectTreeRuntimeProjection, projectTreeApplyRuntimeTopics } from "../lib/projectTreeRuntime";
import type { ProjectNode } from "../lib/types";

const catalog: ProjectNode[] = [
  { key: "project-a", kind: "project", label: "A", root: "/a", children: [
    { key: "known", kind: "topic", label: "Known", root: "/a", topicId: "known", children: [] },
  ] },
  { key: "project-b", kind: "project", label: "B", root: "/b", children: [] },
];
const overlaid = projectTreeApplyRuntimeTopics(catalog, [
  { scope: "project", workspaceRoot: "/a", node: {
    key: "known", kind: "topic", label: "Known live", root: "/a", topicId: "known", running: true, status: "thinking", children: [
      { key: "known-a", kind: "session", label: "Session A", topicId: "known", sessionPath: "/a/a.jsonl" },
      { key: "known-b", kind: "session", label: "Session B", topicId: "known", sessionPath: "/a/b.jsonl" },
    ],
  } },
  { scope: "project", workspaceRoot: "/b", node: {
    key: "new", kind: "topic", label: "New live", root: "/b", topicId: "new", running: true, status: "streaming", children: [],
  } },
]);
const shape = (tree: ProjectNode[]) => tree.map((project) => project.children?.map((topic) => [
  topic.topicId, topic.running, topic.runtimeOnly,
]));
assert.deepEqual(shape(overlaid), [[['known', true, undefined]], [['new', true, true]]]);
assert.equal(overlaid[0]?.children?.[0]?.children?.length, 2);
assert.deepEqual(shape(projectTreeApplyRuntimeTopics(overlaid, [])), [[['known', undefined, undefined]], []]);
assert.equal(projectTreeApplyRuntimeTopics(overlaid, [
  { scope: "project", workspaceRoot: "/a", node: {
    key: "known", kind: "topic", label: "Known live", root: "/a", topicId: "known", running: true, children: [],
  } },
], new Set(["known"]))[0]?.children?.length, 0);
assert.equal(projectTreeApplyRuntimeTopics(overlaid, [])[0]?.children?.[0]?.children?.length, 0);

const projects: ProjectNode[] = Array.from({ length: 100 }, (_, index) => ({
  key: `p-${index}`, kind: "project", label: `P ${index}`, root: `/p/${index}`, children: [],
}));
const topics = projects.map((project, index) => ({
  scope: "project", workspaceRoot: project.root, node: {
    key: `t-${index}`, kind: "topic" as const, label: `T ${index}`, root: project.root,
    topicId: `t-${index}`, running: true, status: "thinking" as const, children: [],
  },
}));
const hundred = projectTreeApplyRuntimeTopics(projects, topics);
assert.equal(hundred.reduce((count, project) => count + (project.children?.filter((topic) => topic.running).length ?? 0), 0), 100);

const colliding = projectTreeApplyRuntimeTopics([
  { key: "collision-a", kind: "project", label: "A", root: "/collision/a", children: [] },
  { key: "collision-b", kind: "project", label: "B", root: "/collision/b", children: [] },
], [
  { scope: "project", workspaceRoot: "/collision/a", node: {
    key: "shared-a", kind: "topic", label: "Shared A", root: "/collision/a", topicId: "shared",
    sessionPath: "/collision/a/a.jsonl", running: true, children: [],
  } },
  { scope: "project", workspaceRoot: "/collision/b", node: {
    key: "shared-b", kind: "topic", label: "Shared B", root: "/collision/b", topicId: "shared",
    sessionPath: "/collision/b/b.jsonl", open: true, children: [],
  } },
]);
assert.deepEqual(
  colliding.map((project) => project.children?.map((topic) => [topic.label, topic.sessionPath, topic.running, topic.open])),
  [[['Shared A', '/collision/a/a.jsonl', true, undefined]], [['Shared B', '/collision/b/b.jsonl', undefined, true]]],
  "runtime rows with the same topic id stay inside their project scope",
);

const stableProjection = createProjectTreeRuntimeProjection();
const stableCatalog: ProjectNode[] = [{
  key: "stable-project", kind: "project", label: "Stable", root: "/stable", children: [
    { key: "topic-a", kind: "topic", label: "A", root: "/stable", topicId: "a", createdAt: 300, lastActivityAt: 600, children: [] },
    { key: "topic-b", kind: "topic", label: "B", root: "/stable", topicId: "b", createdAt: 200, lastActivityAt: 500, children: [] },
    { key: "topic-c", kind: "topic", label: "C", root: "/stable", topicId: "c", createdAt: 100, lastActivityAt: 400, children: [] },
  ],
}];
const runtimeB = [{
  scope: "project", workspaceRoot: "/stable", node: {
    key: "topic-b", kind: "topic" as const, label: "B", root: "/stable", topicId: "b",
    open: true, running: false, children: [],
  },
}];
const projected = stableProjection.apply(stableCatalog, runtimeB);
assert.strictEqual(projected[0]?.children?.[0], stableCatalog[0]?.children?.[0], "an unrelated row keeps its object identity");
assert.strictEqual(projected[0]?.children?.[2], stableCatalog[0]?.children?.[2], "every unrelated row keeps its object identity");
const repeated = stableProjection.apply(projected, structuredClone(runtimeB));
assert.strictEqual(repeated, projected, "an equivalent runtime snapshot is a tree-level no-op");

const transientCatalog: ProjectNode[] = [{
  ...stableCatalog[0],
  children: [stableCatalog[0]!.children![0]!, stableCatalog[0]!.children![2]!],
}];
const transient = stableProjection.apply(transientCatalog, structuredClone(runtimeB));
const restoredRuntimeB = transient[0]?.children?.find((topic) => topic.topicId === "b");
assert.equal(restoredRuntimeB?.runtimeOnly, true, "a catalog-lag row remains marked as runtime-only");
assert.equal(restoredRuntimeB?.createdAt, 200, "a catalog-lag row retains its resident creation time");
assert.equal(restoredRuntimeB?.lastActivityAt, 500, "a catalog-lag row retains its resident activity time");
assert.deepEqual(
  [...(transient[0]?.children ?? [])]
    .sort((a, b) => (b.lastActivityAt || b.createdAt || 0) - (a.lastActivityAt || a.createdAt || 0))
    .map((topic) => topic.topicId),
  ["a", "b", "c"],
  "catalog lag cannot move the active row to a zero-time sort position",
);

const pruningProjection = createProjectTreeRuntimeProjection();
const pruningCatalog: ProjectNode[] = [{
  key: "pruning-project", kind: "project", label: "Pruning", root: "/pruning", children: [
    { key: "stale", kind: "topic", label: "Stale metadata", root: "/pruning", topicId: "stale", createdAt: 10, children: [] },
  ],
}];
pruningProjection.apply(pruningCatalog, []);
const emptyPruningCatalog: ProjectNode[] = [{ ...pruningCatalog[0]!, children: [] }];
pruningProjection.apply(emptyPruningCatalog, []);
const afterPrune = pruningProjection.apply(emptyPruningCatalog, [{
  scope: "project", workspaceRoot: "/pruning", node: {
    key: "fresh", kind: "topic", label: "Fresh runtime", root: "/pruning", topicId: "stale", running: true, children: [],
  },
}]);
assert.equal(afterPrune[0]?.children?.[0]?.label, "Fresh runtime", "deleted resident metadata is not resurrected");
assert.equal(afterPrune[0]?.children?.[0]?.createdAt, undefined, "deleted resident timestamps are released");
console.log("  PASS  project tree runtime projection");
