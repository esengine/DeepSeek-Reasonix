import { desktopHost } from "./desktopHost";
import { asArray } from "./array";
import { runtimeStateStore } from "./runtimeStateStore";
import { projectSessionIdentity, projectSessionExcluded, projectSessionResolver } from "./projectSessionIdentity";
import type { ProjectNode, ProjectRuntimeTopic, ProjectTreeRuntimeSnapshot } from "./types";

const noExcludedTopicIds: ReadonlySet<string> = new Set();

function withoutRuntimeState(node: ProjectNode): ProjectNode {
  return { ...node, open: undefined, running: undefined, status: undefined,
    children: asArray(node.children).map(withoutRuntimeState) };
}

function runtimeChildren(runtime: ProjectNode, catalog?: ProjectNode): ProjectNode[] {
  const overlays = new Map(asArray(runtime.children).map(child => [projectSessionIdentity(child), child]));
  const children = asArray(catalog?.children).map(child => {
    const overlay = overlays.get(projectSessionIdentity(child));
    if (overlay) overlays.delete(projectSessionIdentity(child));
    return overlay ? { ...withoutRuntimeState(child), open: overlay.open, running: overlay.running,
      status: overlay.status, children: runtimeChildren(overlay, child) } : withoutRuntimeState(child);
  });
  return [...children, ...overlays.values()];
}

function runtimeTopicKey(scope: string, workspaceRoot: string, node: ProjectNode): string {
  return `${scope}\u0000${workspaceRoot}\u0000${projectSessionIdentity(node)}`;
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
      if (topic.runtimeOnly || projectSessionExcluded(topic, excludedTopicIds)) continue;
      const key = runtimeTopicKey(scope, root, topic);
      catalogKeys.add(key);
      residentTopics.set(key, { ...withoutRuntimeState(topic), children: asArray(topic.children) });
    }
  }
  return catalogKeys;
}

function activeRuntimeTopicKeys(
  topics: ProjectRuntimeTopic[],
  excludedTopicIds: ReadonlySet<string>,
): Set<string> {
  return new Set(topics.flatMap((topic) => {
    if (projectSessionExcluded(topic.node, excludedTopicIds)) return [];
    return [runtimeTopicKey(topic.scope, topic.scope === "project" ? topic.workspaceRoot ?? "" : "", topic.node)];
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
    const available = topics
      .filter((topic) => !projectSessionExcluded(topic.node, excludedTopicIds)
        && topic.scope === scope
        && (scope !== "project" || topic.workspaceRoot === root));
    const identityOf = projectSessionResolver([...asArray(project.children), ...available.map(topic => topic.node)]);
    const runtimeByTopic = new Map(available.map(topic => [identityOf(topic.node), topic]));
    const currentChildren = asArray(project.children);
    const base: ProjectNode[] = [];
    for (const node of currentChildren) {
      if (node.runtimeOnly || projectSessionExcluded(node, excludedTopicIds)) continue;
      const identity = identityOf(node);
      if (base.some(row => identityOf(row) === identity)) continue;
      const candidate = runtimeByTopic.get(identity);
      if (candidate) runtimeByTopic.delete(identity);
      const runtime = candidate && (candidate.node.lifecycleGeneration ?? 0) >= (node.lifecycleGeneration ?? 0) ? candidate : undefined;
      const next = runtime ? {
        ...withoutRuntimeState(node),
        session: node.session ?? runtime.node.session,
        identityAliases: node.identityAliases ?? runtime.node.identityAliases,
        open: runtime.node.open,
        running: runtime.node.running,
        status: runtime.node.status,
        children: runtimeChildren(runtime.node, node),
      } : withoutRuntimeState(node);
      base.push(reconcileNode(node, next));
    }
    const runtimeOnly: ProjectNode[] = [];
    for (const topic of runtimeByTopic.values()) {
      const topicId = topic.node.topicId!;
      const identity = projectSessionIdentity(topic.node);
      const current = currentChildren.find((node) => node.runtimeOnly && projectSessionIdentity(node) === identity);
      const resident = residentTopics?.get(runtimeTopicKey(scope, root, topic.node));
      const stable = withoutRuntimeState(resident ?? current ?? topic.node);
      runtimeOnly.push(reconcileNode(current, {
        ...stable,
        key: topic.node.key,
        kind: topic.node.kind,
        label: resident?.label ?? topic.node.label,
        root: topic.node.root,
        topicId,
        sessionPath: stable.sessionPath || topic.node.sessionPath,
        open: topic.node.open,
        running: topic.node.running,
        status: topic.node.status,
        runtimeOnly: true,
        children: runtimeChildren(topic.node, resident ?? current),
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
  const host = desktopHost();
  if (host.kind === "none") return () => {};
  return host.events.on("project-tree:runtime-changed", (payload?: unknown) => cb(normalizeProjectTreeRuntimeSnapshot(payload)));
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
  const unified = () => {
    const current = runtimeStateStore.getSnapshot();
    if (current) {
      const topics = runtimeStateStore.getFailed() ? current.topics.map(topic => ({ ...topic, node: { ...topic.node, running: false, status: "unknown" as const } })) : current.topics;
      snapshot = null;
      accept({ revision: current.revision, topics });
    }
  };
  const stopUnified = runtimeStateStore.subscribe(unified);
  const stop = onProjectTreeRuntimeChanged(next => { if (!runtimeStateStore.getSnapshot()) accept(next); });
  unified();
  if (!runtimeStateStore.getSnapshot()) void getSnapshot()?.then(next => { if (!runtimeStateStore.getSnapshot()) accept(next); }).catch(() => {});
  return {
    apply,
    dispose() {
      active = false;
      stop();
      stopUnified();
    },
  };
}
