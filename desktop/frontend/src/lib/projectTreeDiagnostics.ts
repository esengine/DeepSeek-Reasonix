import { asArray } from "./array";
import { isRuntimeSessionNode, isTopicNode } from "./projectTreeTopic";
import type { ProjectNode } from "./types";

export type ProjectTreeSessionDiagnosticSummary = {
  workspaceSessions: number;
  visibleSessions: number;
  hiddenSessions: number;
  hiddenByFilter: number;
  hiddenByCollapsed: number;
  hiddenByTruncation: number;
  runtimeSessions: number;
  runtimeOnlySessions: number;
  recoveryOnlySessions: number;
  recoveryCopySessions: number;
  recoveryCopies: number;
  runningSessions: number;
  unreadSessions: number;
  pinnedSessions: number;
  activeSessions: number;
  activeVisibleSessions: number;
  folderCount: number;
  expandedFolders: number;
  showAllFolders: number;
};

export type ProjectTreeSessionDiagnosticOptions = {
  tree: ProjectNode[];
  visibleTree: ProjectNode[];
  expanded: ReadonlySet<string>;
  showAllTopics: ReadonlySet<string>;
  classicTruncationActive: boolean;
  queryActive: boolean;
  timeFilterActive: boolean;
  projectNodeKey: (node: ProjectNode, depth: number) => string;
  isActive?: (node: ProjectNode) => boolean;
  isUnread?: (node: ProjectNode) => boolean;
};

type Counters = Omit<ProjectTreeSessionDiagnosticSummary, "visibleSessions" | "hiddenSessions" | "hiddenByFilter" | "hiddenByCollapsed" | "hiddenByTruncation" | "activeVisibleSessions" | "expandedFolders" | "showAllFolders">;

function isFolder(node: ProjectNode): boolean {
  return node.kind === "project" || node.kind === "global_folder";
}

function isSessionRow(node: ProjectNode): boolean {
  return isTopicNode(node) || isRuntimeSessionNode(node);
}

function countSessionRows(nodes: ProjectNode[]): number {
  let count = 0;
  for (const node of nodes) {
    if (isSessionRow(node)) count += 1;
    count += countSessionRows(asArray(node.children));
  }
  return count;
}

function collectCounters(
  nodes: ProjectNode[],
  counters: Counters,
  isActive: (node: ProjectNode) => boolean,
  isUnread: (node: ProjectNode) => boolean,
): void {
  for (const node of nodes) {
    if (isFolder(node)) counters.folderCount += 1;
    if (isSessionRow(node)) {
      counters.workspaceSessions += 1;
      if (isRuntimeSessionNode(node)) counters.runtimeSessions += 1;
      if (node.runtimeOnly) counters.runtimeOnlySessions += 1;
      if (node.recoveryState === "recovery_only") counters.recoveryOnlySessions += 1;
      if (node.running) counters.runningSessions += 1;
      if (isUnread(node)) counters.unreadSessions += 1;
      if (node.pinned) counters.pinnedSessions += 1;
      if (isActive(node)) counters.activeSessions += 1;
    }
    collectCounters(asArray(node.children), counters, isActive, isUnread);
  }
}

function collectVisible(
  nodes: ProjectNode[],
  depth: number,
  parentVisible: boolean,
  options: ProjectTreeSessionDiagnosticOptions,
  counters: { visibleSessions: number; activeVisibleSessions: number; hiddenByCollapsed: number; hiddenByTruncation: number },
): void {
  for (const node of nodes) {
    const children = asArray(node.children);
    const visible = parentVisible;
    if (isSessionRow(node) && visible) {
      counters.visibleSessions += 1;
      if (options.isActive?.(node)) counters.activeVisibleSessions += 1;
    }

    const key = options.projectNodeKey(node, depth);
    const isExpanded = options.queryActive || options.expanded.has(key);
    if (!isExpanded) {
      counters.hiddenByCollapsed += countSessionRows(children);
      continue;
    }

    if (!visible) {
      // The nearest collapsed ancestor owns the hidden-reason bucket. This
      // keeps filtered, collapsed, and classic-window counts disjoint.
      counters.hiddenByCollapsed += countSessionRows(children);
      continue;
    }

    let childNodes = children;
    if (isFolder(node) && options.classicTruncationActive) {
      const showAll = options.showAllTopics.has(key);
      const windowed = children.length <= 5 || showAll ? children : children.slice(0, 5);
      if (windowed.length !== children.length) {
        counters.hiddenByTruncation += countSessionRows(children.slice(windowed.length));
      }
      childNodes = windowed;
    }
    collectVisible(childNodes, depth + 1, visible, options, counters);
  }
}

export function summarizeProjectTreeSessions(options: ProjectTreeSessionDiagnosticOptions): ProjectTreeSessionDiagnosticSummary {
  const counters: Counters = {
    workspaceSessions: 0,
    runtimeSessions: 0,
    runtimeOnlySessions: 0,
    recoveryOnlySessions: 0,
    recoveryCopySessions: 0,
    recoveryCopies: 0,
    runningSessions: 0,
    unreadSessions: 0,
    pinnedSessions: 0,
    activeSessions: 0,
    folderCount: 0,
  };
  const isActive = options.isActive ?? (() => false);
  const isUnread = options.isUnread ?? (() => false);
  collectCounters(options.tree, counters, isActive, isUnread);

  const visible = {
    visibleSessions: 0,
    activeVisibleSessions: 0,
    hiddenByCollapsed: 0,
    hiddenByTruncation: 0,
  };
  collectVisible(options.visibleTree, 0, true, options, visible);

  const hiddenByFilter = Math.max(0, counters.workspaceSessions - countSessionRows(options.visibleTree));
  const hiddenSessions = Math.max(0, counters.workspaceSessions - visible.visibleSessions);
  let expandedFolders = 0;
  let showAllFolders = 0;
  for (const node of options.tree) {
    const walk = (current: ProjectNode[], depth: number) => {
      for (const item of current) {
        if (isFolder(item)) {
          const key = options.projectNodeKey(item, depth);
          if (options.queryActive || options.expanded.has(key)) expandedFolders += 1;
          if (options.showAllTopics.has(key)) showAllFolders += 1;
        }
        walk(asArray(item.children), depth + 1);
      }
    };
    walk([node], 0);
  }

  return {
    ...counters,
    visibleSessions: visible.visibleSessions,
    hiddenSessions,
    hiddenByFilter,
    hiddenByCollapsed: Math.min(hiddenSessions, visible.hiddenByCollapsed),
    hiddenByTruncation: Math.min(hiddenSessions, visible.hiddenByTruncation),
    activeVisibleSessions: visible.activeVisibleSessions,
    expandedFolders,
    showAllFolders,
  };
}
