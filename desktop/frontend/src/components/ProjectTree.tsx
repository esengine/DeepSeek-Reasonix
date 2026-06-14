// ProjectTree is the sidebar replacement for the flat recent-sessions list.
// It shows a tree of projects (each with expandable topics) plus a Global
// section. Clicking a topic opens its tab; "+" next to a project creates a
// new topic.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, DragEvent as ReactDragEvent, KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent } from "react";
import { Archive, ChevronRight, Pencil, Plus, Folder, FolderPlus, Search, BriefcaseBusiness, Copy, FolderOpen, XCircle, History, Check, ListCollapse, ListRestart, MessageSquare, Clock } from "lucide-react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import type { ProjectNode, ProjectTopicStatus } from "../lib/types";
import { topicActivityTime } from "../lib/session";
import { getLocale, useT, type DictKey, type Translator } from "../lib/i18n";
import { PROJECT_COLOR_OPTIONS, projectColorValue } from "../lib/projectColors";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";
import { Tooltip } from "./Tooltip";

interface ProjectTreeProps {
  activeScope?: string;
  activeWorkspaceRoot?: string;
  activeTopicId?: string;
  imTopicSources?: Record<string, ProjectTreeImTopicSource>;
  onOpenTopic: (scope: string, workspaceRoot: string, topicId: string) => Promise<void> | void;
  onOpenProjectHistory: (scope: "global" | "project", workspaceRoot: string) => Promise<void> | void;
  onAddProject: () => Promise<void>;
  onCreateTopic?: (scope: string, workspaceRoot: string) => Promise<void> | void;
  onRenameTopic?: (topicId: string, title: string) => Promise<void> | void;
  onTopicsChanged?: () => Promise<void> | void;
  refreshSignal?: number;
  timeFilter: "all" | "10" | "20" | "1h" | "3h" | "5h" | "1d";
  onTimeFilterChange: (filter: "all" | "10" | "20" | "1h" | "3h" | "5h" | "1d") => void;
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

function topicIsActive(node: ProjectNode, activeScope?: string, activeWorkspaceRoot?: string, activeTopicId?: string): boolean {
  if (node.kind !== "topic" && node.kind !== "global_topic") return false;
  const scope = node.kind === "global_topic" ? "global" : "project";
  return (
    activeTopicId === node.topicId &&
    activeScope === scope &&
    (scope === "global" || activeWorkspaceRoot === node.root)
  );
}

function topicMetaLine(node: ProjectNode, t: Translator): string {
  const parts: string[] = [];
  const turns = node.turns ?? 0;
  if (turns > 0) parts.push(t(turns === 1 ? "history.turnOne" : "history.turnOther", { n: turns }));
  const activityAt = node.lastActivityAt || node.createdAt || 0;
  if (activityAt) parts.push(topicActivityLabel(activityAt, t));
  if (parts.length === 0) parts.push(t("projectTree.justNow"));
  return parts.join(" · ");
}

const topicStatusLabels: Record<ProjectTopicStatus, DictKey> = {
  thinking: "projectTree.status.thinking",
  streaming: "projectTree.status.streaming",
  waiting_confirmation: "projectTree.status.waitingConfirmation",
  paused: "projectTree.status.paused",
  error: "projectTree.status.error",
};

function normalizeTopicStatus(status?: string): ProjectTopicStatus | "" {
  if (!status) return "";
  if (status === "thinking" || status === "streaming" || status === "waiting_confirmation" || status === "paused" || status === "error") {
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

function topicActivityLabel(ms: number, t: Translator): string {
  if (ms <= 0) return "";
  const delta = Date.now() - ms;
  const locale = getLocale();
  const rtf = new Intl.RelativeTimeFormat(locale === "zh" ? "zh-CN" : "en", { numeric: "auto" });
  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;
  if (delta < minute) return t("projectTree.justNow");
  if (delta < hour) return rtf.format(-Math.max(1, Math.round(delta / minute)), "minute");
  if (delta < day) return rtf.format(-Math.round(delta / hour), "hour");
  if (delta < 7 * day) return rtf.format(-Math.round(delta / day), "day");
  return new Date(ms).toLocaleDateString();
}

type ProjectDropPosition = "before" | "after";

type CollapseSnapshot = {
  expanded: Set<string>;
  manuallyCollapsed: Set<string>;
};

const GLOBAL_PROJECT_ORDER_KEY = "__global__";

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
  imTopicSources = {},
  onOpenTopic,
  onOpenProjectHistory,
  onAddProject,
  onCreateTopic,
  onRenameTopic,
  onTopicsChanged,
  refreshSignal,
  timeFilter,
  onTimeFilterChange,
}: ProjectTreeProps) {
  const t = useT();
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
  const filterRef = useRef<HTMLDivElement>(null);
  const filterTriggerRef = useRef<HTMLButtonElement>(null);
  const [filterMenuOpen, setFilterMenuOpen] = useState(false);
  const creatingRef = useRef(false);
  const manuallyCollapsedRef = useRef(manuallyCollapsed);

  const closeMenu = useCallback(() => {
    setMenuTopic(null);
    setMenuProject(null);
    setMenuPoint(null);
    setConfirmAction(null);
    setConfirmRemoveProject(null);
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
        for (const node of list) {
          if (node?.key && !collapsed.has(node.key)) next.add(node.key);
        }
        return next;
      });
    } catch {
      /* bridge unavailable */
    }
  }, []);

  useEffect(() => {
    manuallyCollapsedRef.current = manuallyCollapsed;
  }, [manuallyCollapsed]);

  useEffect(() => {
    void refresh();
  }, [refresh, refreshSignal]);

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
    } catch {
      /* ignore */
    }
  };

  const commitRenameProject = async (root: string) => {
    const title = projectDraft.trim();
    setEditingProject(null);
    if (!title) return;
    try {
      await app.RenameProject(root, title);
      await refresh();
    } catch {
      /* ignore */
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
    } catch {
      /* ignore */
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
    } catch {
      /* ignore */
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
    return tree
      .map(filterNode)
      .filter((node): node is ProjectNode => node !== null);
  }, [query, tree, timeFilter]);

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

  const activeAncestorKeys = useMemo(() => {
    const walk = (nodes: ProjectNode[], ancestors: string[]): string[] | null => {
      for (const node of nodes) {
        if (!node) continue;
        if (topicIsActive(node, activeScope, activeWorkspaceRoot, activeTopicId)) return ancestors;
        const children = asArray(node.children);
        if (children.length > 0) {
          const next = walk(children, [...ancestors, projectNodeKey(node, ancestors.length)]);
          if (next) return next;
        }
      }
      return null;
    };
    return walk(tree, []) ?? [];
  }, [activeScope, activeTopicId, activeWorkspaceRoot, tree]);

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

  const renderNode = (node: ProjectNode | null | undefined, depth: number) => {
    if (!node) return null;
    const key = projectNodeKey(node, depth);
    const children = asArray(node.children);
    const isExpanded = query.trim() ? true : expanded.has(key);
    const hasChildren = children.length > 0;

    if (node.kind === "topic" || node.kind === "global_topic") {
      const scope = node.kind === "global_topic" ? "global" : "project";
      const scopeClass = scope === "global" ? " project-tree__topic--global" : " project-tree__topic--project";
      const accentStyle = projectAccentStyle(node.projectColor, scope === "global" ? "var(--project-tree-global-accent)" : undefined);
      const active = topicIsActive(node, activeScope, activeWorkspaceRoot, activeTopicId);
      const label = (node.label || node.topicId || "Untitled").replace(/^●\s*/, "");
      const meta = topicMetaLine(node, t);
      const status = topicStatus(node);
      const statusLabel = topicStatusLabel(node, t);
      const topicId = node.topicId ?? "";
      const imSource = scope === "global" && topicId ? imTopicSources[topicId] : undefined;
      const imSourceLabel = imSource?.label || "";
      const imSourceTitle = imSourceLabel ? t("msg.fromIm", { source: imSourceLabel }) : "";
      const imSourcePlatform = (imSource?.platform || "im").replace(/[^a-z0-9_-]/gi, "").toLowerCase() || "im";
      const title = [label, imSourceTitle, statusLabel, meta].filter(Boolean).join(" · ");
      const topicMenuOpen = menuTopic === topicId;
      const openTopicMenu = (event: ReactMouseEvent<HTMLElement> | ReactKeyboardEvent<HTMLElement>) => {
        event.preventDefault();
        event.stopPropagation();
        setMenuProject(null);
        setConfirmRemoveProject(null);
        setMenuPoint(contextMenuPointFromEvent(event));
        setMenuTopic(topicId);
        setConfirmAction(null);
      };
      const topicMenuItems: ContextMenuItem[] = [
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
      if (editingTopic === topicId) {
        return (
          <div
            key={key}
            className={`project-tree__topic project-tree__topic--editing${active ? " project-tree__topic--active" : ""}${imSource ? " project-tree__topic--im-source" : ""}`}
            style={{ paddingLeft: 14 + depth * 16 }}
          >
            <input
              autoFocus
              className="project-tree__topic-input"
              value={topicDraft}
              onChange={(event) => setTopicDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void commitRenameTopic(topicId);
                if (event.key === "Escape") setEditingTopic(null);
              }}
              onBlur={() => void commitRenameTopic(topicId)}
            />
          </div>
        );
      }
      return (
        <div
          key={key}
          className={`project-tree__topic${scopeClass}${active ? " project-tree__topic--active" : ""}${node.running ? " project-tree__topic--running" : ""}${status ? ` project-tree__topic--status-${status}` : ""}${topicMenuOpen ? " project-tree__topic--menu-open" : ""}${meta ? " project-tree__topic--has-meta" : ""}${imSource ? " project-tree__topic--im-source" : ""}`}
          style={accentStyle}
          onContextMenu={openTopicMenu}
        >
          <button
            type="button"
            className="project-tree__topic-main"
            title={title}
            style={{ paddingLeft: 14 + depth * 16 }}
            onClick={() => onOpenTopic(scope, node.root ?? "", topicId)}
            onKeyDown={(event) => {
              if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) {
                openTopicMenu(event);
              }
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
                {statusLabel && <span className={`project-tree__topic-status project-tree__topic-status--${status}`}>{statusLabel}</span>}
              </span>
              {meta && (
                <span className="project-tree__topic-meta">
                  <span className="project-tree__topic-meta-text">{meta}</span>
                </span>
              )}
            </span>
          </button>
          <ContextMenu
            open={topicMenuOpen}
            point={menuPoint}
            items={topicMenuItems}
            minWidth={178}
            ariaLabel={t("projectTree.topicActions")}
            onClose={closeMenu}
          />
        </div>
      );
    }

    const scope = node.kind === "global_folder" ? "global" : "project";
    const scopeClass = scope === "global" ? " project-tree__folder--global" : " project-tree__folder--project";
    const accentStyle = projectAccentStyle(node.projectColor, scope === "global" ? "var(--project-tree-global-accent)" : undefined);
    const projectRoot = scope === "global" ? "" : node.root ?? "";
    const projectDragKey = scope === "global" ? GLOBAL_PROJECT_ORDER_KEY : projectRoot;
    const projectPath = node.root ?? "";
    const colorTargetRoot = scope === "global" ? "" : projectPath;
    const projectLabel = node.label || (scope === "global" ? "Global" : "Untitled");
    const projectActive = activeScope === scope && (scope === "global" || activeWorkspaceRoot === node.root);
    const draggableProject = projectDragEnabled && depth === 0 && Boolean(projectDragKey) && editingProject?.key !== key;
    const projectDropPosition = dropProject?.root === projectDragKey ? dropProject.position : null;
    const handleProjectDragStart = (event: ReactDragEvent<HTMLElement>) => {
      if (!draggableProject) return;
      const target = event.target;
      if (target instanceof Element && target.closest(".project-tree__action-slot")) {
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

    if (editingProject?.key === key) {
      return (
        <div key={key}>
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
                {children.map((child) => renderNode(child, depth + 1))}
              </div>
            </div>
          )}
        </div>
      );
    }

    return (
      <div key={key}>
        <div
          className={`project-tree__folder${scopeClass}${draggableProject ? " project-tree__folder--draggable" : ""}${projectActive ? " project-tree__folder--active" : ""}${menuProject?.key === key ? " project-tree__folder--menu-open" : ""}${dragProjectRoot === projectDragKey ? " project-tree__folder--dragging" : ""}${projectDropPosition ? ` project-tree__folder--drop-${projectDropPosition}` : ""}`}
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
              if (hasChildren) toggleExpand(key);
            }}
            onKeyDown={(event) => {
              if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) {
                openProjectMenu(event);
              }
            }}
            aria-expanded={hasChildren ? isExpanded : undefined}
          >
            {hasChildren ? (
              <span className={`project-tree__chevron${isExpanded ? " project-tree__chevron--open" : ""}`}>
                <ChevronRight size={12} />
              </span>
            ) : (
              <span style={{ width: 12 }} />
            )}
            <Folder size={12} />
            <span className="project-tree__folder-color" aria-hidden="true" />
            <span className="project-tree__folder-label">{projectLabel}</span>
          </button>
          <Tooltip label={t("projectTree.newTopicTooltip")} className="project-tree__action-slot">
            <button
              type="button"
              className={`project-tree__new-topic${creatingProject === key ? " project-tree__new-topic--active" : ""}`}
              disabled={creatingProject !== null}
              onClick={(e) => {
                e.stopPropagation();
                void handleCreateTopic(scope, projectRoot, key);
              }}
            >
              <Plus size={12} />
            </button>
          </Tooltip>
          <ContextMenu
            open={menuProject?.key === key}
            point={menuPoint}
            items={projectMenuItems}
            minWidth={212}
            ariaLabel={t("projectTree.projectActions")}
            onClose={closeMenu}
          />
        </div>
        {hasChildren && (
          <div className={`project-tree__children${isExpanded ? " project-tree__children--expanded" : ""}`}>
            <div className="project-tree__children-inner">
              {children.map((child) => renderNode(child, depth + 1))}
            </div>
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="project-tree">
      <label className="project-tree__search">
        <Search size={14} />
        <input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={t("projectTree.searchPlaceholder")}
        />
      </label>
      <div className="project-tree__header">
        <span className="project-tree__header-title">
          <BriefcaseBusiness className="project-tree__header-icon" size={13} />
          {t("projectTree.workspaceTitle")}
        </span>
        <span className="project-tree__header-actions">
          <Tooltip label={t("projectTree.timeFilter")} className="project-tree__action-slot project-tree__header-action-slot project-tree__header-action-slot--filter">
            <div ref={filterRef} className="project-tree__time-filter">
              <button
                ref={filterTriggerRef}
                type="button"
                className={`project-tree__header-action-btn${timeFilter !== "all" ? " project-tree__header-action-btn--active" : ""}`}
                aria-label={t("projectTree.timeFilter")}
                aria-haspopup="menu"
                aria-expanded={filterMenuOpen}
                onClick={() => setFilterMenuOpen(!filterMenuOpen)}
              >
                <Clock size={14} />
                {timeFilter !== "all" && (
                  <span className="project-tree__time-filter-label">
                    {timeFilter === "1d" ? "24h" : timeFilter}
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
        </span>
      </div>
      <div className="project-tree__list">
        {visibleTree.length === 0 ? (
          query.trim() ? (
            <div className="project-tree__empty">{t("projectTree.emptyNoMatch")}</div>
          ) : timeFilter !== "all" ? (
            <div className="project-tree__empty">{t("projectTree.emptyNoTimeFilterMatch")}
              <button
                type="button"
                className="project-tree__empty-primary"
                onClick={() => onTimeFilterChange("all")}
              >
                {t("projectTree.clearTimeFilter")}
              </button>
            </div>
          ) : (
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
          )
        ) : (
          visibleTree.map((node) => renderNode(node, 0))
        )}
      </div>
    </div>
  );
}
