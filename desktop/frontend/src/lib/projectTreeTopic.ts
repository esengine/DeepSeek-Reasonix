import { asArray } from "./array";
import { getLocale, type DictKey, type Translator } from "./i18n";
import type { ProjectNode, ProjectTopicStatus } from "./types";
import { projectSessionIdentity, projectSessionExcluded, projectSessionKeys, sameProjectSession } from "./projectSessionIdentity";

export type ProjectTreeVariant = "workbench" | "creation";
export type WorkbenchSortMode = "created" | "updated";

// Shared by workbench and creation; key string kept for existing saved choices
// and for downgrade compatibility.
export const WORKBENCH_SORT_KEY = "projectTree:workbenchSort";
export const WORKBENCH_SORT_CREATED_DEFAULT_MIGRATION_KEY = "projectTree:workbenchSort:createdDefault:v1";

export function loadWorkbenchSortMode(): WorkbenchSortMode {
  try {
    if (localStorage.getItem(WORKBENCH_SORT_CREATED_DEFAULT_MIGRATION_KEY) !== "1") {
      // This release intentionally resets every existing choice once. Keep the
      // original preference key so older builds can still read the new value.
      localStorage.setItem(WORKBENCH_SORT_KEY, "created");
      localStorage.setItem(WORKBENCH_SORT_CREATED_DEFAULT_MIGRATION_KEY, "1");
      return "created";
    }
    const value = localStorage.getItem(WORKBENCH_SORT_KEY);
    if (value === "created" || value === "updated") return value;
  } catch {
    /* localStorage unavailable */
  }
  return "created";
}

export function isRuntimeSessionNode(node: ProjectNode): boolean {
  return node.kind === "session" || node.kind === "global_session";
}

export function isTopicNode(node: ProjectNode): boolean {
  return node.kind === "topic" || node.kind === "global_topic";
}

export function projectTreeRevisionIsFresh(currentRevision: number, incomingRevision: number): boolean {
  return incomingRevision >= currentRevision;
}

export function projectTreeTopicPageIsFresh(
  revisions: Readonly<Record<string, number>>,
  projectKey: string,
  incomingRevision: number,
): boolean {
  return projectTreeRevisionIsFresh(revisions[projectKey] ?? 0, incomingRevision);
}

// Project shells come from desktop-projects.json and are valid even when the
// disposable catalog still reports revision 0. Catalog revision only gates
// topic pages and non-empty tree refreshes after the first shell is painted.
export function projectTreeShouldApplyShellSnapshot(options: {
  currentRevision: number;
  incomingRevision: number;
  treeEmpty: boolean;
}): boolean {
  if (options.treeEmpty) return true;
  return projectTreeRevisionIsFresh(options.currentRevision, options.incomingRevision);
}

export function mergeProjectTopicPage(current: ProjectNode[], incoming: ProjectNode[], append: boolean): ProjectNode[] {
  // Owner aliases retire the source projection even when its old page arrives
  // after adoption. Canonical identity always wins; its durable row metadata is retained.
  incoming = incoming.map(row => !row.session ? current.find(old => old.session && sameProjectSession(old,row)) ?? row : row);
  if (!append) {
    // Project snapshots carry every pinned topic shell, while a lazy first
    // page is bounded. Keep off-page pins so expanding a busy project cannot
    // make its pinned section incomplete again.
    const incomingKeys = new Set(incoming.flatMap(projectSessionKeys));
    const offPagePins = current.filter((node) => Boolean(node.pinned) && !projectSessionKeys(node).some(key => incomingKeys.has(key)));
    return [...incoming, ...offPagePins];
  }
  const next = [...current];
  const positions = new Map(next.flatMap((node, index) => projectSessionKeys(node).map(key => [key,index] as const)));
  for (const node of incoming) {
    const index = projectSessionKeys(node).map(key => positions.get(key)).find(index => index !== undefined);
    if (index === undefined) {
      positions.set(projectSessionIdentity(node), next.length);
      next.push(node);
    } else {
      next[index] = node;
    }
  }
  const seen = new Set<string>();
  return next.filter(node => {
    const keys = projectSessionKeys(node);
    if (keys.some(key => seen.has(key))) return false;
    keys.forEach(key => seen.add(key)); return true;
  });
}

// A directory scan commits catalog rows in batches, but an incomplete page is
// not authoritative for replacement, deletion, timestamps, or order. Keep the
// last complete resident rows byte-for-byte and append only newly discovered
// keys until a complete page can replace the canonical first page.
export function mergeIncompleteProjectTopicPage(current: ProjectNode[], incoming: ProjectNode[]): ProjectNode[] {
  const residents = new Map(current.flatMap(node => projectSessionKeys(node).map(key => [key, node] as const)));
  const discovered = incoming.filter(node => {
    const resident = projectSessionKeys(node).map(key => residents.get(key)).find(Boolean);
    return !resident || Boolean(node.session && !resident.session);
  });
  return discovered.length === 0 ? current : mergeProjectTopicPage(current, discovered, true);
}

// Topic page loads rewrite children, so a signature keyed only on the project
// shells lets the debounced reload effect observe arrivals without re-arming
// itself on its own writes.
export function projectTreeShellSignature(tree: ProjectNode[]): string {
  return tree.map((node) => node.key).join("\u001f");
}

// After archive, drop that topic immediately so a shell-only refresh cannot
// resurrect it from the previously loaded children.
export function projectTreeWithoutTopic(tree: ProjectNode[], topicId: string): ProjectNode[] {
  const id = topicId.trim();
  if (!id) return tree;
  return projectTreeWithoutTopics(tree, new Set([id]));
}

// Post-commit archive IDs are a client-side tombstone overlay. Apply it to
// every incoming page as well as the resident tree so a pre-commit request
// cannot paint a topic back before the canonical reload acquires its sequence.
export function projectTreeWithoutTopics(tree: ProjectNode[], topicIds: ReadonlySet<string>): ProjectNode[] {
  if (topicIds.size === 0) return tree;
  let changed = false;
  const next: ProjectNode[] = [];
  for (const node of tree) {
    if ((isTopicNode(node) || isRuntimeSessionNode(node)) &&
      projectSessionExcluded(node, topicIds)) {
      changed = true;
      continue;
    }
    const children = asArray(node.children);
    const filteredChildren = projectTreeWithoutTopics(children, topicIds);
    if (filteredChildren !== children) {
      changed = true;
      next.push({ ...node, children: filteredChildren });
    } else {
      next.push(node);
    }
  }
  return changed ? next : tree;
}

export function projectTreeWithoutSession(tree: ProjectNode[], target: ProjectNode): ProjectNode[] {
  return projectTreeWithoutTopics(tree, new Set([projectSessionIdentity(target)]));
}

// After a successful rename, paint the new label immediately instead of
// waiting for the catalog event round-trip.
export function projectTreeWithTopicTitle(tree: ProjectNode[], topicId: string, title: string): ProjectNode[] {
  const id = topicId.trim();
  if (!id) return tree;
  let changed = false;
  const next: ProjectNode[] = [];
  for (const node of tree) {
    if (node.topicId === id && (isTopicNode(node) || isRuntimeSessionNode(node))) {
      if (node.label !== title) {
        changed = true;
        next.push({ ...node, label: title });
      } else {
        next.push(node);
      }
      continue;
    }
    const children = asArray(node.children);
    const renamedChildren = projectTreeWithTopicTitle(children, id, title);
    if (renamedChildren !== children) {
      changed = true;
      next.push({ ...node, children: renamedChildren });
    } else {
      next.push(node);
    }
  }
  return changed ? next : tree;
}

export function projectTreeWithSessionTitle(tree: ProjectNode[], target: ProjectNode, title: string): ProjectNode[] {
  const identity = projectSessionIdentity(target);
  return tree.map((node) => {
    if ((isTopicNode(node) || isRuntimeSessionNode(node)) && projectSessionIdentity(node) === identity) {
      return node.label === title ? node : { ...node, label: title };
    }
    if (!node.children?.length) return node;
    const children = projectTreeWithSessionTitle(node.children, target, title);
    return children.every((child, index) => child === node.children?.[index]) ? node : { ...node, children };
  });
}

export function projectTreeFolderKeyForTopic(tree: ProjectNode[], topicId: string): string {
  const id = topicId.trim();
  if (!id) return "";
  for (const node of tree) {
    if (node.kind !== "project" && node.kind !== "global_folder") continue;
    if (asArray(node.children).some((child) => child.topicId === id)) return node.key;
  }
  return "";
}

export function projectTreeFolderKeyForSession(tree: ProjectNode[], sessionPath: string): string {
  const path = sessionPath.trim();
  if (!path) return "";
  const containsSession = (nodes: ProjectNode[]): boolean => nodes.some((node) =>
    ((isRuntimeSessionNode(node) || isTopicNode(node)) && node.sessionPath?.trim() === path)
    || containsSession(asArray(node.children)),
  );
  for (const node of tree) {
    if (node.kind !== "project" && node.kind !== "global_folder") continue;
    if (containsSession(asArray(node.children))) return node.key;
  }
  return "";
}

export function invalidateProjectTreeTopicLoads(sequences: Record<string, number>, keys: Iterable<string>): void {
  for (const key of keys) {
    let matched = false;
    const prefix = `${key}\u001f`;
    for (const sequenceKey of Object.keys(sequences)) {
      if (sequenceKey !== key && !sequenceKey.startsWith(prefix)) continue;
      sequences[sequenceKey] = (sequences[sequenceKey] ?? 0) + 1;
      matched = true;
    }
    if (!matched) sequences[key] = (sequences[key] ?? 0) + 1;
  }
}

export function projectTreeShellChildren(
  previous: ProjectNode[] | undefined,
  pinnedShells: ProjectNode[] | undefined = [],
): ProjectNode[] {
  const shells = asArray(pinnedShells).filter((node) => isTopicNode(node) && Boolean(node.pinned));
  if (!previous || previous.length === 0) return shells;

  const shellByKey = new Map(shells.map((node) => [node.key, node]));
  const next = asArray(previous).map((node) => {
    if (!isTopicNode(node)) return node;
    const shell = shellByKey.get(node.key);
    if (!shell) return node.pinned ? { ...node, pinned: false } : node;
    shellByKey.delete(node.key);
    return { ...node, ...shell, children: node.children ?? shell.children };
  });
  return [...next, ...shellByKey.values()];
}

export function projectTreeEventAffectsFolder(project: ProjectNode, roots: string[]): boolean {
  if (roots.length === 0) return true;
  const root = project.kind === "global_folder" ? "" : project.root ?? "";
  return roots.includes(root);
}

export type ProjectTreeTopicOpenRequest = {
  scope: "global" | "project";
  workspaceRoot: string;
  topicId: string;
  sessionPath?: string;
};

export function projectTreeTopicOpenRequest(node: ProjectNode): ProjectTreeTopicOpenRequest | null {
  if (!isTopicNode(node) && !isRuntimeSessionNode(node)) return null;
  const scope = node.kind === "global_topic" || node.kind === "global_session" ? "global" : "project";
  return {
    scope,
    workspaceRoot: scope === "global" ? "" : node.root ?? "",
    topicId: node.topicId ?? "",
    sessionPath: node.session ? `session-id:${node.session.sessionId}` : node.source?.headId
      ? `session-source:${encodeURIComponent(JSON.stringify(node.source))}` : node.sessionPath,
  };
}

export type ProjectTreeTopicClickTarget = {
  rowKey: string;
  canRename: boolean;
};

export type ProjectTreePendingTopicOpen = ProjectTreeTopicClickTarget & {
  timer: ReturnType<typeof setTimeout>;
};

export function projectTreeShouldSuppressOpenForRename(
  pending: ProjectTreeTopicClickTarget | null,
  next: ProjectTreeTopicClickTarget,
): boolean {
  return Boolean(pending && pending.rowKey === next.rowKey && pending.canRename && next.canRename);
}

export type ProjectTreeFolderDisclosure = {
  canExpand: boolean;
  isOpen: boolean;
  ariaExpanded?: boolean;
  iconStackClassName: string;
};

// allowEmptyExpand lets a project shell open before its first topic page has
// arrived: without it an empty folder is inert, so expanding it could never
// start the load that fills it.
export function projectTreeFolderDisclosure(hasChildren: boolean, isExpanded: boolean, allowEmptyExpand = false): ProjectTreeFolderDisclosure {
  const canExpand = hasChildren || allowEmptyExpand;
  const isOpen = canExpand && isExpanded;
  return {
    canExpand,
    isOpen,
    ariaExpanded: canExpand ? isExpanded : undefined,
    iconStackClassName: `project-tree__icon-stack${canExpand ? " project-tree__icon-stack--expandable" : ""}`,
  };
}

function topicMatchesActiveIdentity(node: ProjectNode, activeScope?: string, activeWorkspaceRoot?: string, activeTopicId?: string): boolean {
  if (!node.topicId || !activeTopicId) return false;
  const scope = node.kind === "global_topic" || node.kind === "global_session" ? "global" : "project";
  if (scope === "global") return activeScope === "global" && activeTopicId === node.topicId;
  return activeScope === "project" && activeTopicId === node.topicId && activeWorkspaceRoot === node.root;
}

type ActiveRemoteSessionIdentity = {
  hostId: string;
  workspace: string;
  sessionId?: string;
};

function remoteTopicMatchesActiveSession(node: ProjectNode, activeRemote?: ActiveRemoteSessionIdentity): boolean {
  const remote = node.remoteSession;
  const active = activeRemote;
  if (!remote || !active || remote.hostId !== active.hostId || remote.workspace !== active.workspace) return false;
  const sessionID = active.sessionId?.trim();
  if (!sessionID) return false;
  return remote.sessionId?.trim() === sessionID || (!remote.sessionId?.trim() && remote.name.trim() === sessionID);
}

export function topicIsActive(
  node: ProjectNode,
  activeScope?: string,
  activeWorkspaceRoot?: string,
  activeTopicId?: string,
  activeSessionPath?: string,
  activeRemote?: ActiveRemoteSessionIdentity,
): boolean {
  if (node.source?.headId) return projectTreeTopicOpenRequest(node)?.sessionPath === activeSessionPath;
  if (node.session?.sessionId) {
    return Boolean(activeSessionPath && (activeSessionPath === `session-id:${node.session.sessionId}` || activeSessionPath === node.sessionPath
      || projectSessionKeys(node).includes(`path\u0000${activeSessionPath}`)));
  }
  if (isRuntimeSessionNode(node)) {
    return Boolean(node.sessionPath && activeSessionPath && activeSessionPath === node.sessionPath);
  }
  if (!isTopicNode(node)) return false;
  if (activeSessionPath && asArray(node.children).some(isRuntimeSessionNode)) return false;
  // Remote filesystem paths are not globally unique: two hosts can expose the
  // same absolute session path. Their synthesized rows already carry a
  // host-qualified topicId, so never let the generic path fallback mark a row
  // from another host active.
  if (node.remoteSession) {
    if (remoteTopicMatchesActiveSession(node, activeRemote)) return true;
    return topicMatchesActiveIdentity(node, activeScope, activeWorkspaceRoot, activeTopicId);
  }
  if (node.sessionPath) return Boolean(activeSessionPath && activeSessionPath === node.sessionPath);
  if (topicMatchesActiveIdentity(node, activeScope, activeWorkspaceRoot, activeTopicId)) return true;
  return Boolean(node.sessionPath && activeSessionPath && activeSessionPath === node.sessionPath);
}

export function projectTreeTopicMetaLine(node: ProjectNode, t: Translator, compact = false): string {
  const parts: string[] = [];
  const turns = node.turns ?? 0;
  if (node.turnsState === "unknown") parts.push(t("history.indexing"));
  else if (turns > 0) parts.push(t(turns === 1 ? "history.turnOne" : "history.turnOther", { n: turns }));
  const activityAt = node.lastActivityAt || node.createdAt || 0;
  if (activityAt) parts.push(topicActivityLabel(activityAt, t, compact));
  if (parts.length === 0) parts.push(t("projectTree.previously"));
  return parts.join(" · ");
}

// Activity labels older than a week are already the calendar date (always the
// meta line's last part), so callers pairing the two keep a single copy.
export function projectTreeDedupedExactTime(metaLine: string, exactTime: string): string {
  return exactTime && metaLine.endsWith(exactTime) ? "" : exactTime;
}

export function topicUnknownTimeLabel(node: ProjectNode, t: Translator): string {
  return topicActivityAt(node) ? "" : t("projectTree.previously");
}

const topicStatusLabels: Record<ProjectTopicStatus, DictKey> = {
  thinking: "projectTree.status.thinking",
  finishing: "runtime.finishing",
  unknown: "runtime.unknown",
  cancelling: "status.jobStopping",
  streaming: "projectTree.status.streaming",
  waiting_confirmation: "projectTree.status.waitingConfirmation",
  background_job: "projectTree.status.backgroundJob",
  paused: "projectTree.status.paused",
  awaiting_delivery: "projectTree.status.awaitingDelivery",
  error: "projectTree.status.error",
  diverged_recovery: "projectTree.status.divergedRecovery",
};

export function normalizeTopicStatus(status?: string): ProjectTopicStatus | "" {
  if (status === "finishing" || status === "cancelling" || status === "unknown") return status;
  if (!status) return "";
  if (status === "thinking" || status === "streaming" || status === "waiting_confirmation" || status === "background_job" || status === "paused" || status === "awaiting_delivery" || status === "error" || status === "diverged_recovery") {
    return status;
  }
  return "";
}

export function topicStatus(node: ProjectNode): ProjectTopicStatus | "" {
  // Ordinary list never surfaces recovery-branch status. Active runtime states
  // only: thinking/streaming/waiting/etc. History owns other saved versions.
  const live = node.running ? "streaming" : "";
  const stored = normalizeTopicStatus(node.status);
  if (stored && stored !== "diverged_recovery") return stored;
  return live;
}

export function projectTreeTopicArchiveBlocked(node: ProjectNode): boolean {
  if (node.status === "finishing" || node.status === "cancelling" || node.status === "unknown") return true;
  if (asArray(node.children).some(projectTreeTopicArchiveBlocked)) return true;
  const status = normalizeTopicStatus(node.status);
  if (status === "thinking" || status === "streaming" || status === "waiting_confirmation" || status === "background_job") return true;
  if (status === "paused" || status === "awaiting_delivery" || status === "error" || status === "diverged_recovery") return false;
  return Boolean(node.running);
}

export function topicStatusLabel(node: ProjectNode, t: Translator): string {
  const status = topicStatus(node);
  return status ? t(topicStatusLabels[status]) : "";
}

export function topicActivityAt(node: ProjectNode): number {
  return node.lastActivityAt || node.createdAt || 0;
}

export function topicReadRevision(node: ProjectNode): number {
  if (node.session) return node.resultSequence ?? 0;
  return topicActivityAt(node);
}

export function projectTreeReadActivityKey(node: ProjectNode): string | null {
  if (node.session?.sessionId) return projectSessionIdentity(node);
  if (node.sessionPath || node.source || node.remoteSession) return projectSessionIdentity(node);
  const request = projectTreeTopicOpenRequest(node);
  if (!request?.topicId) return null;
  return [request.scope, request.workspaceRoot, request.topicId].join("\u001f");
}

export type ProjectTreeReadActivity = Record<string, number>;

export function projectTreeMigrateReadActivity(current: ProjectTreeReadActivity, storedVersion: number): ProjectTreeReadActivity {
  if (storedVersion >= 2) return current;
  let next = current;
  for (const [key, revision] of Object.entries(current)) {
    if (!key.startsWith("session\u001f") || revision !== 0) continue;
    if (next === current) next = { ...current };
    delete next[key];
  }
  return next;
}

export function projectTreeSeedReadActivity(nodes: readonly ProjectNode[], current: ProjectTreeReadActivity): ProjectTreeReadActivity {
  let next = current;
  const visit = (items: readonly ProjectNode[]) => {
    for (const node of items) {
      const key = projectTreeReadActivityKey(node);
      // A stale catalog cache is exposed as a canonical session with pending
      // metadata and resultSequence 0 while its durable log is rebuilt. Do not
      // persist that placeholder as the read baseline: once the real sequence
      // arrives it would make every historical result look newly unread.
      if (node.session && node.turnsState === "ready" && key
        && next[key] === undefined) {
        if (next === current) next = { ...current };
        next[key] = topicReadRevision(node);
      }
      visit(node.children ?? []);
    }
  };
  visit(nodes);
  return next;
}

export function projectTreeTopicHasUnreadActivity(
  node: ProjectNode,
  readActivity: ProjectTreeReadActivity,
  activeScope?: string,
  activeWorkspaceRoot?: string,
  activeTopicId?: string,
  activeSessionPath?: string,
  baselineAt = 0,
): boolean {
  if (!isTopicNode(node) && !isRuntimeSessionNode(node)) return false;
  if (topicIsActive(node, activeScope, activeWorkspaceRoot, activeTopicId, activeSessionPath)) return false;
  if (topicStatus(node) !== "") return false;
  const key = projectTreeReadActivityKey(node);
  const revision = topicReadRevision(node);
  if (!key || revision <= 0) return false;
  if (node.session) return readActivity[key] !== undefined && readActivity[key] < revision;
  return Math.max(readActivity[key] ?? 0, baselineAt) < revision;
}

export function projectTreeShouldRenderTopicActions(isSessionNode: boolean, variant: ProjectTreeVariant, unread: boolean): boolean {
  return !isSessionNode && variant !== "creation" && !unread;
}

// Pinning reorders the trees shared with creation mode, so the creation
// context menu keeps its original rename/trash-only entries.
export function projectTreeTopicMenuOffersPin(variant: ProjectTreeVariant): boolean {
  return variant !== "creation";
}

export function topicActivityLabel(ms: number, t: Translator, compact = false): string {
  if (ms <= 0) return "";
  const delta = Date.now() - ms;
  const locale = getLocale();
  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;
  const month = 30 * day;
  const year = 365 * day;
  if (delta < minute) return t("projectTree.justNow");
  if (!compact) {
    const rtfLocale = locale === "zh" ? "zh-CN" : locale === "zh-TW" ? "zh-TW" : "en";
    const rtf = new Intl.RelativeTimeFormat(rtfLocale, { numeric: "auto" });
    if (delta < hour) return rtf.format(-Math.max(1, Math.round(delta / minute)), "minute");
    if (delta < day) return rtf.format(-Math.round(delta / hour), "hour");
    if (delta < 7 * day) return rtf.format(-Math.round(delta / day), "day");
    return topicActivityDateLabel(ms);
  }
  if (delta < hour) {
    const value = Math.max(1, Math.round(delta / minute));
    return locale === "zh" || locale === "zh-TW" ? `${value} 分钟` : `${value}m`;
  }
  if (delta < day) {
    const value = Math.round(delta / hour);
    return locale === "zh" || locale === "zh-TW" ? `${value} 小时` : `${value}h`;
  }
  if (delta < 7 * day) {
    const value = Math.round(delta / day);
    return locale === "zh" || locale === "zh-TW" ? `${value} 天` : `${value}d`;
  }
  if (delta < month) {
    const value = Math.round(delta / day);
    return locale === "zh" || locale === "zh-TW" ? `${value} 天` : `${value}d`;
  }
  if (delta < year) {
    const value = Math.max(1, Math.round(delta / month));
    return locale === "zh" || locale === "zh-TW" ? `${value} 个月` : `${value}mo`;
  }
  const value = Math.max(1, Math.round(delta / year));
  return locale === "zh" || locale === "zh-TW" ? `${value} 年` : `${value}y`;
}

export function topicActivityDateLabel(ms: number): string {
  if (ms <= 0) return "";
  const locale = getLocale();
  const dateLocale = locale === "zh" ? "zh-CN" : locale === "zh-TW" ? "zh-TW" : "en";
  return new Date(ms).toLocaleDateString(dateLocale);
}
