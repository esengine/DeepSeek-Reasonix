// ProjectTree is the sidebar replacement for the flat recent-sessions list.
// It shows a tree of projects (each with expandable topics) plus a Global
// section. Clicking a topic opens its tab; "+" next to a project creates a
// new topic.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, DragEvent as ReactDragEvent, KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent } from "react";
import { Archive, ArrowDown, Pencil, Plus, Folder, FolderPlus, Search, BriefcaseBusiness, Copy, FolderOpen, XCircle, History, Check, ListCollapse, ListRestart, MessageSquare, Clock, Pin, MoreHorizontal, Minimize2, Maximize2 } from "lucide-react";
import { asArray } from "../lib/array";
import { useToast } from "../lib/toast";
import { app } from "../lib/bridge";
import type { ProjectNode, ProjectTopicStatus } from "../lib/types";
import { topicActivityTime } from "../lib/session";
import { getLocale, useT, type DictKey, type Translator } from "../lib/i18n";
import { PROJECT_COLOR_OPTIONS, projectColorValue } from "../lib/projectColors";
import { topicShortcutLabel, type TopicShortcutEntry } from "../lib/topicShortcuts";
import type { ShortcutPlatform } from "../lib/keyboardShortcuts";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";
import { Tooltip } from "./Tooltip";

interface ProjectTreeProps {
  activeScope?: string;
  activeWorkspaceRoot?: string;
  activeTopicId?: string;
  activeSessionPath?: string;
  imTopicSources?: Record<string, ProjectTreeImTopicSource>;
  variant?: "classic" | "workbench" | "creation";
  onOpenTopic: (scope: string, workspaceRoot: string, topicId: string, sessionPath?: string) => Promise<void> | void;
  onOpenProjectHistory: (scope: "global" | "project", workspaceRoot: string) => Promise<void> | void;
  onAddProject: () => Promise<void>;
  onCreateTopic?: (scope: string, workspaceRoot: string) => Promise<void> | void;
  onRenameTopic?: (topicId: string, title: string) => Promise<void> | void;
  onTopicsChanged?: () => Promise<void> | void;
  refreshSignal?: number;
  timeFilter: "all" | "10" | "20" | "1h" | "3h" | "5h" | "1d";
  onTimeFilterChange: (filter: "all" | "10" | "20" | "1h" | "3h" | "5h" | "1d") => void;
  searchExpanded?: boolean;
  searchFocusSignal?: number;
  showShortcutBadges?: boolean;
  shortcutPlatform?: ShortcutPlatform;
  onVisibleTopicsChange?: (topics: TopicShortcutEntry[]) => void;
}

type ProjectTreeImTopicSource = {
  platform?: string;
  label: string;
  title?: string;
  remoteId?: string;
};

function projectNodeKey(node: ProjectNode, depth: number): string {
  return node.key || `${node.kind}-${node.root ?? ""}-${node.topicId ?? ""}-${depth}`;
}

function isRuntimeSessionNode(node: ProjectNode): boolean {
  return node.kind === "session" || node.kind === "global_session";
}

function isTopicNode(node: ProjectNode): boolean {
  return node.kind === "topic" || node.kind === "global_topic";
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

type ProjectTreeTopicClickTarget = {
  rowKey: string;
  canRename: boolean;
};

type ProjectTreePendingTopicOpen = ProjectTreeTopicClickTarget & {
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

export function projectTreeFolderDisclosure(hasChildren: boolean, isExpanded: boolean): ProjectTreeFolderDisclosure {
  const canExpand = hasChildren;
  const isOpen = canExpand && isExpanded;
  return {
    canExpand,
    isOpen,
    ariaExpanded: canExpand ? isExpanded : undefined,
    iconStackClassName: `project-tree__icon-stack${canExpand ? " project-tree__icon-stack--expandable" : ""}`,
  };
}

function topicIsActive(node: ProjectNode, activeScope?: string, activeWorkspaceRoot?: string, activeTopicId?: string, activeSessionPath?: string): boolean {
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

function topicMetaLine(node: ProjectNode, t: Translator, compact = false): string {
  const parts: string[] = [];
  const turns = node.turns ?? 0;
  if (turns > 0) parts.push(t(turns === 1 ? "history.turnOne" : "history.turnOther", { n: turns }));
  const activityAt = node.lastActivityAt || node.createdAt || 0;
  if (activityAt) parts.push(topicActivityLabel(activityAt, t, compact));
  if (parts.length === 0) parts.push(t("projectTree.justNow"));
  return parts.join(" · ");
}

const topicStatusLabels: Record<ProjectTopicStatus, DictKey> = {
  thinking: "projectTree.status.thinking",
  streaming: "projectTree.status.streaming",
  waiting_confirmation: "projectTree.status.waitingConfirmation",
  background_job: "projectTree.status.backgroundJob",
  paused: "projectTree.status.paused",
  error: "projectTree.status.error",
};

function normalizeTopicStatus(status?: string): ProjectTopicStatus | "" {
  if (!status) return "";
  if (status === "thinking" || status === "streaming" || status === "waiting_confirmation" || status === "background_job" || status === "paused" || status === "error") {
    return status;
  }
  return "";
}

function topicStatus(node: ProjectNode): ProjectTopicStatus | "" {
  return normalizeTopicStatus(node.status) || (node.running ? "streaming" : "");
}

function topicStatusLabel(node: ProjectNode, t: Translator): string {
  const status = topicStatus(node);
  return status ? t(topicStatusLabels[status]) : "";
}

function topicActivityAt(node: ProjectNode): number {
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

type ProjectTreeReadActivity = Record<string, number>;

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

export function projectTreeShouldRenderTopicActions(isSessionNode: boolean, compactTopics: boolean, unread: boolean): boolean {
  return !isSessionNode && compactTopics && !unread;
}

function topicActivityLabel(ms: number, t: Translator, compact = false): string {
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
    return new Date(ms).toLocaleDateString();
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

function topicActivityDateLabel(ms: number): string {
  if (ms <= 0) return "";
  const locale = getLocale();
  const dateLocale = locale === "zh" ? "zh-CN" : locale === "zh-TW" ? "zh-TW" : "en";
  return new Date(ms).toLocaleDateString(dateLocale);
}

type ProjectDropPosition = "before" | "after";
type WorkbenchHeaderMenu = "more" | "add" | null;
type WorkbenchOrganizeMode = "project" | "recent" | "time";
type WorkbenchSortMode = "created" | "updated";

type CollapseSnapshot = {
  expanded: Set<string>;
  manuallyCollapsed: Set<string>;
};

type WorkbenchTreeSections = {
  pinned: ProjectNode[];
  projects: ProjectNode[];
};

const GLOBAL_PROJECT_ORDER_KEY = "__global__";
const WORKBENCH_ORGANIZE_KEY = "projectTree:workbenchOrganize";
const WORKBENCH_SORT_KEY = "projectTree:workbenchSort";
const READ_ACTIVITY_KEY = "projectTree:readActivity";
const READ_ACTIVITY_INIT_KEY = "projectTree:readActivityInitialized";

function loadReadActivity(): ProjectTreeReadActivity {
  try {
    const raw = localStorage.getItem(READ_ACTIVITY_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    const out: ProjectTreeReadActivity = {};
    for (const [key, value] of Object.entries(parsed)) {
      if (typeof value === "number" && Number.isFinite(value)) out[key] = value;
    }
    return out;
  } catch {
    return {};
  }
}

function saveReadActivity(readActivity: ProjectTreeReadActivity) {
  try {
    localStorage.setItem(READ_ACTIVITY_KEY, JSON.stringify(readActivity));
  } catch {
    /* localStorage unavailable */
  }
}

function loadWorkbenchOrganizeMode(): WorkbenchOrganizeMode {
  try {
    const value = localStorage.getItem(WORKBENCH_ORGANIZE_KEY);
    if (value === "recent" || value === "time") return value;
  } catch {
    /* localStorage unavailable */
  }
  return "project";
}

function loadWorkbenchSortMode(): WorkbenchSortMode {
  try {
    const value = localStorage.getItem(WORKBENCH_SORT_KEY);
    if (value === "created") return "created";
  } catch {
    /* localStorage unavailable */
  }
  return "updated";
}

function projectOrderKey(node: ProjectNode): string {
  if (node.kind === "global_folder") return GLOBAL_PROJECT_ORDER_KEY;
  if (node.kind === "project" && node.root) return node.root;
  return "";
}

function projectRoots(nodes: ProjectNode[]): string[] {
  return nodes
    .map(projectOrderKey)
    .filter((key) => key !== "");
}

function collapsibleFolderKeys(nodes: ProjectNode[], depth = 0): string[] {
  const keys: string[] = [];
  for (const node of nodes) {
    if (!node) continue;
    const children = asArray(node.children);
    if ((node.kind === "project" || node.kind === "global_folder") && children.length > 0) {
      keys.push(projectNodeKey(node, depth));
    }
    keys.push(...collapsibleFolderKeys(children, depth + 1));
  }
  return keys;
}

export function activeSessionAncestorKeys(
  nodes: ProjectNode[],
  activeScope?: string,
  activeWorkspaceRoot?: string,
  activeTopicId?: string,
  activeSessionPath?: string,
): string[] {
  const walk = (nodeList: ProjectNode[], ancestors: string[]): string[] | null => {
    for (const node of nodeList) {
      if (!node) continue;
      if (topicIsActive(node, activeScope, activeWorkspaceRoot, activeTopicId, activeSessionPath)) return ancestors;
      const children = asArray(node.children);
      if (children.length > 0) {
        const next = walk(children, [...ancestors, projectNodeKey(node, ancestors.length)]);
        if (next) return next;
      }
    }
    return null;
  };
  return walk(nodes, []) ?? [];
}

export function defaultExpandedProjectTreeKeys(
  nodes: ProjectNode[],
  activeScope?: string,
  activeWorkspaceRoot?: string,
  activeTopicId?: string,
  activeSessionPath?: string,
): string[] {
  return activeSessionAncestorKeys(nodes, activeScope, activeWorkspaceRoot, activeTopicId, activeSessionPath);
}

function reorderedProjectRoots(nodes: ProjectNode[], draggedRoot: string, targetRoot: string, position: ProjectDropPosition): string[] {
  const roots = projectRoots(nodes);
  if (draggedRoot === targetRoot || !roots.includes(draggedRoot) || !roots.includes(targetRoot)) return roots;
  const next = roots.filter((root) => root !== draggedRoot);
  const targetIndex = next.indexOf(targetRoot);
  if (targetIndex < 0) return roots;
  next.splice(position === "before" ? targetIndex : targetIndex + 1, 0, draggedRoot);
  return next;
}

function applyProjectOrder(nodes: ProjectNode[], roots: string[]): ProjectNode[] {
  const projectEntries = nodes
    .map((node): [string, ProjectNode] => [projectOrderKey(node), node])
    .filter(([key]) => key !== "");
  const byRoot = new Map<string, ProjectNode>(projectEntries);
  const orderedProjects = roots.map((root) => byRoot.get(root)).filter((node): node is ProjectNode => Boolean(node));
  const orderedKeys = new Set(roots);
  const nonProjects = nodes.filter((node) => !orderedKeys.has(projectOrderKey(node)));
  return [...nonProjects, ...orderedProjects];
}

function topicSortValue(node: ProjectNode, sortMode: WorkbenchSortMode): number {
  if (sortMode === "created") return node.createdAt || node.lastActivityAt || 0;
  return topicActivityTime(node);
}

function projectSortValue(node: ProjectNode, sortMode: WorkbenchSortMode): number {
  return asArray(node.children).reduce((max, child) => {
    if (!isTopicNode(child)) return max;
    return Math.max(max, topicSortValue(child, sortMode));
  }, 0);
}

function sortWorkbenchChildren(children: ProjectNode[], sortMode: WorkbenchSortMode): ProjectNode[] {
  return [...children].sort((a, b) => {
    if (!isTopicNode(a) || !isTopicNode(b)) return 0;
    if (Boolean(a.pinned) !== Boolean(b.pinned)) return a.pinned ? -1 : 1;
    return topicSortValue(b, sortMode) - topicSortValue(a, sortMode);
  });
}

function arrangeWorkbenchTree(nodes: ProjectNode[], organizeMode: WorkbenchOrganizeMode, sortMode: WorkbenchSortMode): ProjectNode[] {
  const arranged = nodes.map((node) => {
    if (node.kind !== "project" && node.kind !== "global_folder") return node;
    return { ...node, children: sortWorkbenchChildren(asArray(node.children), sortMode) };
  });
  if (organizeMode === "project") return arranged;
  const mode = organizeMode === "recent" ? "updated" : sortMode;
  return [...arranged].sort((a, b) => {
    if (Boolean(a.pinned) !== Boolean(b.pinned)) return a.pinned ? -1 : 1;
    return projectSortValue(b, mode) - projectSortValue(a, mode);
  });
}

function splitWorkbenchPinnedTree(nodes: ProjectNode[], sortMode: WorkbenchSortMode): WorkbenchTreeSections {
  const pinnedTopics: ProjectNode[] = [];
  const pinnedProjects: ProjectNode[] = [];
  const projects: ProjectNode[] = [];

  for (const node of nodes) {
    if (!node) continue;
    const isFolder = node.kind === "project" || node.kind === "global_folder";
    if (!isFolder) {
      if (node.pinned) pinnedTopics.push(node);
      else projects.push(node);
      continue;
    }

    if (node.pinned && node.kind === "project") {
      pinnedProjects.push(node);
      continue;
    }

    const children = asArray(node.children);
    const nextChildren: ProjectNode[] = [];
    for (const child of children) {
      if (isTopicNode(child) && child.pinned) {
        pinnedTopics.push(child);
        continue;
      }
      nextChildren.push(child);
    }
    projects.push({ ...node, children: nextChildren });
  }

  pinnedTopics.sort((a, b) => topicSortValue(b, sortMode) - topicSortValue(a, sortMode));
  pinnedProjects.sort((a, b) => projectSortValue(b, sortMode) - projectSortValue(a, sortMode));

  return {
    pinned: [...pinnedTopics, ...pinnedProjects],
    projects,
  };
}

// Global rows use the same project tree recipe; the fallback supplies their non-workspace accent.
function projectAccentStyle(color?: string, fallbackValue?: string): CSSProperties | undefined {
  const value = projectColorValue(color) || fallbackValue;
  if (!value) return undefined;
  return { "--project-accent": value } as CSSProperties;
}

function colorMenuLabel(label: string, color?: string, active = false) {
  const value = projectColorValue(color);
  return (
    <span className="project-tree__color-option">
      <span
        className="project-tree__color-swatch"
        style={value ? ({ "--project-accent": value } as CSSProperties) : undefined}
        aria-hidden="true"
      />
      <span>{label}</span>
      {active && <Check className="project-tree__color-check" size={12} />}
    </span>
  );
}

function menuLabelWithCheck(label: string, checked: boolean) {
  return (
    <span className="context-menu__label-with-check">
      <span className="context-menu__label-text">{label}</span>
      {checked && <Check className="context-menu__check" size={13} aria-hidden="true" />}
    </span>
  );
}

function revealLabelKey(platform: string): "projectTree.revealInFinder" | "projectTree.revealInExplorer" | "projectTree.revealInFileManager" {
  if (platform === "darwin") return "projectTree.revealInFinder";
  if (platform === "windows") return "projectTree.revealInExplorer";
  return "projectTree.revealInFileManager";
}

function projectColorLabel(t: Translator, color?: string): string {
  switch (color) {
    case "red": return t("projectTree.colorRed");
    case "orange": return t("projectTree.colorOrange");
    case "amber": return t("projectTree.colorAmber");
    case "green": return t("projectTree.colorGreen");
    case "teal": return t("projectTree.colorTeal");
    case "blue": return t("projectTree.colorBlue");
    case "purple": return t("projectTree.colorPurple");
    case "pink": return t("projectTree.colorPink");
    default: return t("projectTree.colorDefault");
  }
}

export function ProjectTree({
  activeScope,
  activeWorkspaceRoot,
  activeTopicId,
  activeSessionPath,
  imTopicSources = {},
  variant = "classic",
  onOpenTopic,
  onOpenProjectHistory,
  onAddProject,
  onCreateTopic,
  onRenameTopic,
  onTopicsChanged,
  refreshSignal,
  timeFilter,
  onTimeFilterChange,
  searchExpanded = true,
  searchFocusSignal = 0,
  showShortcutBadges = false,
  shortcutPlatform,
  onVisibleTopicsChange,
}: ProjectTreeProps) {
  const t = useT();
  const { showToast } = useToast();
  const compactTopics = variant === "workbench";
  const creationTopics = variant === "creation";
  const [tree, setTree] = useState<ProjectNode[]>([]);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [manuallyCollapsed, setManuallyCollapsed] = useState<Set<string>>(new Set());
  const [creatingProject, setCreatingProject] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [editingTopic, setEditingTopic] = useState<string | null>(null);
  const [topicDraft, setTopicDraft] = useState("");
  const [menuTopic, setMenuTopic] = useState<string | null>(null);
  const [menuProject, setMenuProject] = useState<{ key: string; root: string; path: string; scope: "global" | "project"; label: string } | null>(null);
  const [menuPoint, setMenuPoint] = useState<ContextMenuPoint | null>(null);
  const [editingProject, setEditingProject] = useState<{ key: string; root: string } | null>(null);
  const [projectDraft, setProjectDraft] = useState("");
  const [addingProject, setAddingProject] = useState(false);
  const [confirmAction, setConfirmAction] = useState<{ topicId: string; action: "trash" } | null>(null);
  const [confirmRemoveProject, setConfirmRemoveProject] = useState<string | null>(null);
  const [dragProjectRoot, setDragProjectRoot] = useState<string | null>(null);
  const [dropProject, setDropProject] = useState<{ root: string; position: ProjectDropPosition } | null>(null);
  const [collapseSnapshot, setCollapseSnapshot] = useState<CollapseSnapshot | null>(null);
  const [platform, setPlatform] = useState("");
  const [workbenchHeaderMenu, setWorkbenchHeaderMenu] = useState<WorkbenchHeaderMenu>(null);
  const [workbenchOrganizeMode, setWorkbenchOrganizeMode] = useState<WorkbenchOrganizeMode>(loadWorkbenchOrganizeMode);
  const [workbenchSortMode, setWorkbenchSortMode] = useState<WorkbenchSortMode>(loadWorkbenchSortMode);
  const [readActivity, setReadActivity] = useState<ProjectTreeReadActivity>(loadReadActivity);
  const filterRef = useRef<HTMLDivElement>(null);
  const filterTriggerRef = useRef<HTMLButtonElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const topicIndexRef = useRef(0);
  const visibleTopicsCollectorRef = useRef<TopicShortcutEntry[]>([]);
  const [filterMenuOpen, setFilterMenuOpen] = useState(false);
  const creatingRef = useRef(false);
  const clickTimerRef = useRef<ProjectTreePendingTopicOpen | null>(null);
  useEffect(() => {
    return () => {
      if (clickTimerRef.current !== null) clearTimeout(clickTimerRef.current.timer);
    };
  }, []);
  const manuallyCollapsedRef = useRef(manuallyCollapsed);

  const closeMenu = useCallback(() => {
    setMenuTopic(null);
    setMenuProject(null);
    setMenuPoint(null);
    setConfirmAction(null);
    setConfirmRemoveProject(null);
    setWorkbenchHeaderMenu(null);
  }, []);

  const updateManuallyCollapsed = useCallback((updater: (prev: Set<string>) => Set<string>) => {
    setManuallyCollapsed((prev) => {
      const next = updater(prev);
      manuallyCollapsedRef.current = next;
      return next;
    });
  }, []);

  const refresh = useCallback(async () => {
    try {
      const nodes = await app.ListProjectTree();
      const list = asArray(nodes);
      setTree(list);
      setExpanded((prev) => {
        const next = new Set(prev);
        const collapsed = manuallyCollapsedRef.current;
        for (const key of defaultExpandedProjectTreeKeys(list, activeScope, activeWorkspaceRoot, activeTopicId, activeSessionPath)) {
          if (!collapsed.has(key)) next.add(key);
        }
        return next;
      });
    } catch {
      /* bridge unavailable */
    }
  }, [activeScope, activeWorkspaceRoot, activeTopicId, activeSessionPath]);

  useEffect(() => {
    manuallyCollapsedRef.current = manuallyCollapsed;
  }, [manuallyCollapsed]);

  const searchVisible = searchExpanded || query.trim().length > 0;

  useEffect(() => {
    if (!searchVisible || searchFocusSignal <= 0) return;
    searchInputRef.current?.focus();
  }, [searchFocusSignal, searchVisible]);

  useEffect(() => {
    void refresh();
  }, [refresh, refreshSignal]);

  const markNodeRead = useCallback((node: ProjectNode) => {
    const key = projectTreeReadActivityKey(node);
    const activityAt = topicActivityAt(node);
    if (!key || activityAt <= 0) return;
    setReadActivity((prev) => {
      if ((prev[key] ?? 0) >= activityAt) return prev;
      const next = { ...prev, [key]: activityAt };
      saveReadActivity(next);
      return next;
    });
  }, []);

  useEffect(() => {
    if (tree.length === 0) return;
    try {
      if (localStorage.getItem(READ_ACTIVITY_INIT_KEY)) return;
    } catch {
      return;
    }
    const baseline: ProjectTreeReadActivity = {};
    const collectBaseline = (nodes: ProjectNode[]) => {
      for (const node of nodes) {
        if ((isTopicNode(node) || isRuntimeSessionNode(node)) && topicStatus(node) === "") {
          const key = projectTreeReadActivityKey(node);
          const activityAt = topicActivityAt(node);
          if (key && activityAt > 0) baseline[key] = Math.max(baseline[key] ?? 0, activityAt);
        }
        collectBaseline(asArray(node.children));
      }
    };
    collectBaseline(tree);
    try {
      localStorage.setItem(READ_ACTIVITY_INIT_KEY, "1");
    } catch {
      /* localStorage unavailable */
    }
    if (Object.keys(baseline).length === 0) return;
    setReadActivity((prev) => {
      const next = { ...prev };
      let changed = false;
      for (const [key, value] of Object.entries(baseline)) {
        if ((next[key] ?? 0) >= value) continue;
        next[key] = value;
        changed = true;
      }
      if (!changed) return prev;
      saveReadActivity(next);
      return next;
    });
  }, [tree]);

  useEffect(() => {
    const markActive = (nodes: ProjectNode[]) => {
      for (const node of nodes) {
        if (topicIsActive(node, activeScope, activeWorkspaceRoot, activeTopicId, activeSessionPath)) markNodeRead(node);
        markActive(asArray(node.children));
      }
    };
    markActive(tree);
  }, [activeScope, activeSessionPath, activeTopicId, activeWorkspaceRoot, markNodeRead, tree]);

  useEffect(() => {
    try {
      localStorage.setItem(WORKBENCH_ORGANIZE_KEY, workbenchOrganizeMode);
    } catch {
      /* ignore */
    }
  }, [workbenchOrganizeMode]);

  useEffect(() => {
    try {
      localStorage.setItem(WORKBENCH_SORT_KEY, workbenchSortMode);
    } catch {
      /* ignore */
    }
  }, [workbenchSortMode]);

  useEffect(() => {
    let cancelled = false;
    void app.Platform().then((value) => {
      if (!cancelled) setPlatform(value);
    }).catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  // Close the time-filter menu on outside click or Escape; move focus into the
  // menu on open and back to the trigger on Escape so it is keyboard-operable.
  useEffect(() => {
    if (!filterMenuOpen) return;
    const onMouseDown = (e: MouseEvent) => {
      if (filterRef.current && !filterRef.current.contains(e.target as Node)) setFilterMenuOpen(false);
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setFilterMenuOpen(false);
        filterTriggerRef.current?.focus();
      }
    };
    document.addEventListener("mousedown", onMouseDown);
    document.addEventListener("keydown", onKeyDown);
    const menu = filterRef.current?.querySelector<HTMLElement>(".project-tree__time-filter-menu");
    (menu?.querySelector<HTMLButtonElement>(".project-tree__time-filter-opt--on") ??
      menu?.querySelector<HTMLButtonElement>('[role="menuitem"]'))?.focus();
    return () => {
      document.removeEventListener("mousedown", onMouseDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [filterMenuOpen]);

  const moveMenuFocus = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "Home" && e.key !== "End") return;
    e.preventDefault();
    const items = Array.from(e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'));
    if (items.length === 0) return;
    const current = items.indexOf(document.activeElement as HTMLButtonElement);
    const next = e.key === "Home" ? 0
      : e.key === "End" ? items.length - 1
      : e.key === "ArrowDown" ? (current + 1 + items.length) % items.length
      : (current - 1 + items.length) % items.length;
    items[next]?.focus();
  };

  const toggleExpand = (key: string) => {
    const willCollapse = expanded.has(key);
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
    updateManuallyCollapsed((prev) => {
      const next = new Set(prev);
      if (willCollapse) next.add(key);
      else next.delete(key);
      return next;
    });
  };

  const folderKeys = useMemo(() => collapsibleFolderKeys(tree), [tree]);
  const searchActive = query.trim().length > 0;
  const hasExpandedFolders = !searchActive && folderKeys.some((key) => expanded.has(key));
  const canRestoreCollapsedView = collapseSnapshot !== null;
  const canToggleCollapsedView = !searchActive && folderKeys.length > 0 && (hasExpandedFolders || canRestoreCollapsedView);
  const collapseToggleLabel = t(canRestoreCollapsedView ? "projectTree.restoreCollapsedTooltip" : "projectTree.collapseAllTooltip");
  const workbenchCollapseToggleLabel = t(canRestoreCollapsedView ? "projectTree.restoreCollapsedWorkbench" : "projectTree.collapseAllWorkbench");

  const toggleCollapsedView = useCallback(() => {
    if (searchActive || folderKeys.length === 0) return;
    if (collapseSnapshot) {
      const currentFolderKeys = new Set(folderKeys);
      setExpanded(() => {
        const next = new Set<string>();
        for (const key of collapseSnapshot.expanded) {
          if (currentFolderKeys.has(key)) next.add(key);
        }
        return next;
      });
      updateManuallyCollapsed(() => {
        const next = new Set<string>();
        for (const key of collapseSnapshot.manuallyCollapsed) {
          if (currentFolderKeys.has(key)) next.add(key);
        }
        return next;
      });
      setCollapseSnapshot(null);
      return;
    }
    if (!hasExpandedFolders) return;
    setCollapseSnapshot({
      expanded: new Set(expanded),
      manuallyCollapsed: new Set(manuallyCollapsed),
    });
    setExpanded((prev) => {
      let changed = false;
      const next = new Set(prev);
      for (const key of folderKeys) {
        if (next.delete(key)) changed = true;
      }
      return changed ? next : prev;
    });
    updateManuallyCollapsed((prev) => {
      let changed = false;
      const next = new Set(prev);
      for (const key of folderKeys) {
        if (!next.has(key)) {
          next.add(key);
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, [collapseSnapshot, expanded, folderKeys, hasExpandedFolders, manuallyCollapsed, searchActive, updateManuallyCollapsed]);

  const handleAddProject = async () => {
    if (addingProject) return;
    setAddingProject(true);
    try {
      await onAddProject();
      await refresh();
    } finally {
      setAddingProject(false);
    }
  };

  const openWorkbenchHeaderMenu = (
    event: ReactMouseEvent<HTMLElement> | ReactKeyboardEvent<HTMLElement>,
    menu: Exclude<WorkbenchHeaderMenu, null>,
  ) => {
    event.preventDefault();
    event.stopPropagation();
    setMenuTopic(null);
    setMenuProject(null);
    setConfirmAction(null);
    setConfirmRemoveProject(null);
    setFilterMenuOpen(false);
    setMenuPoint(contextMenuPointFromEvent(event));
    setWorkbenchHeaderMenu((value) => (value === menu ? null : menu));
  };

  const handleCreateTopic = async (scope: string, workspaceRoot: string, key: string) => {
    if (creatingRef.current) return;
    creatingRef.current = true;
    setCreatingProject(key);
    setMenuProject(null);
    setMenuPoint(null);
    setExpanded((prev) => {
      const next = new Set(prev);
      next.add(key);
      return next;
    });
    updateManuallyCollapsed((prev) => {
      if (!prev.has(key)) return prev;
      const next = new Set(prev);
      next.delete(key);
      return next;
    });
    try {
      if (onCreateTopic) {
        await onCreateTopic(scope, workspaceRoot);
        await refresh();
        await onTopicsChanged?.();
        return;
      }
      const topic = await app.CreateTopic(scope, workspaceRoot, "");
      await refresh();
      await onTopicsChanged?.();
      await onOpenTopic(scope, workspaceRoot, topic.id);
    } catch {
      /* ignore */
    } finally {
      creatingRef.current = false;
      setCreatingProject(null);
    }
  };

  const startRenameTopic = (node: ProjectNode, label: string) => {
    setMenuTopic(null);
    setMenuProject(null);
    setMenuPoint(null);
    setConfirmAction(null);
    setEditingTopic(node.topicId ?? null);
    setTopicDraft(label);
  };

  const startRenameProject = (key: string, root: string, label: string) => {
    setMenuProject(null);
    setMenuTopic(null);
    setMenuPoint(null);
    setConfirmRemoveProject(null);
    setEditingProject({ key, root });
    setProjectDraft(label);
  };

  const commitRenameTopic = async (topicId: string) => {
    const title = topicDraft.trim();
    setEditingTopic(null);
    if (!title) return;
    try {
      if (onRenameTopic) await onRenameTopic(topicId, title);
      else await app.RenameTopic(topicId, title);
      await refresh();
      if (!onRenameTopic) await onTopicsChanged?.();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    }
  };

  const commitRenameProject = async (root: string) => {
    const title = projectDraft.trim();
    setEditingProject(null);
    if (!title) return;
    try {
      await app.RenameProject(root, title);
      await refresh();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    }
  };

  const trashTopic = async (topicId: string) => {
    try {
      await app.TrashTopic(topicId);
      setMenuTopic(null);
      setMenuPoint(null);
      setConfirmAction(null);
      await refresh();
      await onTopicsChanged?.();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    }
  };

  const setTopicPinned = async (topicId: string, pinned: boolean) => {
    try {
      await app.SetTopicPinned(topicId, pinned);
      setMenuTopic(null);
      setMenuPoint(null);
      await refresh();
      await onTopicsChanged?.();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    }
  };

  const setProjectPinned = async (workspaceRoot: string, pinned: boolean) => {
    if (!workspaceRoot) return;
    try {
      await app.SetProjectPinned(workspaceRoot, pinned);
      setMenuProject(null);
      setMenuPoint(null);
      await refresh();
      await onTopicsChanged?.();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    }
  };

  const copyProjectPath = async (path: string) => {
    if (!path) return;
    try {
      await navigator.clipboard?.writeText(path);
    } catch {
      /* ignore */
    }
  };

  const removeProject = async (path: string) => {
    if (!path) return;
    try {
      await app.RemoveWorkspace(path);
      setMenuProject(null);
      setMenuPoint(null);
      setConfirmRemoveProject(null);
      await refresh();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    }
  };

  const setProjectColor = async (path: string, color: string) => {
    try {
      await app.SetProjectColor(path, color);
      setMenuProject(null);
      setMenuPoint(null);
      await refresh();
      await onTopicsChanged?.();
    } catch {
      /* ignore */
    }
  };

  const visibleTree = useMemo(() => {
    const q = query.trim().toLowerCase();
    // Time filter: compute cutoff timestamp.
    const diff = timeFilter === "1h" ? 60 * 60 * 1000
      : timeFilter === "3h" ? 3 * 60 * 60 * 1000
      : timeFilter === "5h" ? 5 * 60 * 60 * 1000
      : timeFilter === "1d" ? 24 * 60 * 60 * 1000
      : 0;
    const nthLatestActivity = (n: number): number | null => {
      const times = new Set<number>();
      const collect = (nodes: ProjectNode[]) => {
        for (const node of nodes) {
          if (node.kind === "topic" || node.kind === "global_topic") times.add(topicActivityTime(node));
          collect(asArray(node.children));
        }
      };
      collect(tree);
      const sorted = [...times].sort((a, b) => b - a);
      return sorted.length === 0 ? null : sorted[Math.min(n, sorted.length) - 1];
    };
    const cutoff: number | null = timeFilter === "all" ? null
      : timeFilter === "10" ? nthLatestActivity(10)
      : timeFilter === "20" ? nthLatestActivity(20)
      : Date.now() - diff;
    const topicMatchesTime = (node: ProjectNode) => {
      if (cutoff === null) return true;
      return topicActivityTime(node) >= cutoff;
    };
    const matchesQuery = (node: ProjectNode) =>
      [node.label, node.root, node.topicId].some((value) => (value ?? "").toLowerCase().includes(q));
    const filterNode = (node: ProjectNode): ProjectNode | null => {
      // For folder nodes: always show when time filter is active (so the tree structure remains navigable).
      const isFolder = node.kind === "project" || node.kind === "global_folder";
      const children = asArray(node.children)
        .map(filterNode)
        .filter((child): child is ProjectNode => child !== null);
      if (isFolder) {
        if (cutoff !== null && children.length === 0 && !matchesQuery(node) && q === "") return null;
        if (children.length > 0 || matchesQuery(node)) return { ...node, children };
        if (q) return null;
        // With only time filter, show folder if it has any child that matches the time.
        const hasTimeMatch = asArray(node.children).some((c) => topicMatchesTime(c));
        return hasTimeMatch ? { ...node, children: asArray(node.children).filter(topicMatchesTime) } : null;
      }
      if (!q && cutoff === null) return node;
      if (cutoff !== null && !topicMatchesTime(node)) return null;
      if (q && !matchesQuery(node)) return null;
      return node;
    };
    const filtered = tree
      .map(filterNode)
      .filter((node): node is ProjectNode => node !== null);
    return compactTopics ? arrangeWorkbenchTree(filtered, workbenchOrganizeMode, workbenchSortMode) : filtered;
  }, [compactTopics, query, tree, timeFilter, workbenchOrganizeMode, workbenchSortMode]);

  const workbenchTreeSections = useMemo<WorkbenchTreeSections>(() => {
    if (!compactTopics) return { pinned: [], projects: visibleTree };
    return splitWorkbenchPinnedTree(visibleTree, workbenchSortMode);
  }, [compactTopics, visibleTree, workbenchSortMode]);

  const projectDragEnabled = query.trim() === "";

  const commitProjectReorder = useCallback(async (draggedRoot: string, targetRoot: string, position: ProjectDropPosition) => {
    const nextRoots = reorderedProjectRoots(tree, draggedRoot, targetRoot, position);
    const currentRoots = projectRoots(tree);
    if (nextRoots.join("\n") === currentRoots.join("\n")) return;
    setTree((current) => applyProjectOrder(current, nextRoots));
    try {
      await app.ReorderProjects(nextRoots);
      await refresh();
      await onTopicsChanged?.();
    } catch {
      await refresh();
    }
  }, [onTopicsChanged, refresh, tree]);

  const clearProjectDrag = useCallback(() => {
    setDragProjectRoot(null);
    setDropProject(null);
  }, []);

  useEffect(() => {
    if (!dragProjectRoot) return;
    window.addEventListener("dragend", clearProjectDrag);
    window.addEventListener("drop", clearProjectDrag);
    window.addEventListener("blur", clearProjectDrag);
    return () => {
      window.removeEventListener("dragend", clearProjectDrag);
      window.removeEventListener("drop", clearProjectDrag);
      window.removeEventListener("blur", clearProjectDrag);
    };
  }, [clearProjectDrag, dragProjectRoot]);

  const activeAncestorKeys = useMemo(
    () => activeSessionAncestorKeys(tree, activeScope, activeWorkspaceRoot, activeTopicId, activeSessionPath),
    [activeScope, activeSessionPath, activeTopicId, activeWorkspaceRoot, tree],
  );

  useEffect(() => {
    if (activeAncestorKeys.length === 0) return;
    setExpanded((prev) => {
      let changed = false;
      const next = new Set(prev);
      for (const key of activeAncestorKeys) {
        if (manuallyCollapsed.has(key) || next.has(key)) continue;
        next.add(key);
        changed = true;
      }
      return changed ? next : prev;
    });
  }, [activeAncestorKeys, manuallyCollapsed]);

  const renderNode = (node: ProjectNode | null | undefined, depth: number, section: "pinned" | "projects" = "projects", isVisible = true) => {
    if (!node) return null;
    const key = projectNodeKey(node, depth);
    const children = asArray(node.children);
    const isExpanded = query.trim() ? true : expanded.has(key);
    const hasChildren = children.length > 0;
    const folderDisclosure = projectTreeFolderDisclosure(hasChildren, isExpanded);

    if (isTopicNode(node) || isRuntimeSessionNode(node)) {
      const isSessionNode = isRuntimeSessionNode(node);
      const openRequest = projectTreeTopicOpenRequest(node);
      const scope = openRequest?.scope ?? "project";
      const scopeClass = scope === "global" ? " project-tree__topic--global" : " project-tree__topic--project";
      const accentStyle = projectAccentStyle(node.projectColor, scope === "global" ? "var(--project-tree-global-accent)" : undefined);
      const active = topicIsActive(node, activeScope, activeWorkspaceRoot, activeTopicId, activeSessionPath);
      const label = (node.label || node.topicId || "Untitled").replace(/^●\s*/, "");
      const activityAt = node.lastActivityAt || node.createdAt || 0;
      const sideTimeVisible = compactTopics || creationTopics;
      const timeLabel = sideTimeVisible && activityAt ? topicActivityLabel(activityAt, t, true) : "";
      const exactTimeLabel = sideTimeVisible && activityAt ? topicActivityDateLabel(activityAt) : "";
      const meta = topicMetaLine(node, t, compactTopics);
      const status = topicStatus(node);
      const statusLabel = topicStatusLabel(node, t);
      const showStatusInSide = status === "thinking" || status === "streaming" || status === "waiting_confirmation" || status === "background_job";
      const unread = projectTreeTopicHasUnreadActivity(node, readActivity, activeScope, activeWorkspaceRoot, activeTopicId, activeSessionPath);
      const topicId = node.topicId ?? "";
      const imSource = scope === "global" && topicId ? imTopicSources[topicId] : undefined;
      const imSourceLabel = imSource?.label || "";
      const imSourceTitle = imSourceLabel ? t("msg.fromIm", { source: imSourceLabel }) : "";
      const imSourcePlatform = (imSource?.platform || "im").replace(/[^a-z0-9_-]/gi, "").toLowerCase() || "im";
      const title = [label, imSourceTitle, statusLabel, meta, exactTimeLabel].filter(Boolean).join(" · ");
      const topicMenuOpen = !isSessionNode && menuTopic === topicId;
      const pinned = Boolean(node.pinned);
      const pinLabel = t(pinned ? "projectTree.unpinTopic" : "projectTree.pinTopic");
      const openTopicMenu = (event: ReactMouseEvent<HTMLElement> | ReactKeyboardEvent<HTMLElement>) => {
        if (isSessionNode) return;
        event.preventDefault();
        event.stopPropagation();
        setMenuProject(null);
        setConfirmRemoveProject(null);
        setMenuPoint(contextMenuPointFromEvent(event));
        setMenuTopic(topicId);
        setConfirmAction(null);
      };
      const topicMenuItems: ContextMenuItem[] = [
        ...(compactTopics
          ? [
              {
                key: pinned ? "unpin" : "pin",
                icon: <Pin size={13} />,
                label: pinLabel,
                onSelect: () => void setTopicPinned(topicId, !pinned),
              },
            ]
          : []),
        {
          key: "rename",
          icon: <Pencil size={13} />,
          label: t("projectTree.renameTopic"),
          onSelect: () => startRenameTopic(node, label),
        },
        {
          key: "trash",
          icon: <Archive size={13} />,
          label: confirmAction?.topicId === topicId && confirmAction.action === "trash" ? t("history.confirmMoveToTrash") : t("history.moveToTrash"),
          danger: true,
          onSelect: () => {
            if (confirmAction?.topicId === topicId && confirmAction.action === "trash") void trashTopic(topicId);
            else setConfirmAction({ topicId, action: "trash" });
          },
        },
      ];
      if (!isSessionNode && editingTopic === topicId) {
        return (
          <div
            key={key}
            className={`project-tree__topic project-tree__topic--editing${active ? " project-tree__topic--active" : ""}${imSource ? " project-tree__topic--im-source" : ""}${meta ? " project-tree__topic--has-meta" : ""}`}
            style={{ paddingLeft: 14 + depth * 16 }}
          >
            <input
              autoFocus
              className="project-tree__topic-input"
              value={topicDraft}
              onChange={(event) => setTopicDraft(event.target.value)}
              onFocus={(event) => event.target.select()}
              onKeyDown={(event) => {
                if (event.key === "Enter") void commitRenameTopic(topicId);
                if (event.key === "Escape") setEditingTopic(null);
              }}
              onBlur={() => void commitRenameTopic(topicId)}
            />
          </div>
        );
      }
      const shortcutIndex = showShortcutBadges && isVisible && topicIndexRef.current < 9 ? topicIndexRef.current + 1 : 0;
      if (shortcutIndex > 0) topicIndexRef.current++;
      // Collect visible topics in render order for shortcut navigation
      if (openRequest && isVisible) {
        visibleTopicsCollectorRef.current.push({
          scope: openRequest.scope,
          workspaceRoot: openRequest.workspaceRoot,
          topicId: openRequest.topicId,
          sessionPath: openRequest.sessionPath,
        });
      }
      const row = (
        <div
          className={`project-tree__topic${scopeClass}${isSessionNode ? " project-tree__topic--session" : ""}${active ? " project-tree__topic--active" : ""}${node.running ? " project-tree__topic--running" : ""}${status ? ` project-tree__topic--status-${status}` : ""}${unread ? " project-tree__topic--unread" : ""}${!isSessionNode && pinned ? " project-tree__topic--pinned" : ""}${topicMenuOpen ? " project-tree__topic--menu-open" : ""}${sideTimeVisible && (timeLabel || showStatusInSide) ? " project-tree__topic--with-side" : meta ? " project-tree__topic--has-meta" : ""}${imSource ? " project-tree__topic--im-source" : ""}${shortcutIndex > 0 ? " project-tree__topic--show-shortcut" : ""}`}
          style={accentStyle}
          onContextMenu={isSessionNode ? undefined : openTopicMenu}
        >
          <button
            type="button"
            className="project-tree__topic-main"
            title={title}
            style={{ paddingLeft: 14 + depth * 16 }}
            onClick={() => {
              if (!openRequest) return;
              const nextClick = { rowKey: key, canRename: !isSessionNode };
              const pending = clickTimerRef.current;
              if (pending !== null) {
                clearTimeout(pending.timer);
                clickTimerRef.current = null;
                if (projectTreeShouldSuppressOpenForRename(pending, nextClick)) return;
              }
              const timer = setTimeout(() => {
                if (clickTimerRef.current?.timer === timer) clickTimerRef.current = null;
                markNodeRead(node);
                onOpenTopic(openRequest.scope, openRequest.workspaceRoot, openRequest.topicId, openRequest.sessionPath);
              }, 200);
              clickTimerRef.current = { ...nextClick, timer };
            }}
            onKeyDown={(event) => {
              if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) {
                openTopicMenu(event);
              }
            }}
            onDoubleClick={(event) => {
              if (isSessionNode) return;
              event.stopPropagation();
              if (clickTimerRef.current !== null && clickTimerRef.current.rowKey === key) {
                clearTimeout(clickTimerRef.current.timer);
                clickTimerRef.current = null;
              }
              startRenameTopic(node, label);
            }}
          >
            <span className="project-tree__topic-copy">
              <span className="project-tree__topic-heading">
                <span className="project-tree__topic-label">{label}</span>
                {imSource && (
                  <span
                    className={`project-tree__topic-im project-tree__topic-im--${imSourcePlatform}`}
                    title={imSourceTitle}
                    aria-label={imSourceTitle}
                  >
                    <MessageSquare size={11} />
                    <span>{imSourceLabel}</span>
                  </span>
                )}
                {!compactTopics && statusLabel && <span className={`project-tree__topic-status project-tree__topic-status--${status}`}>{statusLabel}</span>}
              </span>
              {!compactTopics && !creationTopics && meta && (
                <span className="project-tree__topic-meta">
                  <span className="project-tree__topic-meta-text">{meta}</span>
                </span>
              )}
            </span>
            {sideTimeVisible && (
              <span className={`project-tree__topic-side${!timeLabel && !showStatusInSide ? " project-tree__topic-side--empty" : ""}`} aria-hidden="true">
                {showStatusInSide && <span className={`project-tree__topic-state project-tree__topic-state--${status}`} title={statusLabel} />}
                {timeLabel && <span className="project-tree__topic-time">{timeLabel}</span>}
              </span>
            )}
            {compactTopics && statusLabel && (
              <span className="sr-only">
                {statusLabel}
              </span>
            )}
            {compactTopics && meta && (
              <span className="sr-only">
                {meta}
              </span>
            )}
          </button>
          {unread && <span className="project-tree__topic-unread-dot" aria-hidden="true" />}
          {projectTreeShouldRenderTopicActions(isSessionNode, compactTopics, unread) && (
            <span className="project-tree__topic-actions" aria-label={t("projectTree.topicActions")}>
              <Tooltip label={pinLabel} side="top" className="project-tree__topic-action-slot">
                <button
                  className={`project-tree__topic-action${pinned ? " project-tree__topic-action--pinned" : ""}`}
                  type="button"
                  aria-label={pinLabel}
                  aria-pressed={pinned}
                  onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    void setTopicPinned(topicId, !pinned);
                  }}
                >
                  <Pin size={15} aria-hidden="true" />
                </button>
              </Tooltip>
              <Tooltip label={t("projectTree.archiveTopic")} side="top" className="project-tree__topic-action-slot">
                <button
                  className="project-tree__topic-action project-tree__topic-action--archive"
                  type="button"
                  aria-label={t("projectTree.archiveTopic")}
                  onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    void trashTopic(topicId);
                  }}
                >
                  <Archive size={15} aria-hidden="true" />
                </button>
              </Tooltip>
            </span>
          )}
          {!isSessionNode && (
            <ContextMenu
              open={topicMenuOpen}
              point={menuPoint}
              items={topicMenuItems}
              minWidth={178}
              ariaLabel={t("projectTree.topicActions")}
              onClose={closeMenu}
            />
          )}
          {shortcutIndex > 0 && (
            <span className="project-tree__topic-shortcut" aria-hidden="true">
              {topicShortcutLabel(shortcutIndex, shortcutPlatform)}
            </span>
          )}
        </div>
      );
      return (
        <div key={key}>
          {row}
          {hasChildren && (
            <div className={`project-tree__children${isExpanded ? " project-tree__children--expanded" : ""}`}>
              <div className="project-tree__children-inner">
                {children.map((child) => renderNode(child, depth + 1, section, isVisible && isExpanded))}
              </div>
            </div>
          )}
        </div>
      );
    }

    const scope = node.kind === "global_folder" ? "global" : "project";
    const scopeClass = scope === "global" ? " project-tree__folder--global" : " project-tree__folder--project";
    const pinnedClass = node.pinned ? " project-tree__folder--pinned" : "";
    const accentStyle = projectAccentStyle(node.projectColor, scope === "global" ? "var(--project-tree-global-accent)" : undefined);
    const projectRoot = scope === "global" ? "" : node.root ?? "";
    const projectDragKey = scope === "global" ? GLOBAL_PROJECT_ORDER_KEY : projectRoot;
    const projectPath = node.root ?? "";
    const colorTargetRoot = scope === "global" ? "" : projectPath;
    const projectLabel = node.label || (scope === "global" ? "Global" : "Untitled");
    const projectPinned = Boolean(node.pinned);
    const projectActive = activeScope === scope && (scope === "global" || activeWorkspaceRoot === node.root);
    const projectMenuOpen = menuProject?.key === key;
    const activeTopicInProject = Boolean(activeTopicId) && activeScope === scope && (scope === "global" || activeWorkspaceRoot === projectRoot);
    const draggableProject = section !== "pinned" && projectDragEnabled && depth === 0 && Boolean(projectDragKey) && editingProject?.key !== key;
    const projectDropPosition = dropProject?.root === projectDragKey ? dropProject.position : null;
    const handleProjectDragStart = (event: ReactDragEvent<HTMLElement>) => {
      if (!draggableProject) return;
      const target = event.target;
      if (target instanceof Element && target.closest(".project-tree__action-slot,.project-tree__folder-action-slot")) {
        event.preventDefault();
        return;
      }
      event.dataTransfer.effectAllowed = "move";
      event.dataTransfer.setData("text/plain", projectDragKey);
      setDragProjectRoot(projectDragKey);
      setDropProject(null);
    };
    const handleProjectDragOver = (event: ReactDragEvent<HTMLDivElement>) => {
      if (!draggableProject || !dragProjectRoot || dragProjectRoot === projectDragKey) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = "move";
      const rect = event.currentTarget.getBoundingClientRect();
      const position: ProjectDropPosition = event.clientY < rect.top + rect.height / 2 ? "before" : "after";
      setDropProject((current) => {
        if (current?.root === projectDragKey && current.position === position) return current;
        return { root: projectDragKey, position };
      });
    };
    const handleProjectDrop = (event: ReactDragEvent<HTMLDivElement>) => {
      if (!draggableProject) return;
      const draggedRoot = dragProjectRoot || event.dataTransfer.getData("text/plain");
      const position = dropProject?.root === projectDragKey ? dropProject.position : "after";
      event.preventDefault();
      clearProjectDrag();
      if (draggedRoot && draggedRoot !== projectDragKey) void commitProjectReorder(draggedRoot, projectDragKey, position);
    };
    const openProjectMenu = (event: ReactMouseEvent<HTMLElement> | ReactKeyboardEvent<HTMLElement>) => {
      event.preventDefault();
      event.stopPropagation();
      setMenuTopic(null);
      setConfirmAction(null);
      setMenuPoint(contextMenuPointFromEvent(event));
      setMenuProject({ key, root: projectRoot, path: projectPath, scope, label: projectLabel });
      setConfirmRemoveProject(null);
    };
    const projectMenuItems: ContextMenuItem[] = [
      {
        key: "new-session",
        icon: <Plus size={13} />,
        label: t("projectTree.newTopic"),
        onSelect: () => {
          void handleCreateTopic(scope, projectRoot, key);
        },
      },
      ...(scope === "project"
        ? [
            {
              key: "project-history",
              icon: <History size={13} />,
              label: t("projectTree.projectHistory"),
              onSelect: () => {
                closeMenu();
                void onOpenProjectHistory(scope, projectRoot);
              },
            },
          ]
        : []),
      {
        key: "rename",
        icon: <Pencil size={13} />,
        label: t("projectTree.renameProject"),
        onSelect: () => startRenameProject(key, projectRoot, projectLabel),
      },
      { type: "separator" as const, key: "color-separator" },
      ...PROJECT_COLOR_OPTIONS.map((option): ContextMenuItem => ({
        key: `color-${option.key || "default"}`,
        label: colorMenuLabel(projectColorLabel(t, option.key), option.key, (node.projectColor || "") === option.key),
        onSelect: () => {
          void setProjectColor(colorTargetRoot, option.key);
        },
      })),
      { type: "separator" as const, key: "path-separator" },
      {
        key: "reveal",
        icon: <FolderOpen size={13} />,
        label: t(revealLabelKey(platform)),
        disabled: !projectPath,
        onSelect: () => {
          void app.RevealPath(projectPath).catch(() => {});
          closeMenu();
        },
      },
      {
        key: "copy-path",
        icon: <Copy size={13} />,
        label: t("projectTree.copyPath"),
        disabled: !projectPath,
        onSelect: () => {
          void copyProjectPath(projectPath);
          closeMenu();
        },
      },
      ...(scope === "project"
        ? [
            { type: "separator" as const, key: "remove-separator" },
            {
              key: "remove",
              icon: <XCircle size={13} />,
              label: confirmRemoveProject === key ? t("projectTree.confirmRemoveProject") : t("projectTree.removeProject"),
              danger: true,
              onSelect: () => {
                if (confirmRemoveProject === key) void removeProject(projectPath);
                else setConfirmRemoveProject(key);
              },
            },
          ]
        : []),
    ];
    const workbenchProjectMenuItems: ContextMenuItem[] = [
      ...(scope === "project"
        ? [
            {
              key: projectPinned ? "unpin-project" : "pin-project",
              icon: <Pin size={13} />,
              label: t(projectPinned ? "projectTree.unpinProject" : "projectTree.pinProject"),
              onSelect: () => {
                void setProjectPinned(projectRoot, !projectPinned);
              },
            },
          ]
        : []),
      {
        key: "reveal",
        icon: <FolderOpen size={13} />,
        label: t(revealLabelKey(platform)),
        disabled: !projectPath,
        onSelect: () => {
          void app.RevealPath(projectPath).catch(() => {});
          closeMenu();
        },
      },
      ...(scope === "project"
        ? [
            {
              key: "project-history",
              icon: <History size={13} />,
              label: t("projectTree.projectHistory"),
              onSelect: () => {
                closeMenu();
                void onOpenProjectHistory(scope, projectRoot);
              },
            },
          ]
        : []),
      {
        key: "rename",
        icon: <Pencil size={13} />,
        label: t("projectTree.renameProjectWorkbench"),
        onSelect: () => startRenameProject(key, projectRoot, projectLabel),
      },
      {
        key: "archive-active-topic",
        icon: <Archive size={13} />,
        label: activeTopicId && confirmAction?.topicId === activeTopicId && confirmAction.action === "trash"
          ? t("history.confirmMoveToTrash")
          : t("projectTree.archiveConversation"),
        disabled: !activeTopicInProject || !activeTopicId,
        danger: true,
        onSelect: () => {
          if (!activeTopicId) return;
          if (confirmAction?.topicId === activeTopicId && confirmAction.action === "trash") void trashTopic(activeTopicId);
          else setConfirmAction({ topicId: activeTopicId, action: "trash" });
        },
      },
      ...(scope === "project"
        ? [
            { type: "separator" as const, key: "remove-separator" },
            {
              key: "remove",
              icon: <XCircle size={13} />,
              label: confirmRemoveProject === key ? t("projectTree.confirmRemoveProjectShort") : t("projectTree.removeProjectShort"),
              danger: true,
              onSelect: () => {
                if (confirmRemoveProject === key) void removeProject(projectPath);
                else setConfirmRemoveProject(key);
              },
            },
          ]
        : []),
    ];

    if (editingProject?.key === key) {
      return (
        <div key={key} className="project-tree__project-wrapper">
          <div
            className={`project-tree__folder project-tree__folder--editing${projectActive ? " project-tree__folder--active" : ""}`}
            style={{ paddingLeft: 8 + depth * 16 }}
          >
            <input
              autoFocus
              className="project-tree__folder-input"
              value={projectDraft}
              onChange={(event) => setProjectDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void commitRenameProject(projectRoot);
                if (event.key === "Escape") setEditingProject(null);
              }}
              onBlur={() => void commitRenameProject(projectRoot)}
            />
          </div>
          {hasChildren && (
            <div className={`project-tree__children${isExpanded ? " project-tree__children--expanded" : ""}`}>
              <div className="project-tree__children-inner">
                {children.map((child) => renderNode(child, depth + 1, section, isVisible && isExpanded))}
              </div>
            </div>
          )}
        </div>
      );
    }

    return (
      <div key={key} className="project-tree__project-wrapper">
        <div
          className={`project-tree__folder${scopeClass}${pinnedClass}${draggableProject ? " project-tree__folder--draggable" : ""}${projectActive ? " project-tree__folder--active" : ""}${projectMenuOpen ? " project-tree__folder--menu-open" : ""}${dragProjectRoot === projectDragKey ? " project-tree__folder--dragging" : ""}${projectDropPosition ? ` project-tree__folder--drop-${projectDropPosition}` : ""}`}
          style={accentStyle}
          draggable={draggableProject}
          aria-grabbed={draggableProject ? dragProjectRoot === projectRoot : undefined}
          onDragStart={handleProjectDragStart}
          onDragOver={handleProjectDragOver}
          onDragLeave={(event) => {
            if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropProject(null);
          }}
          onDrop={handleProjectDrop}
          onDragEnd={clearProjectDrag}
          onContextMenu={openProjectMenu}
        >
          <button
            type="button"
            className="project-tree__folder-main"
            style={{ paddingLeft: 8 + depth * 16 }}
            onClick={() => {
              if (folderDisclosure.canExpand) toggleExpand(key);
            }}
            onKeyDown={(event) => {
              if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) {
                openProjectMenu(event);
              }
            }}
            aria-expanded={folderDisclosure.ariaExpanded}
          >
            <span className={folderDisclosure.iconStackClassName}>
              {folderDisclosure.isOpen ? <FolderOpen size={14} className="project-tree__folder-icon" /> : <Folder size={14} className="project-tree__folder-icon" />}
            </span>
            <span className="project-tree__folder-color" aria-hidden="true" />
            <span className={`project-tree__folder-label${!hasChildren ? " project-tree__folder-label--empty" : ""}`}>{projectLabel}</span>
          </button>
          {compactTopics && (
            <Tooltip label={t("projectTree.projectActions")} className="project-tree__folder-action-slot">
              <button
                type="button"
                className="project-tree__folder-action project-tree__folder-action--menu"
                aria-label={t("projectTree.projectActions")}
                aria-haspopup="menu"
                aria-expanded={projectMenuOpen}
                onClick={(e) => {
                  openProjectMenu(e);
                }}
              >
                <MoreHorizontal size={16} aria-hidden="true" />
              </button>
            </Tooltip>
          )}
          <Tooltip label={t("projectTree.newTopicTooltip")} className={compactTopics ? "project-tree__folder-action-slot" : "project-tree__action-slot"}>
            <button
              type="button"
              className={compactTopics
                ? `project-tree__folder-action project-tree__folder-action--create${creatingProject === key ? " project-tree__folder-action--active" : ""}`
                : `project-tree__new-topic${creatingProject === key ? " project-tree__new-topic--active" : ""}`}
              aria-label={t("projectTree.newTopicTooltip")}
              disabled={creatingProject !== null}
              onClick={(e) => {
                e.stopPropagation();
                void handleCreateTopic(scope, projectRoot, key);
              }}
            >
              {compactTopics ? <Plus size={15} aria-hidden="true" /> : <Plus size={12} aria-hidden="true" />}
            </button>
          </Tooltip>
          <ContextMenu
            open={projectMenuOpen}
            point={menuPoint}
            items={compactTopics ? workbenchProjectMenuItems : projectMenuItems}
            minWidth={compactTopics ? 206 : 212}
            ariaLabel={t("projectTree.projectActions")}
            onClose={closeMenu}
          />
        </div>
        {hasChildren && (
          <div className={`project-tree__children${isExpanded ? " project-tree__children--expanded" : ""}`}>
            <div className="project-tree__children-inner">
              {children.map((child) => renderNode(child, depth + 1, section, isVisible && isExpanded))}
            </div>
          </div>
        )}
      </div>
    );
  };

  const workbenchHeaderMoreItems: ContextMenuItem[] = [
    {
      key: "archive-all",
      icon: <Archive size={13} />,
      label: t("projectTree.archiveAllConversations"),
      disabled: true,
      onSelect: () => {},
    },
    { type: "separator", key: "organize-separator" },
    {
      key: "organize-heading",
      icon: <Folder size={13} />,
      label: t("projectTree.organizeSidebar"),
      disabled: true,
      variant: "section",
      onSelect: () => {},
    },
    {
      key: "organize-project",
      icon: <Folder size={13} />,
      label: menuLabelWithCheck(t("projectTree.organizeByProject"), workbenchOrganizeMode === "project"),
      onSelect: () => {
        setWorkbenchOrganizeMode("project");
        closeMenu();
      },
    },
    {
      key: "organize-recent",
      icon: <Folder size={13} />,
      label: menuLabelWithCheck(t("projectTree.organizeRecentProjects"), workbenchOrganizeMode === "recent"),
      onSelect: () => {
        setWorkbenchOrganizeMode("recent");
        closeMenu();
      },
    },
    {
      key: "organize-time",
      icon: <Clock size={13} />,
      label: menuLabelWithCheck(t("projectTree.organizeByTime"), workbenchOrganizeMode === "time"),
      onSelect: () => {
        setWorkbenchOrganizeMode("time");
        closeMenu();
      },
    },
    {
      key: "move-section-down",
      icon: <ArrowDown size={13} />,
      label: t("projectTree.moveSectionDown"),
      disabled: true,
      onSelect: () => {},
    },
    { type: "separator", key: "sort-separator" },
    {
      key: "sort-heading",
      icon: <Clock size={13} />,
      label: t("projectTree.sortCriteria"),
      disabled: true,
      variant: "section",
      onSelect: () => {},
    },
    {
      key: "sort-created",
      icon: <Clock size={13} />,
      label: menuLabelWithCheck(t("projectTree.sortByCreatedAt"), workbenchSortMode === "created"),
      onSelect: () => {
        setWorkbenchSortMode("created");
        closeMenu();
      },
    },
    {
      key: "sort-updated",
      icon: <Pencil size={13} />,
      label: menuLabelWithCheck(t("projectTree.sortByUpdatedAt"), workbenchSortMode === "updated"),
      onSelect: () => {
        setWorkbenchSortMode("updated");
        closeMenu();
      },
    },
  ];

  const workbenchHeaderAddItems: ContextMenuItem[] = [
    {
      key: "blank-project",
      icon: <FolderPlus size={13} />,
      label: t("projectTree.createBlankProject"),
      disabled: true,
      onSelect: () => {},
    },
    {
      key: "existing-folder",
      icon: <FolderPlus size={13} />,
      label: t("projectTree.useExistingFolder"),
      disabled: addingProject,
      onSelect: () => {
        closeMenu();
        void handleAddProject();
      },
    },
  ];

  const timeFilterBadge = timeFilter !== "all" ? (timeFilter === "1d" ? "24h" : timeFilter) : "";
  const timeFilterDisplayLabel = timeFilter === "all" ? t("projectTree.timeFilterAll")
    : timeFilter === "10" ? t("projectTree.timeFilter10")
    : timeFilter === "20" ? t("projectTree.timeFilter20")
    : timeFilter === "1h" ? t("projectTree.timeFilter1h")
    : timeFilter === "3h" ? t("projectTree.timeFilter3h")
    : timeFilter === "5h" ? t("projectTree.timeFilter5h")
    : t("projectTree.timeFilter1d");
  const renderTimeFilterControl = (mode: "classic" | "workbench") => {
    const workbench = mode === "workbench";
    const active = timeFilter !== "all";
    const controlLabel = workbench ? `${t("projectTree.timeFilter")}: ${timeFilterDisplayLabel}` : t("projectTree.timeFilter");
    const buttonClassName = workbench
      ? `project-tree__header-icon-btn project-tree__header-icon-btn--filter${active ? " project-tree__header-icon-btn--active" : ""}`
      : `project-tree__header-action-btn${active ? " project-tree__header-action-btn--active" : ""}`;
    return (
      <Tooltip
        label={controlLabel}
        className={`project-tree__action-slot project-tree__header-action-slot project-tree__header-action-slot--filter${workbench ? " project-tree__header-action-slot--workbench-filter" : ""}`}
      >
        <div ref={filterRef} className="project-tree__time-filter">
          <button
            ref={filterTriggerRef}
            type="button"
            className={buttonClassName}
            aria-label={controlLabel}
            aria-haspopup="menu"
            aria-expanded={filterMenuOpen}
            onClick={() => {
              setWorkbenchHeaderMenu(null);
              setMenuPoint(null);
              setFilterMenuOpen(!filterMenuOpen);
            }}
          >
            <Clock size={workbench ? 15 : 14} aria-hidden="true" />
            {timeFilterBadge && (
              <span className="project-tree__time-filter-label">
                {timeFilterBadge}
              </span>
            )}
          </button>
          {filterMenuOpen && (
            <div className="project-tree__time-filter-menu" role="menu" aria-label={t("projectTree.timeFilter")} onKeyDown={moveMenuFocus}>
              <button
                type="button"
                className={`project-tree__time-filter-opt${timeFilter === "all" ? " project-tree__time-filter-opt--on" : ""}`}
                onClick={() => { onTimeFilterChange("all"); setFilterMenuOpen(false); }}
                role="menuitem"
              >
                {t("projectTree.timeFilterAll")}
              </button>
              <div className="project-tree__time-filter-sep" role="separator" />
              <button
                type="button"
                className={`project-tree__time-filter-opt${timeFilter === "10" ? " project-tree__time-filter-opt--on" : ""}`}
                onClick={() => { onTimeFilterChange("10"); setFilterMenuOpen(false); }}
                role="menuitem"
              >
                {t("projectTree.timeFilter10")}
              </button>
              <button
                type="button"
                className={`project-tree__time-filter-opt${timeFilter === "20" ? " project-tree__time-filter-opt--on" : ""}`}
                onClick={() => { onTimeFilterChange("20"); setFilterMenuOpen(false); }}
                role="menuitem"
              >
                {t("projectTree.timeFilter20")}
              </button>
              <div className="project-tree__time-filter-sep" role="separator" />
              <button
                type="button"
                className={`project-tree__time-filter-opt${timeFilter === "1h" ? " project-tree__time-filter-opt--on" : ""}`}
                onClick={() => { onTimeFilterChange("1h"); setFilterMenuOpen(false); }}
                role="menuitem"
              >
                {t("projectTree.timeFilter1h")}
              </button>
              <button
                type="button"
                className={`project-tree__time-filter-opt${timeFilter === "3h" ? " project-tree__time-filter-opt--on" : ""}`}
                onClick={() => { onTimeFilterChange("3h"); setFilterMenuOpen(false); }}
                role="menuitem"
              >
                {t("projectTree.timeFilter3h")}
              </button>
              <button
                type="button"
                className={`project-tree__time-filter-opt${timeFilter === "5h" ? " project-tree__time-filter-opt--on" : ""}`}
                onClick={() => { onTimeFilterChange("5h"); setFilterMenuOpen(false); }}
                role="menuitem"
              >
                {t("projectTree.timeFilter5h")}
              </button>
              <button
                type="button"
                className={`project-tree__time-filter-opt${timeFilter === "1d" ? " project-tree__time-filter-opt--on" : ""}`}
                onClick={() => { onTimeFilterChange("1d"); setFilterMenuOpen(false); }}
                role="menuitem"
              >
                {t("projectTree.timeFilter1d")}
              </button>
            </div>
          )}
        </div>
      </Tooltip>
    );
  };

  const renderProjectHeader = (mode: "classic" | "workbench") => (
    <div className="project-tree__header">
      <span className="project-tree__header-title">
        <BriefcaseBusiness className="project-tree__header-icon" size={13} />
        {t("projectTree.workspaceTitle")}
      </span>
      <span className="project-tree__header-actions">
        {mode === "workbench" ? (
          <>
            {renderTimeFilterControl("workbench")}
            <Tooltip label={workbenchCollapseToggleLabel} className="project-tree__header-action-slot">
              <button
                type="button"
                className="project-tree__header-icon-btn"
                aria-label={workbenchCollapseToggleLabel}
                disabled={!canToggleCollapsedView}
                onClick={toggleCollapsedView}
              >
                {canRestoreCollapsedView ? <Maximize2 size={15} aria-hidden="true" /> : <Minimize2 size={15} aria-hidden="true" />}
              </button>
            </Tooltip>
            <span className="project-tree__header-menu-wrap">
              <Tooltip label={t("projectTree.moreActions")} className="project-tree__header-action-slot">
                <button
                  type="button"
                  className={`project-tree__header-icon-btn${workbenchHeaderMenu === "more" ? " project-tree__header-icon-btn--active" : ""}`}
                  aria-label={t("projectTree.moreActions")}
                  aria-haspopup="menu"
                  aria-expanded={workbenchHeaderMenu === "more"}
                  onClick={(event) => {
                    openWorkbenchHeaderMenu(event, "more");
                  }}
                >
                  <MoreHorizontal size={16} aria-hidden="true" />
                </button>
              </Tooltip>
              <ContextMenu
                open={workbenchHeaderMenu === "more"}
                point={menuPoint}
                items={workbenchHeaderMoreItems}
                minWidth={222}
                ariaLabel={t("projectTree.moreActions")}
                onClose={closeMenu}
              />
            </span>
            <span className="project-tree__header-menu-wrap">
              <Tooltip label={t("projectTree.addProjectTooltip")} className="project-tree__header-action-slot">
                <button
                  type="button"
                  className={`project-tree__header-icon-btn${workbenchHeaderMenu === "add" ? " project-tree__header-icon-btn--active" : ""}`}
                  aria-label={t("projectTree.addProjectTooltip")}
                  aria-haspopup="menu"
                  aria-expanded={workbenchHeaderMenu === "add"}
                  disabled={addingProject}
                  onClick={(event) => {
                    openWorkbenchHeaderMenu(event, "add");
                  }}
                >
                  <FolderPlus size={16} aria-hidden="true" />
                </button>
              </Tooltip>
              <ContextMenu
                open={workbenchHeaderMenu === "add"}
                point={menuPoint}
                items={workbenchHeaderAddItems}
                minWidth={206}
                ariaLabel={t("projectTree.addProjectTooltip")}
                onClose={closeMenu}
              />
            </span>
          </>
        ) : (
          <>
            {renderTimeFilterControl("classic")}
            <Tooltip label={collapseToggleLabel} className="project-tree__action-slot project-tree__header-action-slot project-tree__action-slot--collapse">
              <button
                type="button"
                className={`project-tree__collapse-all${canRestoreCollapsedView ? " project-tree__collapse-all--restore" : ""}`}
                aria-label={collapseToggleLabel}
                aria-pressed={canRestoreCollapsedView}
                disabled={!canToggleCollapsedView}
                onClick={toggleCollapsedView}
              >
                {canRestoreCollapsedView ? <ListRestart size={14} /> : <ListCollapse size={14} />}
              </button>
            </Tooltip>
            <Tooltip label={t("projectTree.addProjectTooltip")} className="project-tree__action-slot project-tree__header-action-slot project-tree__action-slot--add">
              <button
                type="button"
                className="project-tree__add-project"
                aria-label={t("projectTree.addProjectTooltip")}
                disabled={addingProject}
                onClick={() => void handleAddProject()}
              >
                <FolderPlus size={14} />
              </button>
            </Tooltip>
          </>
        )}
      </span>
    </div>
  );

  const renderEmptyState = () => {
    if (query.trim()) return <div className="project-tree__empty">{t("projectTree.emptyNoMatch")}</div>;
    if (timeFilter !== "all") {
      return (
        <div className="project-tree__empty">{t("projectTree.emptyNoTimeFilterMatch")}
          <button
            type="button"
            className="project-tree__empty-primary"
            onClick={() => onTimeFilterChange("all")}
          >
            {t("projectTree.clearTimeFilter")}
          </button>
        </div>
      );
    }
    return (
      <div className="project-tree__empty-state">
        <div className="project-tree__empty project-tree__empty--subtle">{t("projectTree.emptyNoProjects")}</div>
        <button
          type="button"
          className="project-tree__empty-primary"
          onClick={() => void handleAddProject()}
          disabled={addingProject}
        >
          <FolderPlus size={14} />
          <span>{t("projectTree.addProjectTooltip")}</span>
        </button>
      </div>
    );
  };

  const hasWorkbenchRows = workbenchTreeSections.pinned.length > 0 || workbenchTreeSections.projects.length > 0;

  // Report visible topics to parent after render so shortcuts match sidebar order.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => {
    onVisibleTopicsChange?.(visibleTopicsCollectorRef.current);
  });

  // Reset topic index counter and visible topics collector before each render.
  topicIndexRef.current = 0;
  visibleTopicsCollectorRef.current = [];

  return (
    <div className="project-tree">
      {searchVisible && (
        <label className="project-tree__search">
          <Search size={14} />
          <input
            ref={searchInputRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t("projectTree.searchPlaceholder")}
          />
        </label>
      )}
      {compactTopics ? (
        <>
          {renderProjectHeader("workbench")}
          <div className="project-tree__list project-tree__list--workbench">
            {!hasWorkbenchRows ? (
              renderEmptyState()
            ) : (
              <>
                {workbenchTreeSections.pinned.length > 0 && (
                  <div className="project-tree__section project-tree__section--pinned">
                    <div className="project-tree__section-title">{t("projectTree.pinnedTitle")}</div>
                    {workbenchTreeSections.pinned.map((node) => renderNode(node, 0, "pinned"))}
                  </div>
                )}
                <div className="project-tree__section project-tree__section--projects">
                  {workbenchTreeSections.projects.map((node) => renderNode(node, 0, "projects"))}
                </div>
              </>
            )}
          </div>
        </>
      ) : (
        <>
          {renderProjectHeader("classic")}
          <div className="project-tree__list">
            {visibleTree.length === 0 ? renderEmptyState() : visibleTree.map((node) => renderNode(node, 0))}
          </div>
        </>
      )}
    </div>
  );
}
