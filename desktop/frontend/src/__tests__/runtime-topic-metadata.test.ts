import assert from "node:assert/strict";
import { createProjectTreeRuntimeProjection, projectTreeApplyRuntimeTopics } from "../lib/projectTreeRuntime";
import type { ProjectNode, ProjectRuntimeTopic } from "../lib/types";

const children: ProjectNode[] = ["first", "second"].map(id => ({ key: id, kind: "session", label: `Title ${id}`,
  root: "/repo", topicId: "topic", sessionPath: `/sessions/${id}`, preview: `Preview ${id}`, children: [] }));
const topic: ProjectNode = { key: "topic", kind: "topic", label: "Saved title", root: "/repo", topicId: "topic", children };
const tree: ProjectNode[] = [{ key: "project", kind: "project", label: "Project", root: "/repo", children: [topic] }];
const runtime: ProjectRuntimeTopic[] = [{ scope: "project", workspaceRoot: "/repo", node: { ...topic, label: "Runtime title",
  children: children.map(child => ({ ...child, label: "Path fallback", preview: "", running: true, status: "thinking" })) } }];
const next = projectTreeApplyRuntimeTopics(tree, runtime);
assert.equal(next[0].children![0].label, "Saved title");
for (const child of next[0].children![0].children!) {
  assert.equal(child.label, `Title ${child.key}`);
  assert.equal(child.preview, `Preview ${child.key}`);
  assert.equal(child.running, true);
}
assert.equal(projectTreeApplyRuntimeTopics(next, runtime), next, "identical runtime overlay preserves tree identity");
const projection = createProjectTreeRuntimeProjection();
projection.apply(tree, runtime);
const pagedAway = projection.apply([{ ...tree[0], children: [] }], runtime);
assert.equal(pagedAway[0].children![0].children![0].preview, "Preview first", "resident catalog metadata survives paging");
console.log("PASS: memory-only runtime topics preserve catalog metadata and structural sharing");
