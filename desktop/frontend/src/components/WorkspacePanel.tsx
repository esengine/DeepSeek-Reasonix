import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type {
  CSSProperties,
  DragEvent as ReactDragEvent,
  KeyboardEvent,
  MouseEvent as ReactMouseEvent,
  PointerEvent as ReactPointerEvent,
} from "react";
import {
  ChevronDown,
  ChevronRight,
  FileText,
  Folder,
  FolderTree,
  FolderX,
  Check,
  GitBranch,
  Maximize2,
  MessageSquarePlus,
  Minimize2,
  RefreshCw,
  Search,
  X,
} from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { loadLayoutSize, saveLayoutSize } from "../lib/layoutPreferences";
import type { DirEntry, FilePreview, WorkspaceChangeView, WorkspaceChangesView } from "../lib/types";
import { formatWorkspaceReference, WORKSPACE_REF_DRAG_TYPE } from "../lib/workspaceDrag";
import { CodeViewer } from "./CodeViewer";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";
import { FloatingMenu, FloatingMenuItems } from "./FloatingMenu";
import { Markdown } from "./Markdown";
import { Tooltip } from "./Tooltip";
import { AnchoredPopover } from "./AnchoredPopover";

const WORKSPACE_TREE_MIN_WIDTH = 208;
const WORKSPACE_TREE_DEFAULT_WIDTH = 240;
const WORKSPACE_TREE_MAX_WIDTH = 340;
const WORKSPACE_PREVIEW_MIN_WIDTH = 360;
const WORKSPACE_PREVIEW_TARGET_WIDTH = 480;
const WORKSPACE_DUAL_PANEL_MIN_WIDTH = WORKSPACE_TREE_MIN_WIDTH + WORKSPACE_PREVIEW_MIN_WIDTH;
const WORKSPACE_DUAL_PANEL_TARGET_WIDTH = WORKSPACE_TREE_DEFAULT_WIDTH + WORKSPACE_PREVIEW_TARGET_WIDTH;
const WORKSPACE_CONTEXT_MENU_FILE_HEIGHT = 92;
const WORKSPACE_CONTEXT_MENU_REF_HEIGHT = 48;
const WORKSPACE_MAX_PREVIEW_TABS = 5;

function clampWorkspaceTreeWidth(width: number, panelWidth?: number): number {
  const maxForPanel =
    typeof panelWidth === "number" && Number.isFinite(panelWidth)
      ? Math.max(WORKSPACE_TREE_MIN_WIDTH, panelWidth - WORKSPACE_PREVIEW_MIN_WIDTH)
      : WORKSPACE_TREE_MAX_WIDTH;
  const max = Math.min(WORKSPACE_TREE_MAX_WIDTH, maxForPanel);
  return Math.min(max, Math.max(WORKSPACE_TREE_MIN_WIDTH, Math.round(width)));
}

function loadWorkspaceTreeWidth(): number {
  return loadLayoutSize("workspaceTreeWidth", WORKSPACE_TREE_DEFAULT_WIDTH, clampWorkspaceTreeWidth);
}

function saveWorkspaceTreeWidth(width: number): void {
  saveLayoutSize("workspaceTreeWidth", width);
}

function entryPath(dir: string, entry: DirEntry): string {
  const prefix = dir === "" || dir.endsWith("/") ? dir : dir + "/";
  return prefix + entry.name + (entry.isDir ? "/" : "");
}

function basename(path: string): string {
  const parts = path.split("/").filter(Boolean);
  return parts[parts.length - 1] ?? "";
}

function parentPath(path: string): string {
  const clean = path.replace(/\/$/, "");
  const parts = clean.split("/").filter(Boolean);
  return parts.slice(0, -1).join("/");
}

function parentDirs(path: string): string[] {
  const parts = path.split("/").filter(Boolean);
  const dirs: string[] = [""];
  let acc = "";
  for (let i = 0; i < parts.length - 1; i++) {
    acc += parts[i] + "/";
    dirs.push(acc);
  }
  return dirs;
}

function languageFor(path: string): string | undefined {
  const name = basename(path).toLowerCase();
  const ext = name.includes(".") ? name.slice(name.lastIndexOf(".") + 1) : name;
  const byExt: Record<string, string> = {
    css: "css",
    go: "go",
    html: "html",
    js: "javascript",
    json: "json",
    jsx: "jsx",
    md: "markdown",
    py: "python",
    rs: "rust",
    sh: "bash",
    toml: "toml",
    ts: "typescript",
    tsx: "tsx",
    yaml: "yaml",
    yml: "yaml",
  };
  return byExt[ext];
}

function renderMediaPreview(preview: FilePreview): JSX.Element | null {
  if (!preview.url) return null;
  if (preview.kind === "image") {
    return (
      <div className="workspace-media workspace-media--image">
        <img src={preview.url} alt={basename(preview.path)} decoding="async" />
      </div>
    );
  }
  if (preview.kind === "pdf") {
    return (
      <iframe
        className="workspace-media workspace-media--pdf"
        src={preview.url}
        title={basename(preview.path)}
      />
    );
  }
  return null;
}

function fenceFor(text: string): string {
  let longest = 0;
  for (const match of text.matchAll(/`+/g)) {
    longest = Math.max(longest, match[0].length);
  }
  return "`".repeat(Math.max(3, longest + 1));
}

function formatSelectionReference(path: string, text: string): string {
  const body = text.replace(/\r\n|\r/g, "\n").trimEnd();
  const fence = fenceFor(body);
  const lang = languageFor(path);
  return `From \`${path}\`:\n\n${fence}${lang ?? ""}\n${body}\n${fence}`;
}

function shortCwd(cwd?: string): string {
  if (!cwd) return "";
  const parts = cwd.split("/").filter(Boolean);
  if (parts.length <= 2) return cwd;
  return "…/" + parts.slice(-2).join("/");
}

function formatBytes(n: number): string {
  if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  if (n >= 1024) return `${Math.ceil(n / 1024)} KB`;
  return `${n} B`;
}

function isDeletedChange(row: WorkspaceChangeView): boolean {
  return !!row.gitStatus && row.gitStatus.includes("D");
}

function changeDetail(row: WorkspaceChangeView): string {
  if (row.latestPrompt) return row.latestPrompt;
  if (row.oldPath) return `← ${row.oldPath}`;
  if (row.turns && row.turns.length > 0) return `#${row.turns.join(", #")}`;
  return row.path;
}

export function WorkspacePanel({
  open,
  cwd,
  maximized,
  panelWidth,
  onClose,
  onToggleMaximized,
  onPreviewModeChange,
  onAddToChat,
  onRequestPanelWidth,
  refreshKey,
  initialViewMode = "files",
  showViewTabs = true,
}: {
  open: boolean;
  cwd?: string;
  maximized: boolean;
  panelWidth?: number;
  onClose: () => void;
  onToggleMaximized: () => void;
  onPreviewModeChange?: (active: boolean) => void;
  onAddToChat?: (text: string) => void;
  onRequestPanelWidth?: (width: number) => void;
  refreshKey?: number;
  initialViewMode?: "files" | "changed";
  showViewTabs?: boolean;
}) {
  const t = useT();
  const panelRef = useRef<HTMLElement>(null);
  const filterRef = useRef<HTMLInputElement>(null);
  const previewBodyRef = useRef<HTMLDivElement>(null);
  const [entriesByDir, setEntriesByDir] = useState<Record<string, DirEntry[]>>({});
  const [openDirs, setOpenDirs] = useState<Set<string>>(() => new Set([""]));
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [openTabs, setOpenTabs] = useState<string[]>([]);
  const [preview, setPreview] = useState<FilePreview | null>(null);
  const [loadingPreview, setLoadingPreview] = useState(false);
  const [viewMode, setViewMode] = useState<"files" | "changed">(initialViewMode);
  const [changes, setChanges] = useState<WorkspaceChangesView | null>(null);
  const [loadingChanges, setLoadingChanges] = useState(false);
  const [selectionMenu, setSelectionMenu] = useState<{ x: number; y: number; text: string; path: string } | null>(null);
  const [treeMenu, setTreeMenu] = useState<{ x: number; y: number; path: string; isDir: boolean } | null>(null);
  const [treeBlankMenuPoint, setTreeBlankMenuPoint] = useState<ContextMenuPoint | null>(null);
  const changesRequestRef = useRef(0);
  const [filter, setFilter] = useState("");
  const [treeVisible, setTreeVisible] = useState(true);
  const [treeWidth, setTreeWidth] = useState(loadWorkspaceTreeWidth);
  const [treeResizing, setTreeResizing] = useState(false);
  const [recentOpen, setRecentOpen] = useState(false);
  const recentAnchorRef = useRef<HTMLButtonElement>(null);
  const [branchMenuOpen, setBranchMenuOpen] = useState(false);
  const [branchList, setBranchList] = useState<string[]>([]);
  const [switchingBranch, setSwitchingBranch] = useState(false);
  const branchBtnRef = useRef<HTMLButtonElement>(null);
  const openDirsRef = useRef(openDirs);

  useEffect(() => {
    openDirsRef.current = openDirs;
  }, [openDirs]);

  const loadDir = useCallback(async (dir: string) => {
    const entries = await app.ListDir(dir).catch(() => []);
    setEntriesByDir((prev) => ({ ...prev, [dir]: entries ?? [] }));
  }, []);

  const loadChanges = useCallback(async () => {
    const requestId = changesRequestRef.current + 1;
    changesRequestRef.current = requestId;
    setLoadingChanges(true);
    try {
      const next = await app.WorkspaceChanges();
      if (changesRequestRef.current === requestId) setChanges(next);
    } catch (err) {
      if (changesRequestRef.current === requestId) {
        setChanges({ files: [], gitAvailable: false, gitErr: String((err as Error)?.message ?? err) });
      }
    } finally {
      if (changesRequestRef.current === requestId) setLoadingChanges(false);
    }
  }, []);

  const openBranchMenu = useCallback(async () => {
    try {
      const branches = await app.GitBranches();
      setBranchList(branches);
    } catch {
      setBranchList([]);
    }
    setBranchMenuOpen((prev) => !prev);
  }, []);

  const switchBranch = useCallback(
    async (branch: string) => {
      if (branch === changes?.gitBranch) return;
      setSwitchingBranch(true);
      try {
        await app.GitCheckout(branch);
        await loadChanges();
      } catch {
        // Error message shown via the changes view
      } finally {
        setSwitchingBranch(false);
        setBranchMenuOpen(false);
      }
    },
    [changes?.gitBranch, loadChanges],
  );

  const selectFile = useCallback(
    (path: string) => {
      onRequestPanelWidth?.(WORKSPACE_DUAL_PANEL_TARGET_WIDTH);
      setSelectedPath(path);
      setFilter("");
      setOpenTabs((tabs) => [...tabs.filter((tab) => tab !== path), path].slice(-WORKSPACE_MAX_PREVIEW_TABS));
      const dirs = parentDirs(path);
      setOpenDirs((prev) => new Set([...Array.from(prev), ...dirs]));
      dirs.forEach((dir) => {
        if (!entriesByDir[dir]) void loadDir(dir);
      });
    },
    [entriesByDir, loadDir, onRequestPanelWidth],
  );

  useEffect(() => {
    if (!open) return;
    setEntriesByDir({});
    setOpenDirs(new Set([""]));
    setSelectedPath(null);
    setOpenTabs([]);
    setPreview(null);
    setChanges(null);
    setSelectionMenu(null);
    setTreeMenu(null);
    setFilter("");
    setTreeVisible(true);
    setViewMode(initialViewMode);
    void loadDir("");
  }, [cwd, initialViewMode, loadDir, open]);

  useEffect(() => {
    if (!open) return;
    setViewMode(initialViewMode);
    if (initialViewMode === "changed") void loadChanges();
  }, [initialViewMode, loadChanges, open]);

  useEffect(() => {
    if (!open) return;
    void loadChanges();
  }, [cwd, loadChanges, open]);

  useEffect(() => {
    if (!open || !refreshKey) return;
    void loadChanges();
    openDirsRef.current.forEach((dir) => void loadDir(dir));
  }, [loadChanges, loadDir, open, refreshKey]);

  useEffect(() => {
    if (!selectionMenu && !treeMenu) return;
    const close = () => {
      setSelectionMenu(null);
      setTreeMenu(null);
    };
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    window.addEventListener("click", close);
    window.addEventListener("resize", close);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("click", close);
      window.removeEventListener("resize", close);
      window.removeEventListener("keydown", onKey);
    };
  }, [selectionMenu, treeMenu]);

  const refreshWorkspaceList = useCallback(() => {
    setTreeBlankMenuPoint(null);
    setSelectionMenu(null);
    setTreeMenu(null);
    if (viewMode === "changed") {
      void loadChanges();
      return;
    }
    const dirs = Array.from(openDirsRef.current);
    setEntriesByDir({});
    dirs.forEach((dir) => void loadDir(dir));
  }, [loadChanges, loadDir, viewMode]);

  const refreshSelected = useCallback(() => {
    if (!selectedPath) return;
    let live = true;
    setLoadingPreview(true);
    app
      .ReadFile(selectedPath)
      .then((next) => {
        if (live) setPreview(next);
      })
      .catch((err) => {
        if (live) {
          setPreview({
            path: selectedPath,
            body: "",
            size: 0,
            truncated: false,
            binary: false,
            err: String(err?.message ?? err),
          });
        }
      })
      .finally(() => {
        if (live) setLoadingPreview(false);
      });
    return () => {
      live = false;
    };
  }, [selectedPath]);

  useEffect(() => {
    if (!open || !selectedPath) return;
    return refreshSelected();
  }, [open, refreshSelected, selectedPath]);

  const toggleDir = useCallback(
    (dir: string) => {
      setOpenDirs((prev) => {
        const next = new Set(prev);
        if (next.has(dir)) {
          next.delete(dir);
        } else {
          next.add(dir);
          if (!entriesByDir[dir]) void loadDir(dir);
        }
        return next;
      });
    },
    [entriesByDir, loadDir],
  );

  const closeTab = (path: string) => {
    setOpenTabs((tabs) => {
      const next = tabs.filter((tab) => tab !== path);
      if (selectedPath === path) {
        const replacement = next[next.length - 1] ?? null;
        setSelectedPath(replacement);
        if (!replacement) {
          setPreview(null);
          setTreeVisible(true);
        }
        setSelectionMenu(null);
        setTreeMenu(null);
        setRecentOpen(false);
      }
      return next;
    });
  };

  const breadcrumbDirs = selectedPath ? parentDirs(selectedPath) : [""];
  const pathParts = selectedPath?.split("/").filter(Boolean) ?? [];
  const currentFileName = selectedPath ? basename(selectedPath) : t("workspace.noFile");
  const currentFileDir = selectedPath ? parentPath(selectedPath) : "";
  const recentFiles = useMemo(() => [...openTabs].reverse(), [openTabs]);
  const flattened = useMemo(() => {
    const rows: { path: string; entry: DirEntry }[] = [];
    for (const [dir, entries] of Object.entries(entriesByDir)) {
      for (const entry of entries) {
        rows.push({ path: entryPath(dir, entry), entry });
      }
    }
    const q = filter.trim().toLowerCase();
    if (!q) return null;
    return rows
      .filter((row) => row.path.toLowerCase().includes(q))
      .sort((a, b) => a.path.localeCompare(b.path));
  }, [entriesByDir, filter]);

  const changedRows = useMemo(() => {
    const rows = changes?.files ?? [];
    const q = filter.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((row) => `${row.path} ${row.oldPath ?? ""} ${row.gitStatus ?? ""}`.toLowerCase().includes(q));
  }, [changes?.files, filter]);
  const searchPlaceholder = viewMode === "changed" ? t("workspace.filterChanges") : t("workspace.filter");

  const effectiveTreeWidth = useMemo(() => clampWorkspaceTreeWidth(treeWidth, panelWidth), [panelWidth, treeWidth]);
  const previewVisible = openTabs.length > 0 || selectedPath !== null;
  const selectedFileVisible = selectedPath !== null;
  const compactTreeSplit =
    treeVisible && selectedFileVisible && panelWidth !== undefined && panelWidth < WORKSPACE_DUAL_PANEL_MIN_WIDTH;
  const actualTreeVisible = treeVisible;
  const previewModeActive = open && previewVisible;
  const embeddedDockMode = !showViewTabs;
  const showFileTools = showViewTabs || previewVisible;

  const panelStyle = useMemo(
    () =>
      ({
        "--workspace-tree-width": `${effectiveTreeWidth}px`,
        "--workspace-preview-min-width": compactTreeSplit ? "0px" : `${WORKSPACE_PREVIEW_MIN_WIDTH}px`,
      }) as CSSProperties,
    [compactTreeSplit, effectiveTreeWidth],
  );

  useEffect(() => {
    onPreviewModeChange?.(previewModeActive);
  }, [onPreviewModeChange, previewModeActive]);

  useEffect(() => {
    if (open && !treeVisible && !previewVisible) onClose();
  }, [onClose, open, previewVisible, treeVisible]);

  const hideTreeOrClosePanel = useCallback(() => {
    if (previewVisible) {
      setTreeVisible(false);
    } else {
      onClose();
    }
  }, [onClose, previewVisible]);

  const setSavedTreeWidth = useCallback(
    (width: number) => {
      const next = clampWorkspaceTreeWidth(width, panelWidth);
      setTreeWidth(next);
      saveWorkspaceTreeWidth(next);
    },
    [panelWidth],
  );

  const startTreeResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (!treeVisible) return;
      const rect = panelRef.current?.getBoundingClientRect();
      if (!rect) return;
      event.preventDefault();
      setTreeResizing(true);
      let nextWidth = effectiveTreeWidth;
      const onMove = (moveEvent: PointerEvent) => {
        nextWidth = clampWorkspaceTreeWidth(moveEvent.clientX - rect.left, rect.width);
        setTreeWidth(nextWidth);
      };
      const onDone = () => {
        setTreeWidth(nextWidth);
        saveWorkspaceTreeWidth(nextWidth);
        setTreeResizing(false);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [effectiveTreeWidth, treeVisible],
  );

  const resizeTreeWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setSavedTreeWidth(effectiveTreeWidth + (event.key === "ArrowRight" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setSavedTreeWidth(WORKSPACE_TREE_MIN_WIDTH);
      } else if (event.key === "End") {
        event.preventDefault();
        setSavedTreeWidth(WORKSPACE_TREE_MAX_WIDTH);
      }
    },
    [effectiveTreeWidth, setSavedTreeWidth],
  );

  if (!open) return null;

  const selectedTextFromPreview = (): string => {
    const root = previewBodyRef.current;
    const selection = typeof window === "undefined" ? null : window.getSelection();
    if (!root || !selection || selection.rangeCount === 0) return "";
    const range = selection.getRangeAt(0);
    const container = range.commonAncestorContainer;
    const node = container instanceof Element ? container : container.parentElement;
    if (!node || !root.contains(node)) return "";
    return selection.toString();
  };

  const openSelectionMenu = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (!selectedPath || loadingPreview || preview?.err || preview?.binary || preview?.kind) return;
    const text = selectedTextFromPreview();
    if (text.trim() === "") return;
    event.preventDefault();
    event.stopPropagation();
    setSelectionMenu({ x: event.clientX, y: event.clientY, text, path: selectedPath });
  };

  const addSelectionToChat = () => {
    if (!selectionMenu) return;
    onAddToChat?.(formatSelectionReference(selectionMenu.path, selectionMenu.text));
    setSelectionMenu(null);
  };

  const openTreeMenu = (event: ReactMouseEvent<HTMLElement>, path: string, isDir: boolean) => {
    event.preventDefault();
    event.stopPropagation();
    setTreeBlankMenuPoint(null);
    setSelectionMenu(null);
    setTreeMenu({ x: event.clientX, y: event.clientY, path, isDir });
  };

  const openTreeBlankMenu = (event: ReactMouseEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement | null;
    if (target?.closest(".workspace-tree__row,.workspace-change,button,input,textarea,select")) return;
    event.preventDefault();
    event.stopPropagation();
    setSelectionMenu(null);
    setTreeMenu(null);
    setTreeBlankMenuPoint(contextMenuPointFromEvent(event));
  };

  const startTreeDrag = (event: ReactDragEvent<HTMLElement>, path: string, isDir: boolean) => {
    const ref = formatWorkspaceReference(path, isDir);
    event.dataTransfer.effectAllowed = "copy";
    event.dataTransfer.setData(WORKSPACE_REF_DRAG_TYPE, JSON.stringify({ path, isDir }));
    event.dataTransfer.setData("text/plain", ref);
  };

  const addTreeReferenceToChat = () => {
    if (!treeMenu) return;
    onAddToChat?.(formatWorkspaceReference(treeMenu.path, treeMenu.isDir));
    setTreeMenu(null);
  };

  const addTreeFileToChat = async () => {
    if (!treeMenu || treeMenu.isDir) return;
    const target = treeMenu;
    setTreeMenu(null);
    try {
      const file = await app.ReadFile(target.path);
      if (file.err || file.binary || file.kind) {
        onAddToChat?.(formatWorkspaceReference(target.path, false));
        return;
      }
      const suffix = file.truncated ? `\n\n${t("workspace.truncated")}` : "";
      onAddToChat?.(formatSelectionReference(target.path, file.body) + suffix);
    } catch {
      onAddToChat?.(formatWorkspaceReference(target.path, false));
    }
  };

  const renderChangedRows = () => {
    if (loadingChanges) return <div className="workspace-empty">{t("workspace.loadingChanges")}</div>;
    if (!changes) return null;
    if (changedRows.length === 0) return <div className="workspace-empty">{t("workspace.noChanges")}</div>;
    return changedRows.map((row) => {
      const deleted = isDeletedChange(row);
      return (
        <button
          key={`${row.path}-${row.sources.join("-")}`}
          className={`workspace-change${selectedPath === row.path ? " workspace-change--active" : ""}${deleted ? " workspace-change--disabled" : ""}`}
          draggable
          onDragStart={(event) => startTreeDrag(event, row.path, false)}
          onContextMenu={(event) => openTreeMenu(event, row.path, false)}
          onClick={() => {
            if (!deleted) selectFile(row.path);
          }}
          type="button"
        >
          <FileText size={14} className="workspace-tree__icon" />
          <span className="workspace-change__body">
            <span className="workspace-change__name">{basename(row.path)}</span>
            <span className="workspace-change__path">{row.path}</span>
            <span className="workspace-change__detail">{changeDetail(row)}</span>
          </span>
          <span className="workspace-change__meta">
            {row.gitStatus && <span className="workspace-change__badge workspace-change__badge--git">{row.gitStatus}</span>}
            {deleted && <span className="workspace-change__badge">{t("workspace.deleted")}</span>}
            {row.sources.includes("session") && <span className="workspace-change__badge">{t("workspace.sourceSession")}</span>}
            {row.sources.includes("git") && <span className="workspace-change__badge">{t("workspace.sourceGit")}</span>}
          </span>
        </button>
      );
    });
  };

  const renderRows = (dir: string, depth: number): JSX.Element[] => {
    const entries = entriesByDir[dir] ?? [];
    return entries.flatMap((entry) => {
      const path = entryPath(dir, entry);
      const isOpen = openDirs.has(path);
      const active = selectedPath === path;
      const row = (
        <button
          key={path}
          className={`workspace-tree__row${active ? " workspace-tree__row--active" : ""}`}
          draggable
          onDragStart={(event) => startTreeDrag(event, path, entry.isDir)}
          onClick={() => (entry.isDir ? toggleDir(path) : selectFile(path))}
          onContextMenu={(event) => openTreeMenu(event, path, entry.isDir)}
          style={{ paddingLeft: 8 + depth * 14 }}
        >
          {entry.isDir ? (
            isOpen ? (
              <ChevronDown size={13} className="workspace-tree__chev" />
            ) : (
              <ChevronRight size={13} className="workspace-tree__chev" />
            )
          ) : (
            <span className="workspace-tree__chev" />
          )}
          {entry.isDir ? (
            <Folder size={14} className="workspace-tree__icon workspace-tree__icon--dir" />
          ) : (
            <FileText size={14} className="workspace-tree__icon" />
          )}
          <span className="workspace-tree__name">{entry.name}</span>
        </button>
      );
      if (!entry.isDir || !isOpen) return [row];
      return [row, ...renderRows(path, depth + 1)];
    });
  };

  const isMarkdown = selectedPath?.toLowerCase().endsWith(".md") ?? false;
  const treeBlankMenuItems: ContextMenuItem[] = [
    {
      key: "refresh-tree",
      icon: <RefreshCw size={13} />,
      label: t(viewMode === "changed" ? "workspace.refreshChanges" : "workspace.refreshTree"),
      onSelect: refreshWorkspaceList,
    },
  ];

  return (
    <aside
      ref={panelRef}
      className={`workspace-panel${embeddedDockMode ? " workspace-panel--embedded" : ""}${previewVisible && actualTreeVisible ? " workspace-panel--split-preview" : ""}${compactTreeSplit ? " workspace-panel--compact-split" : ""}${actualTreeVisible ? "" : " workspace-panel--tree-hidden"}${previewVisible ? "" : " workspace-panel--preview-hidden"}${treeResizing ? " workspace-panel--tree-resizing" : ""}`}
      aria-label={t("workspace.title")}
      style={panelStyle}
    >
      {previewVisible && <section className="workspace-preview">
        <header className="workspace-preview__head">
          <div className="workspace-current-file" aria-label={t("workspace.currentFile")}>
            <FileText size={15} className="workspace-current-file__icon" />
            <div className="workspace-current-file__text">
              <Tooltip label={selectedPath ?? undefined}>
                <span className="workspace-current-file__name">{currentFileName}</span>
              </Tooltip>
              {currentFileDir && <span className="workspace-current-file__path">{currentFileDir}</span>}
            </div>
            <Tooltip label={t("workspace.recentFiles")}>
              <button
                ref={recentAnchorRef}
                className={`workspace-current-file__recent${recentOpen ? " workspace-current-file__recent--open" : ""}`}
                type="button"
                aria-label={t("workspace.recentFiles")}
                aria-expanded={recentOpen}
                onClick={() => setRecentOpen((open) => !open)}
              >
                <ChevronDown size={13} />
              </button>
            </Tooltip>
          </div>

          <div className="workspace-preview__window-actions">
            <Tooltip label={maximized ? t("workspace.restore") : t("workspace.maximize")}>
              <button className="workspace-iconbtn" onClick={onToggleMaximized}>
                {maximized ? <Minimize2 size={15} /> : <Maximize2 size={15} />}
              </button>
            </Tooltip>
            {selectedPath && (
              <Tooltip label={t("workspace.closePreview")}>
                <button className="workspace-iconbtn" onClick={() => closeTab(selectedPath)}>
                  <X size={15} />
                </button>
              </Tooltip>
            )}
          </div>
          <AnchoredPopover
            open={recentOpen}
            anchorRef={recentAnchorRef}
            onClose={() => setRecentOpen(false)}
            className="workspace-recent-menu"
            align="start"
            offset={6}
            placement="bottom"
          >
            <div className="workspace-recent-menu__title">{t("workspace.recentFiles")}</div>
            <div className="workspace-recent-menu__list">
              {recentFiles.map((path) => (
                <button
                  key={path}
                  type="button"
                  className={`workspace-recent-menu__item${path === selectedPath ? " workspace-recent-menu__item--active" : ""}`}
                  onClick={() => {
                    setSelectedPath(path);
                    setRecentOpen(false);
                  }}
                >
                  <FileText size={14} />
                  <span>
                    <span className="workspace-recent-menu__name">{basename(path)}</span>
                    <span className="workspace-recent-menu__path">{parentPath(path)}</span>
                  </span>
                </button>
              ))}
            </div>
          </AnchoredPopover>
        </header>

        <div className="workspace-preview__meta">
          <Tooltip label={cwd}>
            <button
              className="workspace-crumb"
              onClick={() => {
                setFilter("");
                setTreeVisible(true);
                onRequestPanelWidth?.(WORKSPACE_DUAL_PANEL_TARGET_WIDTH);
                setOpenDirs((prev) => new Set([...Array.from(prev), ""]));
              }}
            >
              {shortCwd(cwd) || t("workspace.title")}
            </button>
          </Tooltip>
          {pathParts.map((part, index) => {
            const isLast = index === pathParts.length - 1;
            const dir = pathParts.slice(0, index + 1).join("/") + "/";
            return (
              <span className="workspace-crumb-group" key={`${part}-${index}`}>
                <span>›</span>
                <Tooltip label={isLast ? (selectedPath ?? undefined) : dir}>
                  <button
                    className={`workspace-crumb${isLast ? " workspace-crumb--current" : ""}`}
                    onClick={() => {
                      if (isLast) return;
                      setTreeVisible(true);
                      onRequestPanelWidth?.(WORKSPACE_DUAL_PANEL_TARGET_WIDTH);
                      setFilter("");
                      setOpenDirs((prev) => new Set([...Array.from(prev), ...breadcrumbDirs, dir]));
                      void loadDir(dir);
                    }}
                  >
                    {part}
                  </button>
                </Tooltip>
              </span>
            );
          })}
          {preview && preview.size > 0 && <span className="workspace-preview__size">{formatBytes(preview.size)}</span>}
        </div>

        <div className="workspace-preview__body" ref={previewBodyRef} onContextMenu={openSelectionMenu}>
          {!selectedPath ? (
            <div className="workspace-empty">{t("workspace.pickFile")}</div>
          ) : loadingPreview ? (
            <div className="workspace-empty">{t("workspace.loading")}</div>
          ) : preview?.err ? (
            <div className="workspace-empty workspace-empty--error">{preview.err}</div>
          ) : preview?.kind ? (
            renderMediaPreview(preview)
          ) : preview?.binary ? (
            <div className="workspace-empty">{t("workspace.binary")}</div>
          ) : preview ? (
            <>
              {preview.truncated && <div className="workspace-note">{t("workspace.truncated")}</div>}
              {isMarkdown ? (
                <Markdown text={preview.body} />
              ) : (
                <CodeViewer value={preview.body || " "} language={languageFor(selectedPath)} />
              )}
            </>
          ) : null}
          {selectionMenu && (
            <FloatingMenu x={selectionMenu.x} y={selectionMenu.y} estimatedHeight={WORKSPACE_CONTEXT_MENU_REF_HEIGHT}>
              <FloatingMenuItems
                items={[
                  {
                    icon: <MessageSquarePlus size={14} />,
                    label: t("workspace.addSelectionToChat"),
                    onSelect: addSelectionToChat,
                  },
                ]}
              />
            </FloatingMenu>
          )}
        </div>
      </section>}

      {previewVisible && !actualTreeVisible && (
        <section className="workspace-tree-rail" aria-label={t("workspace.showTree")}>
          <Tooltip label={t("workspace.showTree")} side="right">
            <button
              className="workspace-tree-reveal workspace-iconbtn workspace-iconbtn--on"
              type="button"
              aria-label={t("workspace.showTree")}
              onClick={() => {
                setTreeVisible(true);
                onRequestPanelWidth?.(WORKSPACE_DUAL_PANEL_TARGET_WIDTH);
              }}
            >
              <FolderTree size={15} />
            </button>
          </Tooltip>
        </section>
      )}

      {actualTreeVisible && previewVisible && (
        <button
          className="workspace-tree-resizer"
          type="button"
          role="separator"
          aria-orientation="vertical"
          aria-label={t("workspace.resizeTree")}
          aria-valuemin={WORKSPACE_TREE_MIN_WIDTH}
          aria-valuemax={WORKSPACE_TREE_MAX_WIDTH}
          aria-valuenow={effectiveTreeWidth}
          onPointerDown={startTreeResize}
          onKeyDown={resizeTreeWithKeyboard}
          onDoubleClick={() => setSavedTreeWidth(WORKSPACE_TREE_DEFAULT_WIDTH)}
        />
      )}

      <section className="workspace-files">
        {showFileTools && (
          <div className={`workspace-files__tools${embeddedDockMode ? " workspace-files__tools--embedded" : ""}`}>
            <Tooltip label={previewVisible ? t("workspace.hideTree") : t("workspace.close")}>
              <button
                className="workspace-iconbtn workspace-iconbtn--on"
                type="button"
                aria-label={previewVisible ? t("workspace.hideTree") : t("workspace.close")}
                onClick={hideTreeOrClosePanel}
              >
                {previewVisible ? <FolderX size={15} /> : <X size={15} />}
              </button>
            </Tooltip>
            {showViewTabs && (
              <div className="workspace-files__tabs" role="tablist" aria-label={t("workspace.viewMode")}>
                <button
                  className={viewMode === "files" ? "workspace-files__tab workspace-files__tab--active" : "workspace-files__tab"}
                  onClick={() => setViewMode("files")}
                >
                  {t("workspace.filesTab")}
                </button>
                <button
                  className={viewMode === "changed" ? "workspace-files__tab workspace-files__tab--active" : "workspace-files__tab"}
                  onClick={() => {
                    setViewMode("changed");
                    void loadChanges();
                  }}
                >
                  <GitBranch size={13} />
                  {t("workspace.changedTab")}
                </button>
              </div>
            )}
            {showViewTabs && (
              <Tooltip label={t("workspace.refreshChanges")}>
                <button className="workspace-iconbtn" onClick={() => void loadChanges()}>
                  <RefreshCw size={14} />
                </button>
              </Tooltip>
            )}
          </div>
        )}

        {viewMode === "changed" && changes?.gitBranch && (
          <div className="workspace-branch-indicator">
            <GitBranch size={13} />
            <button
              ref={branchBtnRef}
              className="workspace-branch-name"
              onClick={openBranchMenu}
            >
              <span>{changes.gitBranch}</span>
              <ChevronDown size={11} />
            </button>
            <AnchoredPopover
              open={branchMenuOpen}
              anchorRef={branchBtnRef}
              onClose={() => setBranchMenuOpen(false)}
              className="workspace-branch-menu"
              placement="bottom"
              align="start"
              offset={4}
            >
              <div className="workspace-branch-menu__inner">
                {branchList.length === 0 ? (
                  <div className="workspace-branch-menu__loading">{t("workspace.loading")}</div>
                ) : (
                  branchList.map((b) => (
                    <button
                      key={b}
                      type="button"
                      className={`workspace-branch-menu__item${b === changes.gitBranch ? " workspace-branch-menu__item--active" : ""}`}
                      onClick={() => switchBranch(b)}
                      disabled={switchingBranch}
                    >
                      {b === changes.gitBranch && <Check size={13} />}
                      <span>{b}</span>
                    </button>
                  ))
                )}
              </div>
            </AnchoredPopover>
            <button
              className="workspace-branch-refresh"
              onClick={loadChanges}
              disabled={loadingChanges}
              title={t("workspace.refreshChanges")}
            >
              <RefreshCw size={12} className={loadingChanges ? "spinning" : ""} />
            </button>
          </div>
        )}

        <div className="workspace-search">
          <Search size={14} />
          <input ref={filterRef} value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={searchPlaceholder} />
        </div>
        {viewMode === "changed" && changes && !changes.gitAvailable && changes.gitErr && (
          <div className="workspace-note workspace-note--compact">{t("workspace.gitUnavailable")}</div>
        )}
        <div className="workspace-tree" onContextMenu={openTreeBlankMenu}>
          {viewMode === "changed"
            ? renderChangedRows()
            : flattened
            ? flattened.map(({ path, entry }) => {
                const dir = parentPath(path);
                return (
                  <button
                    key={path}
                    className={`workspace-tree__row workspace-tree__row--search${selectedPath === path ? " workspace-tree__row--active" : ""}`}
                    draggable
                    onDragStart={(event) => startTreeDrag(event, path, entry.isDir)}
                    onClick={() => (entry.isDir ? toggleDir(path) : selectFile(path))}
                    onContextMenu={(event) => openTreeMenu(event, path, entry.isDir)}
                  >
                    {entry.isDir ? (
                      <Folder size={14} className="workspace-tree__icon workspace-tree__icon--dir" />
                    ) : (
                      <FileText size={14} className="workspace-tree__icon" />
                    )}
                    <span className="workspace-tree__result">
                      <span className="workspace-tree__result-name">{basename(path)}</span>
                      {dir && <span className="workspace-tree__result-dir">{dir}</span>}
                    </span>
                  </button>
                );
              })
            : renderRows("", 0)}
        </div>
      </section>
      {treeMenu && (
        <FloatingMenu
          x={treeMenu.x}
          y={treeMenu.y}
          estimatedHeight={treeMenu.isDir ? WORKSPACE_CONTEXT_MENU_REF_HEIGHT : WORKSPACE_CONTEXT_MENU_FILE_HEIGHT}
          className="workspace-tree-menu"
        >
          <FloatingMenuItems
            items={[
              {
                icon: <MessageSquarePlus size={14} />,
                label: treeMenu.isDir ? t("workspace.addFolderReferenceToChat") : t("workspace.addFileReferenceToChat"),
                onSelect: addTreeReferenceToChat,
              },
              ...(treeMenu.isDir
                ? []
                : [
                    {
                      icon: <FileText size={14} />,
                      label: t("workspace.addFileContentToChat"),
                      onSelect: () => void addTreeFileToChat(),
                    },
                  ]),
            ]}
          />
        </FloatingMenu>
      )}
      <ContextMenu
        open={Boolean(treeBlankMenuPoint)}
        point={treeBlankMenuPoint}
        items={treeBlankMenuItems}
        minWidth={150}
        ariaLabel={t("workspace.treeMenu")}
        onClose={() => setTreeBlankMenuPoint(null)}
      />
    </aside>
  );
}
