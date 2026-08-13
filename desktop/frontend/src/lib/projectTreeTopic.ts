import { asArray } from "./array";
import { getLocale, type DictKey, type Translator } from "./i18n";
import type { ProjectNode, ProjectTopicStatus } from "./types";

export type ProjectTreeVariant = "classic" | "workbench" | "creation";

export function isRuntimeSessionNode(node: ProjectNode): boolean {
  return node.kind === "session" || node.kind === "global_session";
}

export function isTopicNode(node: ProjectNode): boolean {
  return node.kind === "topic" || node.kind === "global_topic";
}

export function projectTreeRevisionIsFresh(currentRevision: number, incomingRevision: number): boolean {
  return incomingRevision >= currentRevision;
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
  if (!append) return [...incoming];
  const next = [...current];
  const positions = new Map(next.map((node, index) => [node.key, index]));
  for (const node of incoming) {
    const index = positions.get(node.key);
    if (index === undefined) {
      positions.set(node.key, next.length);
      next.push(node);
    } else {
      next[index] = node;
    }
  }
  return next;
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
    sessionPath: node.sessionPath,
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

// allowEmptyExpand lets classic folders open without children so the expanded
// state can host the "no sessions" placeholder row; other variants keep the
// original contract where empty folders are inert.
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

export function topicIsActive(node: ProjectNode, activeScope?: string, activeWorkspaceRoot?: string, activeTopicId?: string, activeSessionPath?: string): boolean {
  if (!isTopicNode(node) && !isRuntimeSessionNode(node)) return false;
  if (node.sessionPath) return Boolean(activeSessionPath && activeSessionPath === node.sessionPath);
  if (activeSessionPath && asArray(node.children).some(isRuntimeSessionNode)) return false;
  const scope = node.kind === "global_topic" ? "global" : "project";
  return (
    activeTopicId === node.topicId &&
    activeScope === scope &&
    (scope === "global" || activeWorkspaceRoot === node.root)
  );
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

// Model for the classic hover preview card: the row keeps a time-only meta
// line, so the card carries the full title, turns, exact date, and project.
export type ProjectTreeTopicHoverCard = {
  title: string;
  statusLabel: string;
  metaLine: string;
  exactTime: string;
  projectLabel: string;
};

// Activity labels older than a week are already the calendar date (always the
// meta line's last part), so callers pairing the two keep a single copy.
export function projectTreeDedupedExactTime(metaLine: string, exactTime: string): string {
  return exactTime && metaLine.endsWith(exactTime) ? "" : exactTime;
}

export function projectTreeTopicHoverCardModel(node: ProjectNode, t: Translator, projectLabel: string): ProjectTreeTopicHoverCard {
  const activityAt = node.lastActivityAt || node.createdAt || 0;
  const metaLine = projectTreeTopicMetaLine(node, t);
  const exactTime = activityAt ? topicActivityDateLabel(activityAt) : "";
  return {
    title: (node.label || node.topicId || "Untitled").replace(/^●\s*/, ""),
    statusLabel: topicStatusLabel(node, t),
    metaLine,
    exactTime: projectTreeDedupedExactTime(metaLine, exactTime),
    projectLabel,
  };
}

export function topicUnknownTimeLabel(node: ProjectNode, t: Translator): string {
  return topicActivityAt(node) ? "" : t("projectTree.previously");
}

const topicStatusLabels: Record<ProjectTopicStatus, DictKey> = {
  thinking: "projectTree.status.thinking",
  streaming: "projectTree.status.streaming",
  waiting_confirmation: "projectTree.status.waitingConfirmation",
  background_job: "projectTree.status.backgroundJob",
  paused: "projectTree.status.paused",
  error: "projectTree.status.error",
  diverged_recovery: "projectTree.status.divergedRecovery",
};

export function normalizeTopicStatus(status?: string): ProjectTopicStatus | "" {
  if (!status) return "";
  if (status === "thinking" || status === "streaming" || status === "waiting_confirmation" || status === "background_job" || status === "paused" || status === "error" || status === "diverged_recovery") {
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
  if (asArray(node.children).some(projectTreeTopicArchiveBlocked)) return true;
  const status = normalizeTopicStatus(node.status);
  if (status === "thinking" || status === "streaming" || status === "waiting_confirmation" || status === "background_job") return true;
  if (status === "paused" || status === "error" || status === "diverged_recovery") return false;
  return Boolean(node.running);
}

export function topicStatusLabel(node: ProjectNode, t: Translator): string {
  const status = topicStatus(node);
  return status ? t(topicStatusLabels[status]) : "";
}

export function topicActivityAt(node: ProjectNode): number {
  return node.lastActivityAt || node.createdAt || 0;
}

export function projectTreeReadActivityKey(node: ProjectNode): string | null {
  const request = projectTreeTopicOpenRequest(node);
  if (!request?.topicId) return null;
  return [
    request.scope,
    request.workspaceRoot,
    request.topicId,
    request.sessionPath ?? "",
  ].join("\u001f");
}

export type ProjectTreeReadActivity = Record<string, number>;

export function projectTreeTopicHasUnreadActivity(
  node: ProjectNode,
  readActivity: ProjectTreeReadActivity,
  activeScope?: string,
  activeWorkspaceRoot?: string,
  activeTopicId?: string,
  activeSessionPath?: string,
): boolean {
  if (!isTopicNode(node) && !isRuntimeSessionNode(node)) return false;
  if (topicIsActive(node, activeScope, activeWorkspaceRoot, activeTopicId, activeSessionPath)) return false;
  if (topicStatus(node) !== "") return false;
  const key = projectTreeReadActivityKey(node);
  const activityAt = topicActivityAt(node);
  return Boolean(key && activityAt > 0 && (readActivity[key] ?? 0) < activityAt);
}

export function projectTreeShouldRenderTopicActions(isSessionNode: boolean, variant: ProjectTreeVariant, unread: boolean): boolean {
  return !isSessionNode && variant !== "creation" && !unread;
}

// Pinning reorders the classic/workbench trees shared with creation mode, so
// the creation context menu keeps its original rename/trash-only entries.
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
