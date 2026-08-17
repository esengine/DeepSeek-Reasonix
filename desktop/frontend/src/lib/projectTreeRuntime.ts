import { asArray } from "./array";
import type { ProjectNode, ProjectRuntimeTopic, ProjectTreeRuntimeSnapshot } from "./types";

const noExcludedTopicIds: ReadonlySet<string> = new Set();

function withoutRuntimeState(node: ProjectNode): ProjectNode {
  return { ...node, open: undefined, running: undefined, status: undefined, children: [] };
}

function runtimeTopicKey(scope: string, workspaceRoot: string, topicId: string): string {
  return `${scope}\u0000${workspaceRoot}\u0000${topicId}`;
}

function sameOwnFields(current: ProjectNode, next: ProjectNode): boolean {
  const keys = new Set([...Object.keys(current), ...Object.keys(next)]);
  keys.delete("children");
  for (const key of keys) {
    if (current[key as keyof ProjectNode] !== next[key as keyof ProjectNode]) return false;
  }
  return true;
}

function reconcileNode(current: ProjectNode | undefined, next: ProjectNode): ProjectNode {
  const currentChildren = asArray(current?.children);
  const currentByKey = new Map(currentChildren.map((child) => [child.key, child]));
  const nextChildren = asArray(next.children).map((child) => reconcileNode(currentByKey.get(child.key), child));
  if (current
    && sameOwnFields(current, next)
    && currentChildren.length === nextChildren.length
    && currentChildren.every((child, index) => child === nextChildren[index])) {
    return current;
  }
  return { ...next, children: nextChildren };
}

function rememberResidentTopics(
  tree: ProjectNode[],
  residentTopics: Map<string, ProjectNode>,
  excludedTopicIds: ReadonlySet<string>,
): Set<string> {
  const catalogKeys = new Set<string>();
  for (const project of tree) {
    if (project.kind !== "project" && project.kind !== "global_folder") continue;
    const scope = project.kind === "project" ? "project" : "global";
    const root = scope === "project" ? project.root ?? "" : "";
    for (const topic of asArray(project.children)) {
      if (topic.runtimeOnly || !topic.topicId || excludedTopicIds.has(topic.topicId)) continue;
      const key = runtimeTopicKey(scope, root, topic.topicId);
      catalogKeys.add(key);
      residentTopics.set(key, withoutRuntimeState(topic));
    }
  }
  return catalogKeys;
}

function activeRuntimeTopicKeys(
  topics: ProjectRuntimeTopic[],
  excludedTopicIds: ReadonlySet<string>,
): Set<string> {
  return new Set(topics.flatMap((topic) => {
    const topicId = topic.node.topicId;
    if (!topicId || excludedTopicIds.has(topicId)) return [];
    return [runtimeTopicKey(topic.scope, topic.scope === "project" ? topic.workspaceRoot ?? "" : "", topicId)];
  }));
}

function pruneResidentTopics(
  residentTopics: Map<string, ProjectNode>,
  catalogKeys: ReadonlySet<string>,
  runtimeKeys: ReadonlySet<string>,
) {
  for (const key of residentTopics.keys()) {
    if (!catalogKeys.has(key) && !runtimeKeys.has(key)) residentTopics.delete(key);
  }
}

// This structural-sharing overlay is loaded after the project tree mounts so
// runtime reconciliation does not enlarge the first-paint bundle. Subscription
// still precedes the initial snapshot read, so loading the module cannot lose
// an ownership transition.
export function projectTreeApplyRuntimeTopics(
  tree: ProjectNode[],
  topics: ProjectRuntimeTopic[],
  excludedTopicIds: ReadonlySet<string> = noExcludedTopicIds,
  residentTopics?: ReadonlyMap<string, ProjectNode>,
): ProjectNode[] {
  const nextTree = tree.map((project) => {
    if (project.kind !== "project" && project.kind !== "global_folder") return project;
    const scope = project.kind === "project" ? "project" : "global";
    const root = scope === "project" ? project.root ?? "" : "";
    const runtimeByTopic = new Map(topics
      .filter((topic) => topic.node.topicId
        && !excludedTopicIds.has(topic.node.topicId)
        && topic.scope === scope
        && (scope !== "project" || topic.workspaceRoot === root))
      .map((topic) => [topic.node.topicId!, topic]));
    const currentChildren = asArray(project.children);
    const base: ProjectNode[] = [];
    for (const node of currentChildren) {
      if (node.runtimeOnly || excludedTopicIds.has(node.topicId ?? "")) continue;
      const runtime = node.topicId ? runtimeByTopic.get(node.topicId) : undefined;
      if (runtime) runtimeByTopic.delete(node.topicId!);
      const next = runtime ? {
        ...withoutRuntimeState(node),
        sessionPath: runtime.node.sessionPath,
        open: runtime.node.open,
        running: runtime.node.running,
        status: runtime.node.status,
        children: asArray(runtime.node.children),
      } : withoutRuntimeState(node);
      base.push(reconcileNode(node, next));
    }
    const runtimeOnly: ProjectNode[] = [];
    for (const topic of runtimeByTopic.values()) {
      const topicId = topic.node.topicId!;
      const current = currentChildren.find((node) => node.runtimeOnly && node.topicId === topicId);
      const resident = residentTopics?.get(runtimeTopicKey(scope, root, topicId));
      const stable = withoutRuntimeState(resident ?? current ?? topic.node);
      runtimeOnly.push(reconcileNode(current, {
        ...stable,
        key: topic.node.key,
        kind: topic.node.kind,
        label: resident?.label ?? topic.node.label,
        root: topic.node.root,
        topicId,
        sessionPath: topic.node.sessionPath,
        open: topic.node.open,
        running: topic.node.running,
        status: topic.node.status,
        runtimeOnly: true,
        children: asArray(topic.node.children),
      }));
    }
    return reconcileNode(project, { ...project, children: [...runtimeOnly, ...base] });
  });
  return nextTree.every((project, index) => project === tree[index]) ? tree : nextTree;
}

export function createProjectTreeRuntimeProjection() {
  const residentTopics = new Map<string, ProjectNode>();
  return {
    apply(
      tree: ProjectNode[],
      topics: ProjectRuntimeTopic[],
      excludedTopicIds: ReadonlySet<string> = noExcludedTopicIds,
    ): ProjectNode[] {
      const catalogKeys = rememberResidentTopics(tree, residentTopics, excludedTopicIds);
      pruneResidentTopics(residentTopics, catalogKeys, activeRuntimeTopicKeys(topics, excludedTopicIds));
      return projectTreeApplyRuntimeTopics(tree, topics, excludedTopicIds, residentTopics);
    },
  };
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
  const projection = createProjectTreeRuntimeProjection();
  const apply = (tree: ProjectNode[]) => snapshot ? projection.apply(tree, snapshot.topics, excludedTopicIds()) : tree;
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
