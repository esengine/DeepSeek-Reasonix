import { asArray } from "./array";
import type { ProjectNode, ProjectRuntimeTopic, ProjectTreeRuntimeSnapshot } from "./types";

type RuntimeProjectNode = ProjectNode & { runtimeOnly?: boolean };
const noExcludedTopicIds: ReadonlySet<string> = new Set();

function withoutRuntimeState(node: ProjectNode): ProjectNode {
  return { ...node, open: undefined, running: undefined, status: undefined, children: [] };
}

// This replace-all overlay is loaded after the project tree mounts so runtime
// reconciliation does not enlarge the first-paint bundle. Subscription still
// precedes the initial snapshot read, so loading the module cannot lose an
// ownership transition.
export function projectTreeApplyRuntimeTopics(
  tree: ProjectNode[],
  topics: ProjectRuntimeTopic[],
  excludedTopicIds: ReadonlySet<string> = noExcludedTopicIds,
): ProjectNode[] {
  return tree.map((project) => {
    if (project.kind !== "project" && project.kind !== "global_folder") return project;
    const scope = project.kind === "project" ? "project" : "global";
    const base = asArray(project.children)
      .filter((node) => !(node as RuntimeProjectNode).runtimeOnly && !excludedTopicIds.has(node.topicId ?? ""))
      .map(withoutRuntimeState);
    const runtimeOnly: RuntimeProjectNode[] = [];
    for (const topic of topics) {
      if (!topic.node.topicId || excludedTopicIds.has(topic.node.topicId) || topic.scope !== scope || (scope === "project" && topic.workspaceRoot !== (project.root ?? ""))) continue;
      const index = base.findIndex((node) => node.topicId === topic.node.topicId);
      if (index < 0) {
        runtimeOnly.push({ ...topic.node, runtimeOnly: true, children: asArray(topic.node.children) });
        continue;
      }
      base[index] = {
        ...base[index],
        sessionPath: topic.node.sessionPath,
        open: topic.node.open,
        running: topic.node.running,
        status: topic.node.status,
        children: asArray(topic.node.children),
      };
    }
    return { ...project, children: [...runtimeOnly, ...base] };
  });
}

export function normalizeProjectTreeRuntimeSnapshot(payload: unknown): ProjectTreeRuntimeSnapshot {
  const value = (payload ?? {}) as Partial<ProjectTreeRuntimeSnapshot>;
  return { revision: value.revision ?? 0, topics: asArray(value.topics) };
}

export function onProjectTreeRuntimeChanged(cb: (event: ProjectTreeRuntimeSnapshot) => void): () => void {
  if (typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("project-tree:runtime-changed", (payload?: unknown) => cb(normalizeProjectTreeRuntimeSnapshot(payload)));
  }
  return () => {};
}

export function bindProjectTreeRuntime(
  setTree: (update: (tree: ProjectNode[]) => ProjectNode[]) => void,
  getSnapshot: () => Promise<ProjectTreeRuntimeSnapshot> | undefined,
  excludedTopicIds: () => ReadonlySet<string>,
) {
  let active = true;
  let snapshot: ProjectTreeRuntimeSnapshot | null = null;
  const apply = (tree: ProjectNode[]) => snapshot ? projectTreeApplyRuntimeTopics(tree, snapshot.topics, excludedTopicIds()) : tree;
  const accept = (next: ProjectTreeRuntimeSnapshot) => {
    if (!active || (snapshot && next.revision < snapshot.revision)) return;
    snapshot = next;
    setTree(apply);
  };
  const stop = onProjectTreeRuntimeChanged(accept);
  void getSnapshot()?.then(accept).catch(() => {});
  return {
    apply,
    dispose() {
      active = false;
      stop();
    },
  };
}
