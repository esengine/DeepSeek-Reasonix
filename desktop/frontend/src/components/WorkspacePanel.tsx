import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type {
  CSSProperties,
  DragEvent as ReactDragEvent,
  KeyboardEvent,
  MouseEvent as ReactMouseEvent,
  PointerEvent as ReactPointerEvent,
  ReactElement,
} from "react";
import {
  ChevronDown,
  ChevronRight,
  FileText,
  Folder,
  FolderOpen,
  FolderTree,
  FolderX,
  GitBranch,
  Maximize2,
  MessageSquarePlus,
  Minimize2,
  RefreshCw,
  Search,
  X,
} from "lucide-react";
import { asArray } from "../lib/array";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import {
  clampWorkspaceSplitTreeWidth,
  initialWorkspaceSplitTreeWidth,
  resolveWorkspaceSplitTreeWidth,
  shouldInitializeWorkspaceSplitOnFileSelect,
  type WorkspaceSplitTreeWidthMode,
  workspaceSplitCanFit,
  workspaceSplitTreeWidthFromPointer,
} from "../lib/workspaceSplit";
import { createRafResizeUpdater } from "../lib/resizeDrag";
import { closeWorkspacePreviewTab } from "../lib/workspacePreviewTabs";
import { shouldScrollWorkspaceTreeSelection } from "../lib/workspaceTreeReveal";
import { mergeWorkspaceSearchResults } from "../lib/workspaceTreeSearch";
import type { DirEntry, FilePreview, GitCommitView, GitCommitDetailView, WorkspaceChangesView } from "../lib/types";
import { formatWorkspaceReference, WORKSPACE_REF_DRAG_TYPE } from "../lib/workspaceDrag";
import { cleanGitDiff } from "../lib/diff";
import { CodeViewer } from "./CodeViewer";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";
import { FloatingMenu, FloatingMenuItems } from "./FloatingMenu";
import { Markdown } from "./Markdown";
import { Tooltip } from "./Tooltip";
import { AnchoredPopover } from "./AnchoredPopover";

const WORKSPACE_TREE_MIN_WIDTH = 140;
const WORKSPACE_TREE_DEFAULT_WIDTH = 300;
const WORKSPACE_TREE_RAIL_WIDTH = 44;
const WORKSPACE_PREVIEW_MIN_WIDTH = 140;
const WORKSPACE_PREVIEW_TARGET_WIDTH = 360;
const WORKSPACE_DUAL_PANEL_TARGET_WIDTH = WORKSPACE_TREE_DEFAULT_WIDTH + WORKSPACE_PREVIEW_TARGET_WIDTH;
const WORKSPACE_CONTEXT_MENU_FILE_HEIGHT = 136;
const WORKSPACE_CONTEXT_MENU_REF_HEIGHT = 92;
const WORKSPACE_CONTEXT_MENU_SELECTION_HEIGHT = 48;
const WORKSPACE_MAX_PREVIEW_TABS = 5;

type WorkspaceRevealRequest = { id: number; path: string };
type WorkspaceFileListRequest = { id: number; paths: string[] };
type WorkspaceChangeListEntry = { key: string; path: string; meta: string; time: string; detail: string };
type WorkspaceChangeListRequest = { id: number; changes: WorkspaceChangeListEntry[] };

function clampWorkspaceTreeWidth(width: number, panelWidth?: number): number {
  return clampWorkspaceSplitTreeWidth({
    width,
    panelWidth,
    railWidth: WORKSPACE_TREE_RAIL_WIDTH,
    treeMinWidth: WORKSPACE_TREE_MIN_WIDTH,
    previewMinWidth: WORKSPACE_PREVIEW_MIN_WIDTH,
  });
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

function renderMediaPreview(preview: FilePreview): ReactElement | null {
  if (!preview.url) return null;
  if (preview.kind === "image") {
    return (
      <div className="workspace-media workspace-media--image">
        <img src={preview.url} alt={basename(preview.path)} decoding="async" draggable={false} />
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

function formatCommitDate(dateStr: string): string {
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return dateStr;
  const day = String(d.getDate()).padStart(2, "0");
  const monthNames = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
  const month = monthNames[d.getMonth()];
  const year = d.getFullYear();
  const hours = String(d.getHours()).padStart(2, "0");
  const minutes = String(d.getMinutes()).padStart(2, "0");
  return `${day} ${month} ${year} ${hours}:${minutes}`;
}
interface TreeRow {
  key: string;
  path: string;
  depth: number;
  entry: DirEntry;
  active: boolean;
  isOpen?: boolean;
  isSearch?: boolean;
}

export function WorkspacePanel({
  open,
  tabId,
  cwd,
  maximized,
  panelWidth,
  onClose,
  onToggleMaximized,
  onPreviewModeChange,
  onAddToChat,
  onRequestPanelWidth,
  onFileTreeRefresh,
  refreshKey,
  initialViewMode = "files",
  revealPathRequest,
  changeRevealRequest,
  fileListRequest,
  changeListRequest,
  showViewTabs = true,
  workspaceScopeKey: workspaceScopeKeyProp,
  creationMode = false,
}: {
  open: boolean;
  tabId?: string;
  cwd?: string;
  maximized: boolean;
  panelWidth?: number;
  onClose: () => void;
  onToggleMaximized: () => void;
  onPreviewModeChange?: (active: boolean) => void;
  onAddToChat?: (text: string) => void;
  onRequestPanelWidth?: (width: number) => void;
  onFileTreeRefresh?: () => void;
  refreshKey?: number;
  initialViewMode?: "files" | "changed";
  revealPathRequest?: WorkspaceRevealRequest | null;
  changeRevealRequest?: WorkspaceRevealRequest | null;
  fileListRequest?: WorkspaceFileListRequest | null;
  changeListRequest?: WorkspaceChangeListRequest | null;
  showViewTabs?: boolean;
  workspaceScopeKey?: string;
  creationMode?: boolean;
}) {
  const t = useT();
  const workspaceTabId = tabId ?? "";
  const workspaceScopeKey = workspaceScopeKeyProp ?? `${workspaceTabId}\u0000${cwd ?? ""}`;
  const panelRef = useRef<HTMLElement>(null);
  const treeRef = useRef<HTMLDivElement>(null);
  const filterRef = useRef<HTMLInputElement>(null);
  const previewBodyRef = useRef<HTMLDivElement>(null);
  const [entriesByDir, setEntriesByDir] = useState<Record<string, DirEntry[]>>({});
  const [openDirs, setOpenDirs] = useState<Set<string>>(() => new Set([""]));
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [openTabs, setOpenTabs] = useState<string[]>([]);
  const [preview, setPreview] = useState<FilePreview | null>(null);
  const [loadingPreview, setLoadingPreview] = useState(false);
  const [viewMode, setViewMode] = useState<"files" | "changed">(initialViewMode);
  const [gitHistory, setGitHistory] = useState<GitCommitView[]>([]);
  const [workspaceChanges, setWorkspaceChanges] = useState<WorkspaceChangesView | null>(null);
  const [loadingHistory, setLoadingHistory] = useState(false);
  const [expandedCommit, setExpandedCommit] = useState<string | null>(null);
  const [commitDetail, setCommitDetail] = useState<GitCommitDetailView | null>(null);
  const [loadingCommit, setLoadingCommit] = useState(false);
  const [selectionMenu, setSelectionMenu] = useState<{ x: number; y: number; text: string; path: string } | null>(null);
  const [treeMenu, setTreeMenu] = useState<{ x: number; y: number; path: string; isDir: boolean } | null>(null);
  const [treeBlankMenuPoint, setTreeBlankMenuPoint] = useState<ContextMenuPoint | null>(null);
  const [filter, setFilter] = useState("");
  const [searchResults, setSearchResults] = useState<DirEntry[] | null>(null);
  const [scopedFilePaths, setScopedFilePaths] = useState<string[] | null>(null);
  const [scopedChangeRows, setScopedChangeRows] = useState<WorkspaceChangeListEntry[] | null>(null);
  const [treeVisible, setTreeVisible] = useState(true);
  const [treeWidth, setTreeWidth] = useState(WORKSPACE_TREE_DEFAULT_WIDTH);
  const [treeWidthMode, setTreeWidthMode] = useState<WorkspaceSplitTreeWidthMode>("manual");
  const [treeResizing, setTreeResizing] = useState(false);
  const [recentOpen, setRecentOpen] = useState(false);
  /** Changes overview: commit history is secondary and starts collapsed. */
  const [commitHistoryOpen, setCommitHistoryOpen] = useState(false);
  const lastPreviewModeActiveRef = useRef<boolean | null>(null);
  const lastRevealRequestIdRef = useRef<number | null>(null);
  const dismissedRevealRequestIdRef = useRef<number | null>(null);
  const lastChangeRevealRequestIdRef = useRef<number | null>(null);
  const dismissedChangeRevealRequestIdRef = useRef<number | null>(null);
  const lastFileListRequestIdRef = useRef<number | null>(null);
  const dismissedFileListRequestIdRef = useRef<number | null>(null);
  const lastChangeListRequestIdRef = useRef<number | null>(null);
  const dismissedChangeListRequestIdRef = useRef<number | null>(null);
  const currentWorkspaceScopeKeyRef = useRef(workspaceScopeKey);
  const lastWorkspaceScopeKeyRef = useRef(workspaceScopeKey);
  const workspaceChangesRequestIdRef = useRef(0);
  const gitHistoryRequestIdRef = useRef(0);
  const commitDetailRequestIdRef = useRef(0);
  const dirLoadGenerationRef = useRef(0);
  const dirLoadRequestIdsRef = useRef<Record<string, number>>({});
  const recentAnchorRef = useRef<HTMLButtonElement>(null);
  const openDirsRef = useRef(openDirs);
  const pendingTreeRevealPathRef = useRef<string | null>(null);
  currentWorkspaceScopeKeyRef.current = workspaceScopeKey;

  useEffect(() => {
    openDirsRef.current = openDirs;
  }, [openDirs]);

  const loadDir = useCallback(async (dir: string) => {
    const requestTabId = workspaceTabId;
    const requestScopeKey = workspaceScopeKey;
    const generation = dirLoadGenerationRef.current;
    const requestId = (dirLoadRequestIdsRef.current[dir] ?? 0) + 1;
    dirLoadRequestIdsRef.current[dir] = requestId;
    const entries = await app.ListDirForTab(requestTabId, dir).catch((): DirEntry[] => []);
    if (
      currentWorkspaceScopeKeyRef.current !== requestScopeKey ||
      dirLoadGenerationRef.current !== generation ||
      dirLoadRequestIdsRef.current[dir] !== requestId
    ) return;
    setEntriesByDir((prev) => ({ ...prev, [dir]: asArray(entries) }));
  }, [workspaceScopeKey, workspaceTabId]);

  const loadGitHistory = useCallback(async () => {
    const requestId = ++gitHistoryRequestIdRef.current;
    const requestTabId = workspaceTabId;
    const requestScopeKey = workspaceScopeKey;
    setLoadingHistory(true);
    try {
      const result = await app.WorkspaceGitHistory(requestTabId, selectedPath || "");
      if (gitHistoryRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
        setGitHistory(result || []);
      }
    } catch (err) {
      if (gitHistoryRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
        setGitHistory([]);
      }
    } finally {
      if (gitHistoryRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
        setLoadingHistory(false);
      }
    }
  }, [selectedPath, workspaceScopeKey, workspaceTabId]);

  const loadWorkspaceChanges = useCallback(async () => {
    const requestId = ++workspaceChangesRequestIdRef.current;
    const requestTabId = workspaceTabId;
    const requestScopeKey = workspaceScopeKey;
    try {
      const result = await app.WorkspaceChanges(requestTabId);
      if (workspaceChangesRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
        setWorkspaceChanges({
          files: Array.isArray(result?.files) ? result.files : [],
          gitAvailable: result?.gitAvailable !== false,
          gitErr: result?.gitErr,
          gitBranch: result?.gitBranch,
        });
      }
    } catch {
      if (workspaceChangesRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
        setWorkspaceChanges({ files: [], gitAvailable: false });
      }
    }
  }, [workspaceScopeKey, workspaceTabId]);

  const toggleCommit = useCallback((hash: string) => {
    setExpandedCommit((prev) => {
      const next = prev === hash ? null : hash;
      if (next) onRequestPanelWidth?.(WORKSPACE_DUAL_PANEL_TARGET_WIDTH);
      return next;
    });
  }, [onRequestPanelWidth]);

  useEffect(() => {
    if (!open) return;
    if (expandedCommit) {
      const requestId = ++commitDetailRequestIdRef.current;
      const requestTabId = workspaceTabId;
      const requestScopeKey = workspaceScopeKey;
      let live = true;
      setLoadingCommit(true);
      app
        .WorkspaceGitCommitDetail(requestTabId, expandedCommit, selectedPath || "")
        .then((detail) => {
          if (live && commitDetailRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
            setCommitDetail(detail);
          }
        })
        .catch(() => {
          if (live && commitDetailRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
            setCommitDetail(null);
          }
        })
        .finally(() => {
          if (live && commitDetailRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
            setLoadingCommit(false);
          }
        });
      return () => {
        live = false;
      };
    } else {
      commitDetailRequestIdRef.current += 1;
      setCommitDetail(null);
    }
  }, [expandedCommit, selectedPath, open, workspaceScopeKey, workspaceTabId]);

  const selectFile = useCallback(
    (path: string) => {
      const initializeSplit = shouldInitializeWorkspaceSplitOnFileSelect({
        previewVisible: openTabs.length > 0 || selectedPath !== null,
        treeVisible,
      });
      if (initializeSplit) {
        setTreeWidth(initialWorkspaceSplitTreeWidth({
          panelWidth,
          railWidth: WORKSPACE_TREE_RAIL_WIDTH,
          savedTreeWidth: null,
          treeMinWidth: WORKSPACE_TREE_MIN_WIDTH,
          previewMinWidth: WORKSPACE_PREVIEW_MIN_WIDTH,
        }));
        setTreeWidthMode("even");
      }
      pendingTreeRevealPathRef.current = path;
      setSelectedPath(path);
      setScopedFilePaths((current) => {
        if (current) dismissedFileListRequestIdRef.current = lastFileListRequestIdRef.current;
        return null;
      });
      setScopedChangeRows((current) => {
        if (current) dismissedChangeListRequestIdRef.current = lastChangeListRequestIdRef.current;
        return null;
      });
      setFilter("");
      setOpenTabs((tabs) => [...tabs.filter((tab) => tab !== path), path].slice(-WORKSPACE_MAX_PREVIEW_TABS));
      const dirs = parentDirs(path);
      setOpenDirs((prev) => new Set([...Array.from(prev), ...dirs]));
      dirs.forEach((dir) => void loadDir(dir));
    },
    [loadDir, openTabs.length, panelWidth, selectedPath, treeVisible],
  );

  useEffect(() => {
    if (!open) return;
    dirLoadGenerationRef.current += 1;
    dirLoadRequestIdsRef.current = {};
    setEntriesByDir({});
    setOpenDirs(new Set([""]));
    setSelectedPath(null);
    setOpenTabs([]);
    setPreview(null);
    setGitHistory([]);
    setExpandedCommit(null);
    setCommitDetail(null);
    setSelectionMenu(null);
    setTreeMenu(null);
    setFilter("");
    setScopedFilePaths(null);
    setScopedChangeRows(null);
    setTreeVisible(true);
    void loadDir("");
  }, [cwd, loadDir, open]);

  useEffect(() => {
    if (!open) return;
    if (lastWorkspaceScopeKeyRef.current === workspaceScopeKey) return;
    lastWorkspaceScopeKeyRef.current = workspaceScopeKey;
    workspaceChangesRequestIdRef.current += 1;
    gitHistoryRequestIdRef.current += 1;
    commitDetailRequestIdRef.current += 1;
    setWorkspaceChanges(null);
    setGitHistory([]);
    setCommitHistoryOpen(false);
    setExpandedCommit(null);
    setCommitDetail(null);
    setScopedChangeRows(null);
    lastChangeRevealRequestIdRef.current = null;
    dismissedChangeRevealRequestIdRef.current = null;
    lastChangeListRequestIdRef.current = null;
    dismissedChangeListRequestIdRef.current = null;
    if (viewMode === "changed") {
      setSelectedPath(null);
      setOpenTabs([]);
      setPreview(null);
    }
  }, [open, viewMode, workspaceScopeKey]);

  useEffect(() => {
    if (!open) return;
    setViewMode(initialViewMode);
    setCommitHistoryOpen(false);
    setExpandedCommit(null);
    setCommitDetail(null);
    setSelectionMenu(null);
    setTreeMenu(null);
    setRecentOpen(false);
    if (initialViewMode === "changed") {
      setScopedFilePaths(null);
      setSelectedPath(null);
      setOpenTabs([]);
      setPreview(null);
      return;
    }
    setScopedChangeRows(null);
    setTreeVisible(true);
  }, [initialViewMode, open]);

  useEffect(() => {
    if (!open || fileListRequest) return;
    lastFileListRequestIdRef.current = null;
    dismissedFileListRequestIdRef.current = null;
    setScopedFilePaths(null);
  }, [fileListRequest, open]);

  useEffect(() => {
    if (!open || !fileListRequest) return;
    const paths = Array.from(new Set(fileListRequest.paths.map((path) => path.trim()).filter(Boolean)));
    const scopedPathsSettled =
      scopedFilePaths !== null &&
      scopedFilePaths.length === paths.length &&
      scopedFilePaths.every((path, index) => path === paths[index]);
    if (dismissedFileListRequestIdRef.current === fileListRequest.id) return;
    if (lastFileListRequestIdRef.current === fileListRequest.id && viewMode === "files" && scopedPathsSettled) return;
    lastFileListRequestIdRef.current = fileListRequest.id;
    dismissedFileListRequestIdRef.current = null;
    if (paths.length === 0) {
      setScopedFilePaths(null);
      return;
    }
    setViewMode("files");
    setTreeVisible(true);
    setScopedFilePaths(paths);
    setSelectedPath(null);
    setOpenTabs([]);
    setPreview(null);
    setFilter("");
    setExpandedCommit(null);
    setCommitDetail(null);
    setSelectionMenu(null);
    setTreeMenu(null);
    const dirs = Array.from(new Set(paths.flatMap(parentDirs)));
    setOpenDirs((prev) => new Set([...Array.from(prev), ...dirs]));
    dirs.forEach((dir) => void loadDir(dir));
  }, [fileListRequest, loadDir, open, scopedFilePaths, viewMode]);

  useEffect(() => {
    if (!open || changeListRequest) return;
    lastChangeListRequestIdRef.current = null;
    dismissedChangeListRequestIdRef.current = null;
    setScopedChangeRows(null);
  }, [changeListRequest, open]);

  useEffect(() => {
    if (!open || !changeListRequest) return;
    const changes = changeListRequest.changes
      .map((change) => ({ ...change, path: change.path.trim() }))
      .filter((change) => change.path.length > 0);
    const scopedChangesSettled =
      scopedChangeRows !== null &&
      scopedChangeRows.length === changes.length &&
      scopedChangeRows.every((change, index) => change.path === changes[index]?.path);
    if (dismissedChangeListRequestIdRef.current === changeListRequest.id) return;
    if (lastChangeListRequestIdRef.current === changeListRequest.id && viewMode === "changed" && scopedChangesSettled) return;
    lastChangeListRequestIdRef.current = changeListRequest.id;
    dismissedChangeListRequestIdRef.current = null;
    if (changes.length === 0) {
      setScopedChangeRows(null);
      return;
    }
    setViewMode("changed");
    setScopedChangeRows(changes);
    setScopedFilePaths(null);
    setSelectedPath(null);
    setOpenTabs([]);
    setPreview(null);
    setFilter("");
    setExpandedCommit(null);
    setCommitDetail(null);
    setSelectionMenu(null);
    setTreeMenu(null);
    void loadGitHistory();
  }, [changeListRequest, loadGitHistory, open, scopedChangeRows, viewMode]);

  useEffect(() => {
    if (!open || revealPathRequest) return;
    lastRevealRequestIdRef.current = null;
    dismissedRevealRequestIdRef.current = null;
  }, [open, revealPathRequest]);

  useEffect(() => {
    if (!open || !revealPathRequest) return;
    if (dismissedRevealRequestIdRef.current === revealPathRequest.id) return;
    if (
      lastRevealRequestIdRef.current === revealPathRequest.id &&
      selectedPath === revealPathRequest.path &&
      viewMode === "files"
    ) {
      return;
    }
    lastRevealRequestIdRef.current = revealPathRequest.id;
    dismissedRevealRequestIdRef.current = null;
    setViewMode("files");
    setTreeVisible(true);
    setScopedFilePaths(null);
    setScopedChangeRows(null);
    setExpandedCommit(null);
    setCommitDetail(null);
    selectFile(revealPathRequest.path);
  }, [open, revealPathRequest, selectFile, selectedPath, viewMode]);

  useEffect(() => {
    if (!open || changeRevealRequest) return;
    lastChangeRevealRequestIdRef.current = null;
    dismissedChangeRevealRequestIdRef.current = null;
  }, [changeRevealRequest, open]);

  useEffect(() => {
    if (!open || !changeRevealRequest) return;
    if (dismissedChangeRevealRequestIdRef.current === changeRevealRequest.id) return;
    if (
      lastChangeRevealRequestIdRef.current === changeRevealRequest.id &&
      selectedPath === changeRevealRequest.path &&
      viewMode === "changed"
    ) {
      return;
    }
    lastChangeRevealRequestIdRef.current = changeRevealRequest.id;
    dismissedChangeRevealRequestIdRef.current = null;
    setViewMode("changed");
    setScopedFilePaths(null);
    setScopedChangeRows(null);
    setSelectedPath(changeRevealRequest.path);
    setOpenTabs([]);
    setPreview(null);
    setFilter("");
    setExpandedCommit(null);
    setCommitDetail(null);
    setSelectionMenu(null);
    setTreeMenu(null);
  }, [changeRevealRequest, open, selectedPath, viewMode]);

  useEffect(() => {
    if (!open) return;
    if (viewMode === "changed") {
      void loadGitHistory();
      void loadWorkspaceChanges();
    }
  }, [selectedPath, viewMode, loadGitHistory, loadWorkspaceChanges, open]);

  useEffect(() => {
    if (!open || !refreshKey) return;
    if (viewMode === "changed") {
      void loadGitHistory();
      void loadWorkspaceChanges();
    }
    openDirsRef.current.forEach((dir) => void loadDir(dir));
  }, [loadGitHistory, loadWorkspaceChanges, loadDir, open, refreshKey, viewMode]);

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
      void loadGitHistory();
      void loadWorkspaceChanges();
      return;
    }
    onFileTreeRefresh?.();
    const dirs = Array.from(openDirsRef.current);
    dirs.forEach((dir) => void loadDir(dir));
  }, [loadGitHistory, loadWorkspaceChanges, loadDir, onFileTreeRefresh, viewMode]);

  const refreshSelected = useCallback(() => {
    if (!selectedPath) return;
    let live = true;
    setLoadingPreview(true);
    app
      .ReadFileForTab(workspaceTabId, selectedPath)
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
  }, [selectedPath, workspaceScopeKey, workspaceTabId]);



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
          void loadDir(dir);
        }
        return next;
      });
    },
    [loadDir],
  );

  const breadcrumbDirs = selectedPath ? parentDirs(selectedPath) : [""];
  const pathParts = selectedPath?.split("/").filter(Boolean) ?? [];
  const sessionChanges = useMemo(
    () => workspaceChanges?.files.filter((c) => c.sources.includes("session")) ?? [],
    [workspaceChanges],
  );
  /** Working-tree files that are not already covered by the session section. */
  const gitWorkingChanges = useMemo(
    () =>
      workspaceChanges?.files.filter(
        (c) => c.sources.includes("git") && !c.sources.includes("session"),
      ) ?? [],
    [workspaceChanges],
  );
  const hasFileChanges = sessionChanges.length > 0 || gitWorkingChanges.length > 0;
  const workspaceGitWarning = workspaceChanges && (!workspaceChanges.gitAvailable || workspaceChanges.gitErr?.trim())
    ? t("workspace.gitUnavailable")
    : null;

  const renderChangeScope = (title: string, changes: typeof sessionChanges) => (
    <div className="workspace-change-scope">
      <div className="workspace-change-scope__head">
        <span className="workspace-change-scope__title">{title}</span>
        <span className="workspace-change-scope__meta">{t("context.changedMeta", { count: changes.length })}</span>
      </div>
      <div className="workspace-change-scope__list">
        {changes.map((change) => {
          const dir = parentPath(change.path);
          return (
            <button
              key={change.path}
              className="workspace-change"
              type="button"
              onClick={() => selectFile(change.path)}
            >
              <FileText size={14} />
              <span className="workspace-change__body">
                <span className="workspace-change__name">{basename(change.path)}</span>
                {dir && <span className="workspace-change__path">{dir}</span>}
                {change.latestPrompt && <span className="workspace-change__detail">{change.latestPrompt}</span>}
              </span>
              <span className="workspace-change__meta">
                {change.gitStatus && <span className="workspace-change__badge workspace-change__badge--git">{change.gitStatus}</span>}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );

  const changedMode = viewMode === "changed";
  const currentFileName = selectedPath ? basename(selectedPath) : t("workspace.noFile");
  const currentFileDir = selectedPath ? parentPath(selectedPath) : "";
  const previewTitle = changedMode && !selectedPath
    ? scopedChangeRows ? t("context.sessionChanges") : t("workspace.changedTab")
    : currentFileName;
  const previewSubtitle = changedMode && !selectedPath
    ? scopedChangeRows ? t("context.changedMeta", { count: scopedChangeRows.length }) : shortCwd(cwd) || t("workspace.title")
    : currentFileDir;
  const recentFiles = useMemo(() => [...openTabs].reverse(), [openTabs]);

  useEffect(() => {
    const q = filter.trim();
    if (!open || viewMode === "changed" || !q || scopedFilePaths) {
      setSearchResults(null);
      return;
    }
    let cancelled = false;
    app.SearchFileRefsForTab(workspaceTabId, q).then((results) => {
      if (!cancelled) setSearchResults(asArray(results));
    }).catch(() => {
      if (!cancelled) setSearchResults(null);
    });
    return () => { cancelled = true; };
  }, [filter, viewMode, scopedFilePaths, open, workspaceScopeKey, workspaceTabId]);

  const flattened = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (scopedFilePaths) {
      return scopedFilePaths
        .map((path) => ({ path, entry: { name: basename(path), isDir: false } }))
        .filter((row) => !q || row.path.toLowerCase().includes(q))
        .sort((a, b) => a.path.localeCompare(b.path));
    }
    const rows: { path: string; entry: DirEntry }[] = [];
    for (const [dir, entries] of Object.entries(entriesByDir)) {
      for (const entry of entries) {
        rows.push({ path: entryPath(dir, entry), entry });
      }
    }
    if (!q) return null;
    return mergeWorkspaceSearchResults(rows, searchResults)
      .filter((row) => row.path.toLowerCase().includes(q))
      .sort((a, b) => a.path.localeCompare(b.path));
  }, [entriesByDir, filter, scopedFilePaths, searchResults]);

  const treeRows = useMemo<TreeRow[]>(() => {
    if (flattened) {
      return flattened.map(({ path, entry }) => ({
        key: path,
        path,
        depth: 0,
        entry,
        active: selectedPath === path,
        isSearch: true,
      }));
    }
    const acc: TreeRow[] = [];
    const build = (dir: string, depth: number) => {
      const entries = entriesByDir[dir] ?? [];
      for (const entry of entries) {
        const path = entryPath(dir, entry);
        const isOpen = openDirs.has(path);
        const active = selectedPath === path;
        acc.push({
          key: path,
          path,
          depth,
          entry,
          active,
          isOpen,
        });
        if (entry.isDir && isOpen) {
          build(path, depth + 1);
        }
      }
    };
    build("", 0);
    return acc;
  }, [flattened, entriesByDir, openDirs, selectedPath]);

  const virtualizer = useVirtualizer({
    count: treeRows.length,
    getScrollElement: () => treeRef.current,
    estimateSize: () => 24,
    overscan: 10,
  });

  const searchPlaceholder = t(scopedFilePaths ? "workspace.filterReferencedFiles" : changedMode ? "workspace.filterChanges" : "workspace.filter");

  const filePreviewActive = openTabs.length > 0 || selectedPath !== null;
  const changeDetailActive = changedMode && expandedCommit !== null;
  const previewVisible = changedMode || filePreviewActive;
  const showTreeRail = previewVisible && !changedMode;
  const splitPanesFit = useMemo(
    () =>
      workspaceSplitCanFit({
        panelWidth,
        railWidth: WORKSPACE_TREE_RAIL_WIDTH,
        treeMinWidth: WORKSPACE_TREE_MIN_WIDTH,
        previewMinWidth: WORKSPACE_PREVIEW_MIN_WIDTH,
      }),
    [panelWidth],
  );
  const actualTreeVisible = changedMode ? false : treeVisible && (!previewVisible || splitPanesFit);
  const previewModeActive = open && (filePreviewActive || changeDetailActive);
  const embeddedDockMode = !showViewTabs;
  const showFileTools = showViewTabs || filePreviewActive;
  const effectiveTreeWidth = useMemo(
    () =>
      resolveWorkspaceSplitTreeWidth({
        mode: treeWidthMode,
        currentTreeWidth: treeWidth,
        panelWidth,
        railWidth: WORKSPACE_TREE_RAIL_WIDTH,
        treeMinWidth: WORKSPACE_TREE_MIN_WIDTH,
        previewMinWidth: WORKSPACE_PREVIEW_MIN_WIDTH,
      }),
    [panelWidth, treeWidth, treeWidthMode],
  );
  const maxTreeWidthForPanel = useMemo(
    () => Math.max(WORKSPACE_TREE_MIN_WIDTH, (panelWidth ?? WORKSPACE_DUAL_PANEL_TARGET_WIDTH) - WORKSPACE_TREE_RAIL_WIDTH - WORKSPACE_PREVIEW_MIN_WIDTH),
    [panelWidth],
  );

  useEffect(() => {
    const pendingRevealPath = pendingTreeRevealPathRef.current;
    if (!pendingRevealPath) return;
    if (!selectedPath || pendingRevealPath !== selectedPath) {
      pendingTreeRevealPathRef.current = null;
      return;
    }
    const selectedIndex = treeRows.findIndex((row) => row.path === selectedPath);
    if (!shouldScrollWorkspaceTreeSelection({ selectedPath, pendingRevealPath, actualTreeVisible, selectedIndex })) return;
    virtualizer.scrollToIndex(selectedIndex, { align: "auto" });
    pendingTreeRevealPathRef.current = null;
  }, [selectedPath, actualTreeVisible, treeRows, virtualizer]);

  const panelStyle = useMemo(
    () =>
      ({
        "--workspace-tree-width": `${effectiveTreeWidth}px`,
        "--workspace-preview-min-width": `${WORKSPACE_PREVIEW_MIN_WIDTH}px`,
      }) as CSSProperties,
    [effectiveTreeWidth],
  );

  useEffect(() => {
    if (lastPreviewModeActiveRef.current === previewModeActive) return;
    lastPreviewModeActiveRef.current = previewModeActive;
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

  const showTreeEvenSplit = useCallback(() => {
    setTreeWidth(initialWorkspaceSplitTreeWidth({
      panelWidth,
      railWidth: WORKSPACE_TREE_RAIL_WIDTH,
      savedTreeWidth: null,
      treeMinWidth: WORKSPACE_TREE_MIN_WIDTH,
      previewMinWidth: WORKSPACE_PREVIEW_MIN_WIDTH,
    }));
    setTreeWidthMode("even");
    setTreeVisible(true);
  }, [panelWidth]);

  const toggleTreeRail = useCallback(() => {
    if (actualTreeVisible) {
      setTreeVisible(false);
      return;
    }
    showTreeEvenSplit();
  }, [actualTreeVisible, showTreeEvenSplit]);

  const closePreviewArea = useCallback(() => {
    if (lastRevealRequestIdRef.current === revealPathRequest?.id) {
      dismissedRevealRequestIdRef.current = revealPathRequest.id;
    }
    if (lastChangeRevealRequestIdRef.current === changeRevealRequest?.id) {
      dismissedChangeRevealRequestIdRef.current = changeRevealRequest.id;
    }
    const nextPreviewTabs = closeWorkspacePreviewTab(openTabs, selectedPath);
    setSelectedPath(nextPreviewTabs.selectedPath);
    setOpenTabs(nextPreviewTabs.openTabs);
    setPreview(null);
    setSelectionMenu(null);
    setTreeMenu(null);
    setRecentOpen(false);
    setTreeVisible(true);
  }, [changeRevealRequest, openTabs, revealPathRequest, selectedPath]);

  const setSavedTreeWidth = useCallback(
    (width: number) => {
      const next = clampWorkspaceTreeWidth(width, panelWidth);
      setTreeWidth(next);
      setTreeWidthMode("manual");
    },
    [panelWidth],
  );

  const startTreeResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (!treeVisible) return;
      const panel = panelRef.current;
      const rect = panel?.getBoundingClientRect();
      if (!panel || !rect) return;
      event.preventDefault();
      const committedTreeWidth = clampWorkspaceTreeWidth(effectiveTreeWidth, panelWidth);
      setTreeWidth(committedTreeWidth);
      setTreeWidthMode("manual");
      setTreeResizing(true);
      let nextWidth = committedTreeWidth;
      const liveResize = createRafResizeUpdater({
        target: panel,
        separator: event.currentTarget,
        cssVar: "--workspace-tree-width",
      });
      const onMove = (moveEvent: PointerEvent) => {
        nextWidth = workspaceSplitTreeWidthFromPointer({
          clientX: moveEvent.clientX,
          panelLeft: rect.left,
          panelWidth: rect.width,
          railWidth: WORKSPACE_TREE_RAIL_WIDTH,
          treeMinWidth: WORKSPACE_TREE_MIN_WIDTH,
          previewMinWidth: WORKSPACE_PREVIEW_MIN_WIDTH,
        });
        liveResize.schedule(nextWidth);
      };
      const onDone = () => {
        liveResize.flush();
        setTreeWidth(nextWidth);
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
    [effectiveTreeWidth, panelWidth, treeVisible],
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
        setSavedTreeWidth(maxTreeWidthForPanel);
      }
    },
    [effectiveTreeWidth, maxTreeWidthForPanel, setSavedTreeWidth],
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
    const requestTabId = workspaceTabId;
    const requestScopeKey = workspaceScopeKey;
    setTreeMenu(null);
    try {
      const file = await app.ReadFileForTab(requestTabId, target.path);
      if (currentWorkspaceScopeKeyRef.current !== requestScopeKey) return;
      if (file.err || file.binary || file.kind) {
        onAddToChat?.(formatWorkspaceReference(target.path, false));
        return;
      }
      const suffix = file.truncated ? `\n\n${t("workspace.truncated")}` : "";
      onAddToChat?.(formatSelectionReference(target.path, file.body) + suffix);
    } catch {
      if (currentWorkspaceScopeKeyRef.current !== requestScopeKey) return;
      onAddToChat?.(formatWorkspaceReference(target.path, false));
    }
  };

  const revealInFileManager = () => {
    if (!treeMenu) return;
    setTreeMenu(null);
    void app.RevealWorkspacePathForTab(workspaceTabId, treeMenu.path).catch(() => {});
  };

  const renderNormalRow = (row: TreeRow) => {
    const { path, depth, entry, isOpen, active } = row;
    return (
      <button
        key={path}
        className={`workspace-tree__row${active ? " workspace-tree__row--active" : ""}`}
        data-workspace-path={path}
        draggable
        onDragStart={(event) => startTreeDrag(event, path, entry.isDir)}
        onClick={() => {
          if (entry.isDir) {
            toggleDir(path);
          } else {
            if (selectedPath === path) {
              setSelectedPath(null);
            } else {
              selectFile(path);
            }
          }
        }}
        onContextMenu={(event) => openTreeMenu(event, path, entry.isDir)}
        style={{ paddingLeft: 8 + depth * 14 }}
      >
        {entry.isDir ? (
          <ChevronRight
            size={13}
            className={`workspace-tree__chev ${isOpen ? "workspace-tree__chev--open" : ""}`}
            style={{
              transition: "transform 0.15s ease",
              transform: isOpen ? "rotate(90deg)" : "rotate(0deg)",
            }}
          />
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
  };

  const renderSearchRow = (row: TreeRow) => {
    const { path, entry, active } = row;
    const dir = parentPath(path);
    return (
      <button
        key={path}
        className={`workspace-tree__row workspace-tree__row--search${active ? " workspace-tree__row--active" : ""}`}
        data-workspace-path={path}
        draggable
        onDragStart={(event) => startTreeDrag(event, path, entry.isDir)}
        onClick={() => {
          if (entry.isDir) {
            toggleDir(path);
          } else {
            if (selectedPath === path) {
              setSelectedPath(null);
            } else {
              selectFile(path);
            }
          }
        }}
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
  };

  const isMarkdown = selectedPath?.toLowerCase().endsWith(".md") ?? false;
  const codePreviewActive = Boolean(
    selectedPath &&
      !changedMode &&
      preview &&
      !loadingPreview &&
      !preview.err &&
      !preview.kind &&
      !preview.binary &&
      !isMarkdown,
  );
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
      className={`workspace-panel${embeddedDockMode ? " workspace-panel--embedded" : ""}${showTreeRail ? " workspace-panel--with-tree-rail" : ""}${changedMode ? " workspace-panel--detail-only" : ""}${changedMode && !selectedPath ? " workspace-panel--changed-overview" : ""}${previewVisible && actualTreeVisible ? " workspace-panel--split-preview" : ""}${actualTreeVisible ? "" : " workspace-panel--tree-hidden"}${previewVisible ? "" : " workspace-panel--preview-hidden"}${treeResizing ? " workspace-panel--tree-resizing" : ""}`}
      aria-label={t("workspace.title")}
      style={panelStyle}
    >
      {previewVisible && <section className="workspace-preview">
        <header className="workspace-preview__head">
          <div className="workspace-current-file" aria-label={t("workspace.currentFile")}>
            {changedMode && !selectedPath ? (
              <GitBranch size={15} className="workspace-current-file__icon" />
            ) : (
              <FileText size={15} className="workspace-current-file__icon" />
            )}
            <div className="workspace-current-file__text">
              <Tooltip label={selectedPath ?? undefined}>
                <span className="workspace-current-file__name">{previewTitle}</span>
              </Tooltip>
              {previewSubtitle && <span className="workspace-current-file__path">{previewSubtitle}</span>}
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
                <button className="workspace-iconbtn" onClick={closePreviewArea}>
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
                showTreeEvenSplit();
                setOpenDirs((prev) => new Set([...Array.from(prev), ""]));
                void loadDir("");
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
                      showTreeEvenSplit();
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

        <div
          className={`workspace-preview__body${codePreviewActive ? " workspace-preview__body--code" : ""}`}
          ref={previewBodyRef}
          onContextMenu={openSelectionMenu}
        >
          {viewMode === "changed" && scopedChangeRows ? (
            <div className="workspace-change-scope">
              <div className="workspace-change-scope__head">
                <span className="workspace-change-scope__title">{t("context.sessionChanges")}</span>
                <span className="workspace-change-scope__meta">{t("context.changedMeta", { count: scopedChangeRows.length })}</span>
                <Tooltip label={t("workspace.clearChangeScope")}>
                  <button
                    type="button"
                    aria-label={t("workspace.clearChangeScope")}
                    onClick={() => {
                      dismissedChangeListRequestIdRef.current = lastChangeListRequestIdRef.current;
                      setScopedChangeRows(null);
                      setSelectedPath(null);
                      setExpandedCommit(null);
                      setCommitDetail(null);
                      void loadGitHistory();
                    }}
                  >
                    <X size={12} />
                  </button>
                </Tooltip>
              </div>
              <div className="workspace-change-scope__list">
                {scopedChangeRows.map((change) => {
                  const dir = parentPath(change.path);
                  return (
                    <button
                      key={change.key}
                      className="workspace-change"
                      type="button"
                      onClick={() => {
                        dismissedChangeListRequestIdRef.current = lastChangeListRequestIdRef.current;
                        setScopedChangeRows(null);
                        selectFile(change.path);
                      }}
                    >
                      <FileText size={14} />
                      <span className="workspace-change__body">
                        <span className="workspace-change__name">{basename(change.path)}</span>
                        {dir && <span className="workspace-change__path">{dir}</span>}
                        {change.detail && <span className="workspace-change__detail">{change.detail}</span>}
                      </span>
                      <span className="workspace-change__meta">
                        <span className="workspace-change__badge workspace-change__badge--git">{change.meta}</span>
                        {change.time && <span className="workspace-change__badge">{change.time}</span>}
                      </span>
                    </button>
                  );
                })}
              </div>
            </div>
          ) : viewMode === "changed" && !selectedPath ? (
            <div className="workspace-git-history">
              {workspaceGitWarning && (
                <div className="workspace-note workspace-note--warning" role="status">
                  {workspaceGitWarning}
                </div>
              )}
              {creationMode ? (
                <>
                  {sessionChanges.length > 0 && renderChangeScope(t("context.sessionChanges"), sessionChanges)}
                  {gitWorkingChanges.length > 0 && renderChangeScope(t("workspace.workingChanges"), gitWorkingChanges)}
                  {!loadingHistory && !hasFileChanges && !workspaceGitWarning && (
                    <div className="workspace-empty">{t("context.noChanges")}</div>
                  )}
                  {loadingHistory ? (
                    <div className="workspace-empty">{t("workspace.loading")}</div>
                  ) : gitHistory.length > 0 ? (
                    <section className={`workspace-commit-history${commitHistoryOpen ? " workspace-commit-history--open" : ""}`}>
                      <button
                        className="workspace-commit-history__toggle"
                        type="button"
                        aria-expanded={commitHistoryOpen}
                        onClick={() => {
                          setCommitHistoryOpen((open) => !open);
                          setExpandedCommit(null);
                        }}
                      >
                        {commitHistoryOpen ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                        <span>{t("workspace.commitHistory")}</span>
                        <small>{t("workspace.commitHistoryMeta", { count: gitHistory.length })}</small>
                      </button>
                      {commitHistoryOpen && (
                        <div className="workspace-git-history__list">
                          {gitHistory.map((commit) => (
                            <div key={commit.hash} className={`workspace-git-history__item${expandedCommit === commit.hash ? " workspace-git-history__item--expanded" : ""}`}>
                              <button
                                className="workspace-git-history__head"
                                onClick={() => void toggleCommit(commit.hash)}
                              >
                                <div className="workspace-git-history__head-top">
                                  {expandedCommit === commit.hash ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                                  <span className="workspace-git-history__message">{commit.message}</span>
                                </div>
                                <div className="workspace-git-history__head-bottom">
                                  <span className="workspace-git-history__author">{commit.author}</span>
                                  <span className="workspace-git-history__date">
                                    {formatCommitDate(commit.date)} <span className="workspace-git-history__hash">{commit.hash.substring(0, 7)}</span>
                                  </span>
                                </div>
                              </button>
                              {expandedCommit === commit.hash && (
                                <div className="workspace-git-history__detail">
                                  {loadingCommit ? (
                                    <div className="workspace-empty">{t("workspace.loading")}</div>
                                  ) : commitDetail?.diff ? (
                                    <CodeViewer value={cleanGitDiff(commitDetail.diff)} language="diff" />
                                  ) : commitDetail?.files ? (
                                    <div className="workspace-git-history__files">
                                      {commitDetail.files.map((file) => (
                                        <button
                                          key={file}
                                          className="workspace-git-history__file"
                                          onClick={() => selectFile(file)}
                                        >
                                          <FileText size={14} /> {file}
                                        </button>
                                      ))}
                                    </div>
                                  ) : (
                                    <div className="workspace-empty">No details available</div>
                                  )}
                                </div>
                              )}
                            </div>
                          ))}
                        </div>
                      )}
                    </section>
                  ) : null}
                </>
              ) : (
                <>
                  {sessionChanges.length > 0 && renderChangeScope(t("workspace.changedTab"), sessionChanges)}
                  {loadingHistory ? (
                    <div className="workspace-empty">{t("workspace.loading")}</div>
                  ) : gitHistory.length === 0 && sessionChanges.length === 0 ? (
                    <div className="workspace-empty">{workspaceGitWarning ? t("workspace.gitChangesUnknown") : t("workspace.noChanges")}</div>
                  ) : (
                    <div className="workspace-git-history__list">
                      {gitHistory.map((commit) => (
                        <div key={commit.hash} className={`workspace-git-history__item${expandedCommit === commit.hash ? " workspace-git-history__item--expanded" : ""}`}>
                          <button
                            className="workspace-git-history__head"
                            onClick={() => void toggleCommit(commit.hash)}
                          >
                            <div className="workspace-git-history__head-top">
                              {expandedCommit === commit.hash ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                              <span className="workspace-git-history__message">{commit.message}</span>
                            </div>
                            <div className="workspace-git-history__head-bottom">
                              <span className="workspace-git-history__author">{commit.author}</span>
                              <span className="workspace-git-history__date">
                                {formatCommitDate(commit.date)} <span className="workspace-git-history__hash">{commit.hash.substring(0, 7)}</span>
                              </span>
                            </div>
                          </button>
                          {expandedCommit === commit.hash && (
                            <div className="workspace-git-history__detail">
                              {loadingCommit ? (
                                <div className="workspace-empty">{t("workspace.loading")}</div>
                              ) : commitDetail?.diff ? (
                                <CodeViewer value={cleanGitDiff(commitDetail.diff)} language="diff" />
                              ) : commitDetail?.files ? (
                                <div className="workspace-git-history__files">
                                  {commitDetail.files.map((file) => (
                                    <button
                                      key={file}
                                      className="workspace-git-history__file"
                                      onClick={() => selectFile(file)}
                                    >
                                      <FileText size={14} /> {file}
                                    </button>
                                  ))}
                                </div>
                              ) : (
                                <div className="workspace-empty">No details available</div>
                              )}
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  )}
                </>
              )}
            </div>
          ) : viewMode === "changed" && selectedPath ? (
            <div className="workspace-git-history">
              {loadingHistory ? (
                <div className="workspace-empty">{t("workspace.loading")}</div>
              ) : gitHistory.length === 0 ? (
                <div className="workspace-empty">{t("workspace.noChanges")}</div>
              ) : (
                <div className="workspace-git-history__list">
                  {gitHistory.map((commit) => (
                    <div key={commit.hash} className={`workspace-git-history__item${expandedCommit === commit.hash ? " workspace-git-history__item--expanded" : ""}`}>
                      <button
                        className="workspace-git-history__head"
                        onClick={() => void toggleCommit(commit.hash)}
                      >
                        <div className="workspace-git-history__head-top">
                          {expandedCommit === commit.hash ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                          <span className="workspace-git-history__message">{commit.message}</span>
                        </div>
                        <div className="workspace-git-history__head-bottom">
                          <span className="workspace-git-history__author">{commit.author}</span>
                          <span className="workspace-git-history__date">
                            {formatCommitDate(commit.date)} <span className="workspace-git-history__hash">{commit.hash.substring(0, 7)}</span>
                          </span>
                        </div>
                      </button>
                      {expandedCommit === commit.hash && (
                        <div className="workspace-git-history__detail">
                          {loadingCommit ? (
                            <div className="workspace-empty">{t("workspace.loading")}</div>
                          ) : commitDetail?.diff ? (
                            <CodeViewer value={cleanGitDiff(commitDetail.diff)} language="diff" />
                          ) : (
                            <div className="workspace-empty">No details available</div>
                          )}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          ) : !selectedPath ? (
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
            <FloatingMenu x={selectionMenu.x} y={selectionMenu.y} estimatedHeight={WORKSPACE_CONTEXT_MENU_SELECTION_HEIGHT}>
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

      {showTreeRail && (
        <section className="workspace-tree-rail" aria-label={actualTreeVisible ? t("workspace.hideTree") : t("workspace.showTree")}>
          <Tooltip label={actualTreeVisible ? t("workspace.hideTree") : t("workspace.showTree")} side="right">
            <button
              className={`workspace-tree-reveal workspace-iconbtn${actualTreeVisible ? " workspace-iconbtn--on" : ""}`}
              type="button"
              aria-label={actualTreeVisible ? t("workspace.hideTree") : t("workspace.showTree")}
              aria-pressed={actualTreeVisible}
              onClick={toggleTreeRail}
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
          aria-valuemax={maxTreeWidthForPanel}
          aria-valuenow={effectiveTreeWidth}
          onPointerDown={startTreeResize}
          onKeyDown={resizeTreeWithKeyboard}
          onDoubleClick={() => setSavedTreeWidth(WORKSPACE_TREE_DEFAULT_WIDTH)}
        />
      )}

      <section className="workspace-files">
        {showFileTools && (
          <div className={`workspace-files__tools${embeddedDockMode ? " workspace-files__tools--embedded" : ""}`}>
            {showViewTabs && (
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
            )}
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
                    void loadGitHistory();
                  }}
                >
                  <GitBranch size={13} />
                  {t("workspace.changedTab")}
                </button>
              </div>
            )}
            {showViewTabs && (
              <Tooltip label={t("workspace.refreshChanges")}>
                <button className="workspace-iconbtn" onClick={() => { void loadGitHistory(); void loadWorkspaceChanges(); }}>
                  <RefreshCw size={14} />
                </button>
              </Tooltip>
            )}
          </div>
        )}

        <div className="workspace-search">
          <Search size={14} />
          <input ref={filterRef} value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={searchPlaceholder} />
        </div>
        {scopedFilePaths && (
          <div className="workspace-files__scope">
            <span className="workspace-files__scope-title">{t("context.referencedFiles")}</span>
            <span className="workspace-files__scope-meta">{t("context.readMeta", { count: scopedFilePaths.length })}</span>
            <Tooltip label={t("workspace.clearFileScope")}>
              <button
                type="button"
                aria-label={t("workspace.clearFileScope")}
                onClick={() => {
                  dismissedFileListRequestIdRef.current = lastFileListRequestIdRef.current;
                  setScopedFilePaths(null);
                  setFilter("");
                }}
              >
                <X size={12} />
              </button>
            </Tooltip>
          </div>
        )}
        <div
          className="workspace-tree"
          ref={treeRef}
          onContextMenu={openTreeBlankMenu}
          style={{
            height: "100%",
            overflow: "auto",
            position: "relative",
          }}
        >
          {treeRows.length > 0 ? (
            <div
              className="workspace-tree__sizer"
              style={{
                height: virtualizer.getTotalSize(),
                width: "100%",
                position: "relative",
              }}
            >
              {virtualizer.getVirtualItems().map((row) => {
                const item = treeRows[row.index];
                if (!item) return null;
                return (
                  <div
                    key={item.key}
                    data-index={row.index}
                    ref={virtualizer.measureElement}
                    style={{
                      position: "absolute",
                      top: 0,
                      left: 0,
                      width: "100%",
                      transform: `translateY(${row.start}px)`,
                    }}
                  >
                    {item.isSearch ? renderSearchRow(item) : renderNormalRow(item)}
                  </div>
                );
              })}
            </div>
          ) : null}
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
              {
                icon: <FolderOpen size={14} />,
                label: t("workspace.revealInFileManager"),
                onSelect: revealInFileManager,
              },
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
