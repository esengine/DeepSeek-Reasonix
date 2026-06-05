// ProjectTree is the sidebar replacement for the flat recent-sessions list.
// It shows a tree of projects (each with expandable topics) plus a Global
// section. Clicking a topic opens its tab; "+" next to a project creates a
// new topic.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, DragEvent as ReactDragEvent, KeyboardEvent as ReactKeyboardEvent, MouseEvent as ReactMouseEvent } from "react";
import { Archive, ChevronRight, ChevronDown, Pencil, Plus, Folder, FolderGit2, FolderPlus, Search, BriefcaseBusiness, Copy, FolderOpen, XCircle, History, Check } from "lucide-react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import type { ProjectNode } from "../lib/types";
import { getLocale, useT, type Translator } from "../lib/i18n";
import { PROJECT_COLOR_OPTIONS, projectColorValue } from "../lib/projectColors";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";
import { Tooltip } from "./Tooltip";

interface ProjectTreeProps {
  activeScope?: string;
  activeWorkspaceRoot?: string;
  activeTopicId?: string;
  currentWorkspaceName?: string;
  onOpenTopic: (scope: string, workspaceRoot: string, topicId: string) => Promise<void> | void;
  onOpenProjectHistory: (scope: "global" | "project", workspaceRoot: string) => Promise<void> | void;
  onAddProject: () => Promise<void>;
  onUseCurrentProject?: () => Promise<void>;
  onRenameTopic?: (topicId: string, title: string) => Promise<void> | void;
  onTopicsChanged?: () => Promise<void> | void;
  refreshSignal?: number;
}

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
  const turns = node.turns ?? 0;
  if (turns <= 0) return "";
  const last = node.lastActivityAt ? ` · ${topicActivityLabel(node.lastActivityAt)}` : "";
  return `${t(turns === 1 ? "history.turnOne" : "history.turnOther", { n: turns })}${last}`;
}

function topicActivityLabel(ms: number): string {
  if (ms <= 0) return "";
  const delta = Date.now() - ms;
  const locale = getLocale();
  const rtf = new Intl.RelativeTimeFormat(locale === "zh" ? "zh-CN" : "en", { numeric: "auto" });
  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;
  if (delta < minute) return new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  if (delta < hour) return rtf.format(-Math.max(1, Math.round(delta / minute)), "minute");
  if (delta < day) return rtf.format(-Math.round(delta / hour), "hour");
  if (delta < 7 * day) return rtf.format(-Math.round(delta / day), "day");
  return new Date(ms).toLocaleDateString();
}

type ProjectDropPosition = "before" | "after";

function projectRoots(nodes: ProjectNode[]): string[] {
  return nodes
    .filter((node) => node.kind === "project" && Boolean(node.root))
    .map((node) => node.root!);
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
  const byRoot = new Map(nodes.filter((node) => node.kind === "project" && node.root).map((node) => [node.root!, node]));
  const orderedProjects = roots.map((root) => byRoot.get(root)).filter((node): node is ProjectNode => Boolean(node));
  const nonProjects = nodes.filter((node) => node.kind !== "project");
  return [...nonProjects, ...orderedProjects];
}

function projectAccentStyle(color?: string): CSSProperties | undefined {
  const value = projectColorValue(color);
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
  currentWorkspaceName,
  onOpenTopic,
  onOpenProjectHistory,
  onAddProject,
  onUseCurrentProject,
  onRenameTopic,
  onTopicsChanged,
  refreshSignal,
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
  const [platform, setPlatform] = useState("");
  const creatingRef = useRef(false);

  const closeMenu = useCallback(() => {
    setMenuTopic(null);
    setMenuProject(null);
    setMenuPoint(null);
    setConfirmAction(null);
    setConfirmRemoveProject(null);
  }, []);

  const refresh = useCallback(async () => {
    try {
      const nodes = await app.ListProjectTree();
      const list = asArray(nodes);
      setTree(list);
      setExpanded((prev) => {
        const next = new Set(prev);
        for (const node of list) {
          if (node?.key && !manuallyCollapsed.has(node.key)) next.add(node.key);
        }
        return next;
      });
    } catch {
      /* bridge unavailable */
    }
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

  const toggleExpand = (key: string) => {
    const willCollapse = expanded.has(key);
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
    setManuallyCollapsed((prev) => {
      const next = new Set(prev);
      if (willCollapse) next.add(key);
      else next.delete(key);
      return next;
    });
  };

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
    setManuallyCollapsed((prev) => {
      if (!prev.has(key)) return prev;
      const next = new Set(prev);
      next.delete(key);
      return next;
    });
    try {
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
    if (!q) return tree;
    const matches = (node: ProjectNode) =>
      [node.label, node.root, node.topicId].some((value) => (value ?? "").toLowerCase().includes(q));
    const filterNode = (node: ProjectNode): ProjectNode | null => {
      const children = asArray(node.children)
        .map(filterNode)
        .filter((child): child is ProjectNode => child !== null);
      if (matches(node) || children.length > 0) return { ...node, children };
      return null;
    };
    return tree
      .map(filterNode)
      .filter((node): node is ProjectNode => node !== null);
  }, [query, tree]);

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
      const active = topicIsActive(node, activeScope, activeWorkspaceRoot, activeTopicId);
      const label = (node.label || node.topicId || "Untitled").replace(/^●\s*/, "");
      const meta = topicMetaLine(node, t);
      const topicId = node.topicId ?? "";
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
            className={`project-tree__topic project-tree__topic--editing${active ? " project-tree__topic--active" : ""}`}
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
          className={`project-tree__topic${active ? " project-tree__topic--active" : ""}${topicMenuOpen ? " project-tree__topic--menu-open" : ""}`}
          style={projectAccentStyle(node.projectColor)}
          onContextMenu={openTopicMenu}
        >
          <button
            type="button"
            className="project-tree__topic-main"
            title={meta ? `${label} · ${meta}` : label}
            style={{ paddingLeft: 14 + depth * 16 }}
            onClick={() => onOpenTopic(scope, node.root ?? "", topicId)}
            onKeyDown={(event) => {
              if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) {
                openTopicMenu(event);
              }
            }}
          >
            <span className="project-tree__topic-copy">
              <span className="project-tree__topic-label">{label}</span>
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
    const projectRoot = scope === "global" ? "" : node.root ?? "";
    const projectPath = node.root ?? "";
    const colorTargetRoot = scope === "global" ? "" : projectPath;
    const projectLabel = node.label || (scope === "global" ? "Global" : "Untitled");
    const projectActive = activeScope === scope && (scope === "global" || activeWorkspaceRoot === node.root);
    const draggableProject = projectDragEnabled && scope === "project" && depth === 0 && Boolean(projectRoot) && editingProject?.key !== key;
    const projectDropPosition = dropProject?.root === projectRoot ? dropProject.position : null;
    const handleProjectDragStart = (event: ReactDragEvent<HTMLDivElement>) => {
      if (!draggableProject) return;
      const target = event.target as HTMLElement | null;
      if (target?.closest("button,input,textarea,select")) {
        event.preventDefault();
        return;
      }
      event.dataTransfer.effectAllowed = "move";
      event.dataTransfer.setData("text/plain", projectRoot);
      setDragProjectRoot(projectRoot);
      setDropProject(null);
    };
    const handleProjectDragOver = (event: ReactDragEvent<HTMLDivElement>) => {
      if (!draggableProject || !dragProjectRoot || dragProjectRoot === projectRoot) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = "move";
      const rect = event.currentTarget.getBoundingClientRect();
      const position: ProjectDropPosition = event.clientY < rect.top + rect.height / 2 ? "before" : "after";
      setDropProject((current) => {
        if (current?.root === projectRoot && current.position === position) return current;
        return { root: projectRoot, position };
      });
    };
    const handleProjectDrop = (event: ReactDragEvent<HTMLDivElement>) => {
      if (!draggableProject) return;
      const draggedRoot = dragProjectRoot || event.dataTransfer.getData("text/plain");
      const position = dropProject?.root === projectRoot ? dropProject.position : "after";
      event.preventDefault();
      clearProjectDrag();
      if (draggedRoot && draggedRoot !== projectRoot) void commitProjectReorder(draggedRoot, projectRoot, position);
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
          void app.RevealPath(projectPath);
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
          {isExpanded && hasChildren && (
            <div className="project-tree__children">
              {children.map((child) => renderNode(child, depth + 1))}
            </div>
          )}
        </div>
      );
    }

    return (
      <div key={key}>
        <div
          className={`project-tree__folder${projectActive ? " project-tree__folder--active" : ""}${menuProject?.key === key ? " project-tree__folder--menu-open" : ""}${dragProjectRoot === projectRoot ? " project-tree__folder--dragging" : ""}${projectDropPosition ? ` project-tree__folder--drop-${projectDropPosition}` : ""}`}
          style={projectAccentStyle(node.projectColor)}
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
              isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />
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
        {isExpanded && hasChildren && (
          <div className="project-tree__children">
            {children.map((child) => renderNode(child, depth + 1))}
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
          <BriefcaseBusiness size={13} />
          {t("projectTree.workspaceTitle")}
        </span>
        <Tooltip label={t("projectTree.addProjectTooltip")} className="project-tree__action-slot">
          <button
            type="button"
            className="project-tree__add-project"
            aria-label={t("projectTree.addProjectTooltip")}
            disabled={addingProject}
            onClick={() => void handleAddProject()}
          >
            <FolderPlus size={13} />
          </button>
        </Tooltip>
      </div>
      <div className="project-tree__list">
        {visibleTree.length === 0 ? (
          query.trim() ? (
            <div className="project-tree__empty">{t("projectTree.emptyNoMatch")}</div>
          ) : (
            <div className="project-tree__empty-state">
              <div className="project-tree__empty-icon">
                <FolderPlus size={18} />
              </div>
              <div className="project-tree__empty-copy">
                <strong>{t("projectTree.emptyNoProjects")}</strong>
                <span>{t("projectTree.emptyNoProjectsDesc")}</span>
              </div>
              <div className="project-tree__empty-actions">
                <button
                  type="button"
                  className="project-tree__empty-primary"
                  onClick={() => void handleAddProject()}
                  disabled={addingProject}
                >
                  <FolderPlus size={14} />
                  <span>{t("projectTree.emptyAddProject")}</span>
                </button>
                {onUseCurrentProject && currentWorkspaceName && (
                  <button
                    type="button"
                    className="project-tree__empty-secondary"
                    onClick={async () => {
                      await onUseCurrentProject();
                      await refresh();
                    }}
                  >
                    <FolderGit2 size={14} />
                    <span>{t("projectTree.emptyUseCurrentDir")}</span>
                  </button>
                )}
              </div>
              {currentWorkspaceName && <div className="project-tree__empty-current">{t("projectTree.emptyCurrentDir", { dir: currentWorkspaceName })}</div>}
            </div>
          )
        ) : (
          visibleTree.map((node) => renderNode(node, 0))
        )}
      </div>
    </div>
  );
}
