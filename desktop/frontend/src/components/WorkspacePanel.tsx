import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type {
  CSSProperties,
  DragEvent as ReactDragEvent,
  JSX,
  KeyboardEvent,
  MouseEvent as ReactMouseEvent,
  PointerEvent as ReactPointerEvent,
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
  Check,
  Maximize2,
  MessageSquarePlus,
  Minimize2,
  Plus,
  RefreshCw,
  Search,
  Upload,
  X,
  Minus,
} from "lucide-react";
import { app } from "../lib/bridge";
import { useT, type DictKey } from "../lib/i18n";
import { useToast } from "../lib/toast";
import { loadLayoutSize, saveLayoutSize } from "../lib/layoutPreferences";
import type { DirEntry, FilePreview, WorkspaceChangesView, WorkspaceChangeView, WorkspaceGitDiffView } from "../lib/types";
import { formatWorkspaceReference, WORKSPACE_REF_DRAG_TYPE } from "../lib/workspaceDrag";
import { cleanGitDiff } from "../lib/diff";
import { CodeViewer } from "./CodeViewer";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";
import { FloatingMenu, FloatingMenuItems } from "./FloatingMenu";
import { Markdown } from "./Markdown";
import { Tooltip } from "./Tooltip";
import { AnchoredPopover } from "./AnchoredPopover";

const WORKSPACE_TREE_MIN_WIDTH = 300;
const WORKSPACE_TREE_DEFAULT_WIDTH = 300;
const WORKSPACE_TREE_MAX_WIDTH = 340;
const WORKSPACE_PREVIEW_MIN_WIDTH = 360;
const WORKSPACE_PREVIEW_TARGET_WIDTH = 360;
const WORKSPACE_DUAL_PANEL_MIN_WIDTH = WORKSPACE_TREE_MIN_WIDTH + WORKSPACE_PREVIEW_MIN_WIDTH;
const WORKSPACE_DUAL_PANEL_TARGET_WIDTH = WORKSPACE_TREE_DEFAULT_WIDTH + WORKSPACE_PREVIEW_TARGET_WIDTH;
const WORKSPACE_CONTEXT_MENU_FILE_HEIGHT = 136;
const WORKSPACE_CONTEXT_MENU_REF_HEIGHT = 92;
const WORKSPACE_CONTEXT_MENU_SELECTION_HEIGHT = 48;
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

type SelectedChangeKind = "staged" | "unstaged";

function normalizedGitStatus(value: string | undefined): string | undefined {
  if (value === undefined) return undefined;
  return value.trim();
}

function isStagedGitChange(change: WorkspaceChangeView): boolean {
  if (!change.sources.includes("git")) return false;
  const index = normalizedGitStatus(change.gitIndexStatus);
  if (index !== undefined) return index !== "" && index !== "?";
  return false;
}

function isUnstagedGitChange(change: WorkspaceChangeView): boolean {
  if (!change.sources.includes("git")) return false;
  if (change.gitStatus === "??") return true;
  const worktree = normalizedGitStatus(change.gitWorktreeStatus);
  if (worktree !== undefined) return worktree !== "";
  return false;
}

function unstagedWorkspaceChanges(files: WorkspaceChangeView[] | undefined): WorkspaceChangeView[] {
  return (files ?? []).filter(isUnstagedGitChange);
}

function stagedWorkspaceChanges(files: WorkspaceChangeView[] | undefined): WorkspaceChangeView[] {
  return (files ?? []).filter(isStagedGitChange);
}

function statusForChange(change: WorkspaceChangeView, kind: SelectedChangeKind = "unstaged"): string {
  if (kind === "staged") {
    const index = normalizedGitStatus(change.gitIndexStatus) || normalizedGitStatus(change.gitStatus) || "";
    return index === "?" || change.gitStatus === "??" ? "A" : index;
  }
  if (change.gitStatus === "??" || change.gitWorktreeStatus === "?") return "A";
  return normalizedGitStatus(change.gitWorktreeStatus) || normalizedGitStatus(change.gitStatus) || "";
}

function renderMediaPreview(preview: FilePreview): JSX.Element | null {
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
  const { showToast } = useToast();
  const panelRef = useRef<HTMLElement>(null);
  const filterRef = useRef<HTMLInputElement>(null);
  const previewBodyRef = useRef<HTMLDivElement>(null);
  const commitBranchAnchorRef = useRef<HTMLButtonElement>(null);
  const commitBranchFilterRef = useRef<HTMLInputElement>(null);
  const [entriesByDir, setEntriesByDir] = useState<Record<string, DirEntry[]>>({});
  const [openDirs, setOpenDirs] = useState<Set<string>>(() => new Set([""]));
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [openTabs, setOpenTabs] = useState<string[]>([]);
  const [preview, setPreview] = useState<FilePreview | null>(null);
  const [loadingPreview, setLoadingPreview] = useState(false);
  const [viewMode, setViewMode] = useState<"files" | "changed">(initialViewMode);
  const [workspaceChanges, setWorkspaceChanges] = useState<WorkspaceChangesView | null>(null);
  const [loadingChanges, setLoadingChanges] = useState(false);
  const [changeDiff, setChangeDiff] = useState<WorkspaceGitDiffView | null>(null);
  const [selectedChangeKind, setSelectedChangeKind] = useState<SelectedChangeKind>("unstaged");
  const [loadingChangeDiff, setLoadingChangeDiff] = useState(false);
  const [gitBusy, setGitBusy] = useState(false);
  const [commitOpen, setCommitOpen] = useState(false);
  const [commitMessage, setCommitMessage] = useState("");
  const [commitBranch, setCommitBranch] = useState("");
  const [commitBranches, setCommitBranches] = useState<string[]>([]);
  const [commitBranchOpen, setCommitBranchOpen] = useState(false);
  const [commitBranchFilter, setCommitBranchFilter] = useState("");
  const [selectionMenu, setSelectionMenu] = useState<{ x: number; y: number; text: string; path: string } | null>(null);
  const [treeMenu, setTreeMenu] = useState<{ x: number; y: number; path: string; isDir: boolean } | null>(null);
  const [treeBlankMenuPoint, setTreeBlankMenuPoint] = useState<ContextMenuPoint | null>(null);
  const [filter, setFilter] = useState("");
  const [treeVisible, setTreeVisible] = useState(true);
  const [treeWidth, setTreeWidth] = useState(loadWorkspaceTreeWidth);
  const [treeResizing, setTreeResizing] = useState(false);
  const [recentOpen, setRecentOpen] = useState(false);
  const lastPreviewModeActiveRef = useRef<boolean | null>(null);
  const recentAnchorRef = useRef<HTMLButtonElement>(null);
  const openDirsRef = useRef(openDirs);

  useEffect(() => {
    openDirsRef.current = openDirs;
  }, [openDirs]);

  const loadDir = useCallback(async (dir: string) => {
    const entries = await app.ListDir(dir).catch(() => []);
    setEntriesByDir((prev) => ({ ...prev, [dir]: entries ?? [] }));
  }, []);

  const loadWorkspaceChanges = useCallback(async () => {
    setLoadingChanges(true);
    try {
      const result = await app.WorkspaceChanges();
      setWorkspaceChanges(result ?? { files: [], gitAvailable: false });
      const files = unstagedWorkspaceChanges(result?.files);
      setOpenDirs((prev) => {
        const next = new Set(prev);
        for (const file of files) {
          for (const dir of parentDirs(file.path)) next.add(dir);
        }
        return next;
      });
    } catch (err) {
      setWorkspaceChanges({ files: [], gitAvailable: false, gitErr: err instanceof Error ? err.message : String(err) });
    } finally {
      setLoadingChanges(false);
    }
  }, []);

  const loadSelectedChangeDiff = useCallback(async () => {
    if (!selectedPath) return;
    setLoadingChangeDiff(true);
    try {
      const result = await app.WorkspaceGitDiff(selectedPath, selectedChangeKind === "staged");
      setChangeDiff(result);
    } catch (err) {
      setChangeDiff({
        path: selectedPath,
        diff: "",
        err: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setLoadingChangeDiff(false);
    }
  }, [selectedChangeKind, selectedPath]);

  const clearSelectedChange = useCallback(() => {
    setSelectedPath(null);
    setPreview(null);
    setChangeDiff(null);
    setRecentOpen(false);
  }, []);

  const runGitAction = useCallback(
    async (action: () => Promise<void>, successKey: DictKey, clearSelection = false, nextKind?: SelectedChangeKind) => {
      setGitBusy(true);
      try {
        await action();
        showToast(t(successKey));
        if (clearSelection) clearSelectedChange();
        return true;
      } catch (err) {
        showToast(err instanceof Error ? err.message : String(err), "error");
        return false;
      } finally {
        await loadWorkspaceChanges();
        if (!clearSelection && selectedPath) {
          const kind = nextKind ?? selectedChangeKind;
          setSelectedChangeKind(kind);
          const result = await app.WorkspaceGitDiff(selectedPath, kind === "staged");
          setChangeDiff(result);
        }
        setGitBusy(false);
      }
    },
    [clearSelectedChange, loadWorkspaceChanges, selectedChangeKind, selectedPath, showToast, t],
  );

  const stageChange = useCallback(
    (path: string) => {
      void runGitAction(() => app.WorkspaceGitStage(path), "workspace.gitStaged", false, selectedPath === path ? "staged" : undefined);
    },
    [runGitAction, selectedPath],
  );

  const unstageChange = useCallback(
    (path: string) => {
      void runGitAction(() => app.WorkspaceGitUnstage(path), "workspace.gitUnstaged", false, selectedPath === path ? "unstaged" : undefined);
    },
    [runGitAction, selectedPath],
  );

  const stageAllChanges = useCallback(() => {
    void runGitAction(() => app.WorkspaceGitStageAll(), "workspace.gitStagedAll", false, selectedPath ? "staged" : undefined);
  }, [runGitAction, selectedPath]);

  const unstageAllChanges = useCallback(() => {
    void runGitAction(() => app.WorkspaceGitUnstageAll(), "workspace.gitUnstagedAll", false, selectedPath ? "unstaged" : undefined);
  }, [runGitAction, selectedPath]);

  const commitChanges = useCallback(
    async (push: boolean) => {
      const message = commitMessage.trim();
      if (!message) return;
      const success = await runGitAction(
        () => app.WorkspaceGitCommit(message, push, commitBranch),
        push ? "workspace.gitCommittedPushed" : "workspace.gitCommitted",
        true,
      );
      if (success) {
        setCommitOpen(false);
        setCommitBranchOpen(false);
        setCommitBranchFilter("");
        setCommitMessage("");
      }
    },
    [commitBranch, commitMessage, runGitAction],
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

  const selectChangeFile = useCallback(
    (path: string, kind: SelectedChangeKind) => {
      setSelectedChangeKind(kind);
      selectFile(path);
    },
    [selectFile],
  );

  useEffect(() => {
    if (!open) return;
    setEntriesByDir({});
    setOpenDirs(new Set([""]));
    setSelectedPath(null);
    setSelectedChangeKind("unstaged");
    setOpenTabs([]);
    setPreview(null);
    setWorkspaceChanges(null);
    setChangeDiff(null);
    setCommitOpen(false);
    setCommitMessage("");
    setCommitBranch("");
    setCommitBranches([]);
    setCommitBranchOpen(false);
    setCommitBranchFilter("");
    setSelectionMenu(null);
    setTreeMenu(null);
    setFilter("");
    setTreeVisible(true);
    void loadDir("");
  }, [cwd, loadDir, open]);

  useEffect(() => {
    if (!open) return;
    setViewMode(initialViewMode);
    setSelectionMenu(null);
    setTreeMenu(null);
    setRecentOpen(false);
    if (initialViewMode === "changed") {
      setSelectedPath(null);
      setSelectedChangeKind("unstaged");
      setPreview(null);
      setChangeDiff(null);
      setRecentOpen(false);
      setFilter("");
      void loadWorkspaceChanges();
    }
  }, [initialViewMode, loadWorkspaceChanges, open]);

  useEffect(() => {
    if (!open) return;
    if (viewMode === "changed") {
      void loadWorkspaceChanges();
    }
  }, [viewMode, loadWorkspaceChanges, open]);

  useEffect(() => {
    if (!open || !refreshKey) return;
    if (viewMode === "changed") {
      void loadWorkspaceChanges();
    }
    openDirsRef.current.forEach((dir) => void loadDir(dir));
  }, [loadWorkspaceChanges, loadDir, open, refreshKey, viewMode]);

  useEffect(() => {
    if (!commitOpen) return;
    let live = true;
    app
      .GitBranches()
      .then((branches) => {
        if (!live) return;
        const next = branches ?? [];
        setCommitBranches(next);
        setCommitBranch((current) => {
          if (current && next.includes(current)) return current;
          const active = workspaceChanges?.gitBranch && !workspaceChanges.gitBranch.startsWith("@") ? workspaceChanges.gitBranch : "";
          return (active && next.includes(active) ? active : next[0]) ?? "";
        });
      })
      .catch(() => {
        if (live) setCommitBranches([]);
      });
    return () => {
      live = false;
    };
  }, [commitOpen, workspaceChanges?.gitBranch]);

  useEffect(() => {
    if (!commitBranchOpen) return;
    commitBranchFilterRef.current?.focus();
  }, [commitBranchOpen]);

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
      void loadWorkspaceChanges();
      return;
    }
    const dirs = Array.from(openDirsRef.current);
    setEntriesByDir({});
    dirs.forEach((dir) => void loadDir(dir));
  }, [loadWorkspaceChanges, loadDir, viewMode]);

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
    if (!open || !selectedPath || viewMode === "changed") return;
    return refreshSelected();
  }, [open, refreshSelected, selectedPath, viewMode]);

  useEffect(() => {
    if (!open || viewMode !== "changed" || !selectedPath) return;
    void loadSelectedChangeDiff();
  }, [loadSelectedChangeDiff, open, selectedPath, viewMode]);

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
          setChangeDiff(null);
          setSelectedChangeKind("unstaged");
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
  const changedMode = viewMode === "changed";
  const currentFileName = selectedPath ? basename(selectedPath) : t("workspace.noFile");
  const currentFileDir = selectedPath ? parentPath(selectedPath) : "";
  const previewTitle = changedMode && !selectedPath ? t("workspace.changedTab") : currentFileName;
  const previewSubtitle = changedMode && !selectedPath ? shortCwd(cwd) || t("workspace.title") : currentFileDir;
  const recentFiles = useMemo(() => [...openTabs].reverse(), [openTabs]);
  const allChangedFiles = useMemo(() => workspaceChanges?.files ?? [], [workspaceChanges]);
  const stagedFiles = useMemo(() => stagedWorkspaceChanges(allChangedFiles), [allChangedFiles]);
  const unstagedFiles = useMemo(() => unstagedWorkspaceChanges(allChangedFiles), [allChangedFiles]);

  useEffect(() => {
    if (commitOpen && !gitBusy && stagedFiles.length === 0) {
      setCommitOpen(false);
      setCommitBranchOpen(false);
      setCommitBranchFilter("");
      setCommitMessage("");
    }
  }, [commitOpen, gitBusy, stagedFiles.length]);

  const filteredStagedFiles = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return stagedFiles;
    return stagedFiles.filter((change) =>
      `${change.path} ${change.oldPath ?? ""} ${statusForChange(change, "staged")}`.toLowerCase().includes(q),
    );
  }, [filter, stagedFiles]);
  const filteredUnstagedFiles = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return unstagedFiles;
    return unstagedFiles.filter((change) =>
      `${change.path} ${change.oldPath ?? ""} ${statusForChange(change, "unstaged")}`.toLowerCase().includes(q),
    );
  }, [filter, unstagedFiles]);
  const filteredCommitBranches = useMemo(() => {
    const q = commitBranchFilter.trim().toLowerCase();
    if (!q) return commitBranches;
    return commitBranches.filter((branch) => branch.toLowerCase().includes(q));
  }, [commitBranchFilter, commitBranches]);
  const newCommitBranch = commitBranchFilter.trim();
  const canCreateCommitBranch = newCommitBranch !== "" && !commitBranches.includes(newCommitBranch);
  const commitBranchLabel = commitBranch.trim() || workspaceChanges?.gitBranch || t("workspace.noBranch");
  const selectedChangeDiff = changeDiff?.diff ? cleanGitDiff(changeDiff.diff) : "";
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

  const searchPlaceholder = t(viewMode === "changed" ? "workspace.filterChanges" : "workspace.filter");

  const effectiveTreeWidth = useMemo(() => clampWorkspaceTreeWidth(treeWidth, panelWidth), [panelWidth, treeWidth]);
  const previewVisible = viewMode === "changed" ? selectedPath !== null : openTabs.length > 0 || selectedPath !== null;
  const selectedFileVisible = selectedPath !== null;
  const compactTreeSplit =
    treeVisible && selectedFileVisible && panelWidth !== undefined && panelWidth < WORKSPACE_DUAL_PANEL_MIN_WIDTH;
  const actualTreeVisible = treeVisible;
  const showTreeRail = previewVisible && !actualTreeVisible;
  const previewModeActive = open && previewVisible;
  const embeddedDockMode = !showViewTabs;
  const showFilesTools = changedMode || showViewTabs;

  const panelStyle = useMemo(
    () =>
      ({
        "--workspace-tree-width": `${effectiveTreeWidth}px`,
        "--workspace-preview-min-width": compactTreeSplit ? "0px" : `${WORKSPACE_PREVIEW_MIN_WIDTH}px`,
      }) as CSSProperties,
    [compactTreeSplit, effectiveTreeWidth],
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

  const revealInFileManager = () => {
    if (!treeMenu) return;
    setTreeMenu(null);
    void app.RevealWorkspacePath(treeMenu.path);
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

  const statusToneClass = (status: string): string => {
    if (status === "M") return " workspace-tree__status--modified";
    if (status === "D") return " workspace-tree__status--deleted";
    return "";
  };

  const renderChangeFileRows = (changes: WorkspaceChangeView[], kind: SelectedChangeKind): JSX.Element[] =>
    changes.map((change) => {
      const active = selectedPath === change.path && selectedChangeKind === kind;
      const status = statusForChange(change, kind);
      const actionLabel = t(kind === "staged" ? "workspace.unstageFile" : "workspace.stageFile");
      return (
        <div key={`${kind}-${change.path}`} className={`workspace-change-row${active ? " workspace-change-row--active" : ""}`}>
          <button
            type="button"
            className="workspace-change-row__main"
            draggable
            onDragStart={(event) => startTreeDrag(event, change.path, false)}
            onClick={() => {
              if (active) {
                setSelectedPath(null);
              } else {
                selectChangeFile(change.path, kind);
              }
            }}
            onContextMenu={(event) => openTreeMenu(event, change.path, false)}
          >
            <FileText size={14} className="workspace-tree__icon" />
            <span className="workspace-tree__result">
              <span className="workspace-tree__result-name">{basename(change.path)}</span>
              {parentPath(change.path) && <span className="workspace-tree__result-dir">{parentPath(change.path)}</span>}
            </span>
          </button>
          <div className="workspace-change-row__ops">
            {status && <span className={`workspace-tree__status${statusToneClass(status)}`}>{status}</span>}
            <Tooltip label={actionLabel}>
              <button
                type="button"
                className="workspace-change-row__action"
                aria-label={`${actionLabel} ${change.path}`}
                disabled={gitBusy}
                onClick={() => {
                  if (kind === "staged") {
                    unstageChange(change.path);
                  } else {
                    stageChange(change.path);
                  }
                }}
              >
                {kind === "staged" ? <Minus size={13} /> : <Plus size={13} />}
              </button>
            </Tooltip>
          </div>
        </div>
      );
    });

  const isMarkdown = selectedPath?.toLowerCase().endsWith(".md") ?? false;
  const treeBlankMenuItems: ContextMenuItem[] = [
    {
      key: "refresh-tree",
      icon: <RefreshCw size={13} />,
      label: t(viewMode === "changed" ? "workspace.refreshChanges" : "workspace.refreshTree"),
      onSelect: refreshWorkspaceList,
    },
  ];

  const closeCommitModal = () => {
    setCommitOpen(false);
    setCommitBranchOpen(false);
    setCommitBranchFilter("");
  };

  const pickCommitBranch = (branch: string) => {
    setCommitBranch(branch.trim());
    setCommitBranchOpen(false);
    setCommitBranchFilter("");
  };

  const chooseFirstCommitBranchMatch = () => {
    if (canCreateCommitBranch) {
      pickCommitBranch(newCommitBranch);
      return;
    }
    const first = filteredCommitBranches[0];
    if (first) pickCommitBranch(first);
  };

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
          {viewMode === "changed" && !selectedPath ? (
            <div className="workspace-empty">{t("workspace.pickChangedFile")}</div>
          ) : viewMode === "changed" && loadingChangeDiff ? (
            <div className="workspace-empty">{t("workspace.loadingChanges")}</div>
          ) : viewMode === "changed" && changeDiff?.err ? (
            <div className="workspace-empty workspace-empty--error">{changeDiff.err}</div>
          ) : viewMode === "changed" && selectedPath ? (
            selectedChangeDiff ? (
              <CodeViewer value={selectedChangeDiff} language="diff" />
            ) : (
              <div className="workspace-empty">{t("workspace.noChanges")}</div>
            )
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
        {showFilesTools && (
          <div className={`workspace-files__tools${embeddedDockMode ? " workspace-files__tools--embedded" : ""}`}>
            {changedMode && (
              <div className="workspace-files__tool-group">
                {previewVisible && (
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
                <Tooltip label={t("workspace.refreshChanges")}>
                  <button className="workspace-iconbtn" type="button" onClick={refreshWorkspaceList}>
                    <RefreshCw size={14} />
                  </button>
                </Tooltip>
                <Tooltip label={t("workspace.commit")}>
                  <button
                    className="workspace-iconbtn"
                    type="button"
                    aria-label={t("workspace.commit")}
                    disabled={gitBusy || stagedFiles.length === 0}
                    onClick={() => setCommitOpen(true)}
                  >
                    <GitBranch size={14} />
                  </button>
                </Tooltip>
              </div>
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
                    setSelectedPath(null);
                    setSelectedChangeKind("unstaged");
                    setPreview(null);
                    setChangeDiff(null);
                    setRecentOpen(false);
                    setFilter("");
                    void loadWorkspaceChanges();
                  }}
                >
                  <GitBranch size={13} />
                  {t("workspace.changedTab")}
                </button>
              </div>
            )}
          </div>
        )}

        <div className="workspace-search">
          <Search size={14} />
          <input ref={filterRef} value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={searchPlaceholder} />
        </div>
        <div className="workspace-tree" onContextMenu={openTreeBlankMenu}>
          {viewMode === "changed" ? (
            loadingChanges ? (
              <div className="workspace-empty">{t("workspace.loadingChanges")}</div>
            ) : workspaceChanges?.gitAvailable === false ? (
              <div className="workspace-empty workspace-empty--error">{workspaceChanges.gitErr || t("workspace.gitUnavailable")}</div>
            ) : filteredStagedFiles.length === 0 && filteredUnstagedFiles.length === 0 ? (
              <div className="workspace-empty">{t("workspace.noChanges")}</div>
            ) : (
              <>
                {filteredStagedFiles.length > 0 && (
                  <div className="workspace-change-section">
                    <div className="workspace-change-section__title">
                      <span>{t("workspace.stagedChanges")}</span>
                      <button
                        type="button"
                        className="workspace-change-section__action"
                        disabled={gitBusy}
                        onClick={unstageAllChanges}
                      >
                        {t("workspace.unstageAll")}
                      </button>
                    </div>
                    {renderChangeFileRows(filteredStagedFiles, "staged")}
                  </div>
                )}
                {filteredUnstagedFiles.length > 0 && (
                  <div className="workspace-change-section">
                    <div className="workspace-change-section__title">
                      <span>{t("workspace.unstagedChanges")}</span>
                      <button
                        type="button"
                        className="workspace-change-section__action"
                        disabled={gitBusy}
                        onClick={stageAllChanges}
                      >
                        {t("workspace.stageAll")}
                      </button>
                    </div>
                    {renderChangeFileRows(filteredUnstagedFiles, "unstaged")}
                  </div>
                )}
              </>
            )
          ) : flattened
            ? flattened.map(({ path, entry }) => {
                const dir = parentPath(path);
                return (
                  <button
                    key={path}
                    className={`workspace-tree__row workspace-tree__row--search${selectedPath === path ? " workspace-tree__row--active" : ""}`}
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
      {commitOpen && (
        <div
          className="modal-backdrop workspace-commit-modal-backdrop"
          onClick={(event) => {
            if (event.target === event.currentTarget) closeCommitModal();
          }}
        >
          <div className="modal workspace-commit-modal" role="dialog" aria-modal="true" aria-labelledby="workspace-commit-title">
            <div id="workspace-commit-title" className="modal__title">
              {t("workspace.commitDialogTitle")}
            </div>
            <label className="workspace-commit-modal__field">
              <span>{t("workspace.commitBranch")}</span>
              <button
                ref={commitBranchAnchorRef}
                type="button"
                className={`workspace-commit-modal__branch-button${commitBranchOpen ? " workspace-commit-modal__branch-button--open" : ""}`}
                disabled={gitBusy}
                aria-haspopup="listbox"
                aria-expanded={commitBranchOpen}
                onClick={() => setCommitBranchOpen((value) => !value)}
              >
                <GitBranch size={14} />
                <span>{commitBranchLabel}</span>
                <ChevronDown size={14} />
              </button>
            </label>
            <AnchoredPopover
              open={commitBranchOpen}
              anchorRef={commitBranchAnchorRef}
              onClose={() => {
                setCommitBranchOpen(false);
                setCommitBranchFilter("");
              }}
              className="workspace-commit-branch-menu"
              align="start"
              offset={6}
              placement="bottom"
              style={{ width: commitBranchAnchorRef.current?.getBoundingClientRect().width }}
            >
              <div className="workspace-commit-branch-menu__search">
                <Search size={14} />
                <input
                  ref={commitBranchFilterRef}
                  value={commitBranchFilter}
                  placeholder={t("workspace.branchSearchPlaceholder")}
                  onChange={(event) => setCommitBranchFilter(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter") {
                      event.preventDefault();
                      chooseFirstCommitBranchMatch();
                    } else if (event.key === "Escape") {
                      setCommitBranchOpen(false);
                      setCommitBranchFilter("");
                    }
                    event.stopPropagation();
                  }}
                />
              </div>
              <div className="workspace-commit-branch-menu__list" role="listbox" aria-label={t("workspace.commitBranch")}>
                {canCreateCommitBranch && (
                  <button
                    type="button"
                    className="workspace-commit-branch-menu__item workspace-commit-branch-menu__item--create"
                    role="option"
                    aria-selected={false}
                    onClick={() => pickCommitBranch(newCommitBranch)}
                  >
                    <Plus size={14} />
                    <span>{t("workspace.createBranch", { branch: newCommitBranch })}</span>
                  </button>
                )}
                {filteredCommitBranches.map((branch) => {
                  const active = branch === commitBranch;
                  return (
                    <button
                      key={branch}
                      type="button"
                      className={`workspace-commit-branch-menu__item${active ? " workspace-commit-branch-menu__item--active" : ""}`}
                      role="option"
                      aria-selected={active}
                      onClick={() => pickCommitBranch(branch)}
                    >
                      {active ? <Check size={14} /> : <GitBranch size={14} />}
                      <span>{branch}</span>
                    </button>
                  );
                })}
                {!canCreateCommitBranch && filteredCommitBranches.length === 0 && (
                  <div className="workspace-commit-branch-menu__empty">{t("workspace.noBranches")}</div>
                )}
              </div>
            </AnchoredPopover>
            <textarea
              className="workspace-commit-modal__input"
              value={commitMessage}
              rows={4}
              placeholder={t("workspace.commitMessagePlaceholder")}
              onChange={(event) => setCommitMessage(event.target.value)}
              onKeyDown={(event) => {
                if ((event.metaKey || event.ctrlKey) && event.key === "Enter") commitChanges(false);
                event.stopPropagation();
              }}
              autoFocus
            />
            <div className="modal__actions">
              <button type="button" className="btn" disabled={gitBusy} onClick={closeCommitModal}>
                {t("common.cancel")}
              </button>
              <button
                type="button"
                className="btn"
                disabled={gitBusy || commitMessage.trim() === "" || stagedFiles.length === 0}
                onClick={() => commitChanges(false)}
              >
                <GitBranch size={14} />
                {t("workspace.commit")}
              </button>
              <button
                type="button"
                className="btn btn--primary workspace-git-primary"
                disabled={gitBusy || commitMessage.trim() === "" || stagedFiles.length === 0}
                onClick={() => commitChanges(true)}
              >
                <Upload size={14} />
                {t("workspace.commitAndPush")}
              </button>
            </div>
          </div>
        </div>
      )}
    </aside>
  );
}
