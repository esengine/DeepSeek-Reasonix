import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type {
  CSSProperties,
  ClipboardEvent as ReactClipboardEvent,
  DragEvent as ReactDragEvent,
  KeyboardEvent,
  MouseEvent as ReactMouseEvent,
  PointerEvent as ReactPointerEvent,
} from "react";
import {
  ChevronDown,
  ChevronRight,
  FileText,
  FolderTree,
  FolderX,
  GitBranch,
  History,
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
import { useWorkspaceRefresh } from "../lib/workspaceRefreshStore";
import { useWorkspaceRefreshInvalidation, workspaceRefreshFallbackSequence } from "../lib/workspaceRefreshInvalidation";
import { createWorkspaceRefreshScheduler } from "../lib/workspaceRefreshScheduler";
import {
  beginKeyedResourceRequest,
  emptyKeyedResource,
  rejectKeyedResourceRequest,
  resolveKeyedResourceRequest,
} from "../lib/keyedResource";
import { shouldScrollWorkspaceTreeSelection } from "../lib/workspaceTreeReveal";
import { mergeWorkspaceSearchResults } from "../lib/workspaceTreeSearch";
import { useWorkspaceTreeScrollPersistence } from "../lib/useWorkspaceTreeScrollPersistence";
import {
  readWorkspaceTreeMemory,
  rememberWorkspaceTreeOpenDirs,
  rememberWorkspaceTreeState,
  touchWorkspaceTreeVisit,
  workspaceTreeVisitId,
} from "../lib/workspaceTreeMemory";
import { loadOptionalLayoutSize } from "../lib/layoutPreferences";
import type {
  DirEntry,
  FilePreview,
  GitCommitView,
  GitCommitDetailView,
  RewindResultView,
  WorkspaceChangeDetailView,
  WireCompletionSummary,
} from "../lib/types";
import { workspaceGitStatusLabel } from "../lib/workspaceChanges";
import { formatWorkspaceReference, WORKSPACE_REF_DRAG_TYPE } from "../lib/workspaceDrag";
import { formatSelectionReference, languageFor } from "../lib/selectedTextContext";
import { cleanGitDiff } from "../lib/diff";
import { CodeViewer } from "./CodeViewer";
import { DiffView } from "./DiffView";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";
import { FloatingMenu, FloatingMenuItems } from "./FloatingMenu";
import { Markdown } from "./Markdown";
import { Tooltip } from "./Tooltip";
import { AnchoredPopover } from "./AnchoredPopover";
import { MarkdownImageTabContext } from "./MarkdownImageContext";
import { WorkspaceMediaPreview } from "./WorkspaceMediaPreview";
import { buildWorkspacePathBreadcrumbs, WorkspacePathBreadcrumbs } from "./WorkspacePathBreadcrumbs";
import { WorkspaceTreeRow, type WorkspaceTreeRowData } from "./WorkspaceTreeRow";
import { WorkspaceTreeMenu } from "./WorkspaceTreeMenu";
import { WORKSPACE_TURN_VERIFICATION_ID, WorkspaceTurnVerification } from "./WorkspaceTurnVerification";
import { useWorkspaceChangesResource } from "../lib/useWorkspaceChangesResource";
import {
  workspaceBasename as basename, workspaceEntryPath as entryPath,
  workspaceFormatCommitDate as formatCommitDate,
  workspaceParentDirs as parentDirs, workspaceParentPath as parentPath,
  workspaceTopLevelDirPath as topLevelDirPath,
} from "../lib/workspacePanelFormat";

const WORKSPACE_TREE_MIN_WIDTH = 140;
const WORKSPACE_TREE_DEFAULT_WIDTH = 200;
// Below this available preview width the empty detail pane is pointless on a
// narrow panel — better to show the file tree alone until a file is picked.
const WORKSPACE_PREVIEW_COMFORT_WIDTH = 300;
// The tree/preview split width is shared across every file tab: one global
// cached width, so switching tabs or opening a new one never changes the
// proportions (openDirs/scrollTop stay per-tab, only the width is global).
const GLOBAL_TREE_WIDTH_KEY = "__global_tree_width__";
const WORKSPACE_PREVIEW_MIN_WIDTH = 140;
const WORKSPACE_PREVIEW_TARGET_WIDTH = 360;
const WORKSPACE_DUAL_PANEL_TARGET_WIDTH = WORKSPACE_TREE_DEFAULT_WIDTH + WORKSPACE_PREVIEW_TARGET_WIDTH;
const WORKSPACE_CONTEXT_MENU_SELECTION_HEIGHT = 48;
const WORKSPACE_MAX_PREVIEW_TABS = 5;

type WorkspaceRevealRequest = { id: number; path: string };
export { WORKSPACE_TURN_VERIFICATION_ID } from "./WorkspaceTurnVerification";
export type WorkspaceVerificationRevealRequest = { id: number; summary: WireCompletionSummary; tabId: string; turnStartAt: number; currentSummary?: WireCompletionSummary };
type WorkspaceFileListRequest = { id: number; paths: string[] };
type WorkspaceChangeListEntry = { key: string; path: string; meta: string; time: string; detail: string };
type WorkspaceChangeListRequest = { id: number; changes: WorkspaceChangeListEntry[] };

function clampWorkspaceTreeWidth(width: number, panelWidth?: number): number {
  return clampWorkspaceSplitTreeWidth({
    width,
    panelWidth,
    treeMinWidth: WORKSPACE_TREE_MIN_WIDTH,
    previewMinWidth: WORKSPACE_PREVIEW_MIN_WIDTH,
  });
}

export function WorkspacePanel({
  open,
  tabId,
  cwd,
  tabReady = false,
  maximized,
  panelWidth,
  onClose,
  onToggleMaximized,
  onPreviewModeChange,
  onAddToChat,
  onAddCodeToChat,
  onOpenInTerminal,
  onRequestPanelWidth,
  onFileTreeRefresh,
  onSessionRevertCommitted,
  initialViewMode = "files",
  revealPathRequest,
  changeRevealRequest,
  verificationRevealRequest,
  fileListRequest,
  changeListRequest,
  showViewTabs = true,
  workspaceScopeKey: workspaceScopeKeyProp,
  workspaceMemoryKey: workspaceMemoryKeyProp,
  workspaceMemoryVisitId: workspaceMemoryVisitIdProp,
  creationMode = false,
  completionSummary,
  turnStartAt = 0,
  qualityFloor,
  onOpenFilesChange,
}: {
  open: boolean;
  tabId?: string;
  cwd?: string;
  /** Whether the backend session for the current tab has finished booting
   * (WorkspaceTab.Ready). When false, ListDirForTab returns an empty list for
   * the tab even though the workspace is not actually empty; the file tree must
   * wait for readiness instead of rendering a blank panel. */
  tabReady?: boolean;
  maximized: boolean;
  panelWidth?: number;
  onClose: () => void;
  onToggleMaximized: () => void;
  onPreviewModeChange?: (active: boolean) => void;
  onAddToChat?: (text: string) => void;
  onAddCodeToChat?: (path: string, code: string) => void;
  onOpenInTerminal?: (path: string) => void;
  onRequestPanelWidth?: (width: number) => void;
  onFileTreeRefresh?: () => void;
  onSessionRevertCommitted?: (tabId: string, result: RewindResultView) => void;
  initialViewMode?: "files" | "changed";
  revealPathRequest?: WorkspaceRevealRequest | null;
  changeRevealRequest?: WorkspaceRevealRequest | null;
  verificationRevealRequest?: WorkspaceVerificationRevealRequest | null;
  fileListRequest?: WorkspaceFileListRequest | null;
  changeListRequest?: WorkspaceChangeListRequest | null;
  showViewTabs?: boolean;
  workspaceScopeKey?: string;
  workspaceMemoryKey?: string;
  workspaceMemoryVisitId?: number;
  creationMode?: boolean;
  completionSummary?: WireCompletionSummary;
  turnStartAt?: number;
  qualityFloor?: "standard" | "delivery";
  /** Reports the current set of open preview files (paths, oldest→newest) and
   *  the active one so the dock can mirror the preview as a single tab. */
  onOpenFilesChange?: (openTabs: string[], activePath: string | null) => void;
}) {
  const t = useT();
  const workspaceTabId = tabId ?? "";
  const activeVerificationRevealRequest = verificationRevealRequest?.tabId === workspaceTabId
    && verificationRevealRequest.turnStartAt === turnStartAt
    && verificationRevealRequest.currentSummary === completionSummary ? verificationRevealRequest : null;
  const visibleCompletionSummary = activeVerificationRevealRequest?.summary ?? completionSummary;
  const workspaceScopeKey = workspaceScopeKeyProp ?? `${workspaceTabId}\u0000${cwd ?? ""}`;
  const lastWorkspaceScopeKeyRef = useRef(workspaceScopeKey);
  const scopeSwitchPendingRef = useRef(false);
  const workspaceMemoryKey = workspaceMemoryKeyProp ?? workspaceScopeKey;
  const workspaceMemoryVisitId = workspaceMemoryVisitIdProp ?? workspaceTreeVisitId(workspaceMemoryKey);
  const workspaceRefresh = useWorkspaceRefresh(workspaceTabId, workspaceScopeKey, open);
  const initialWorkspaceMemory = readWorkspaceTreeMemory(workspaceMemoryKey);
  const legacyTreeWidth = loadOptionalLayoutSize("workspaceTreeWidth");
  const panelRef = useRef<HTMLElement>(null);
  const treeRef = useRef<HTMLDivElement>(null);
  // Live panel width from ResizeObserver: during a panel drag the width is
  // driven by a CSS variable (no React re-render per frame), so the split /
  // tree-hidden decisions must react to the element's real width — otherwise
  // the tree only disappears after the pointer is released.
  const [livePanelWidth, setLivePanelWidth] = useState<number | null>(null);
  useEffect(() => {
    const el = panelRef.current;
    if (!el) return;
    const update = () => setLivePanelWidth(el.getBoundingClientRect().width);
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  const onWorkspaceTreeScroll = useWorkspaceTreeScrollPersistence({ memoryKey: workspaceMemoryKey, open, scrollRef: treeRef });
  const filterRef = useRef<HTMLInputElement>(null);
  const previewBodyRef = useRef<HTMLDivElement>(null);
  const [entriesByDir, setEntriesByDir] = useState<Record<string, DirEntry[]>>({});
  const [openDirs, setOpenDirs] = useState<Set<string>>(
    () => new Set(initialWorkspaceMemory?.openDirs ?? [""]),
  );
  // Search-mode directories start expanded; clicking a folder toggles it here
  // (separate from openDirs so filtering never mutates the normal tree state).
  const [collapsedSearchDirs, setCollapsedSearchDirs] = useState<Set<string>>(() => new Set());
  const [revealedRootPaths, setRevealedRootPaths] = useState<Set<string> | null>(
    () => initialWorkspaceMemory && initialWorkspaceMemory.visitId !== workspaceMemoryVisitId ? new Set() : null,
  );
  // File preview and working-tree diff selection are independent navigation
  // states.  Keeping a single path here used to erase the user's file context
  // whenever the Changes tab refreshed or became active.
  const [selectedFilePath, setSelectedFilePath] = useState<string | null>(
    () => initialWorkspaceMemory?.selectedFilePath ?? null,
  );
  const [selectedChangePath, setSelectedChangePath] = useState<string | null>(
    () => initialWorkspaceMemory?.selectedChangePath ?? null,
  );
  const [openTabs, setOpenTabs] = useState<string[]>([]);
  // Independent "recently opened" history: survives closing all preview tabs
  // (openTabs is the live preview state) and app restarts, so the recent-files
  // menu keeps the user's file history even after the previews are dismissed.
  const [recentPaths, setRecentPaths] = useState<string[]>(() =>
    (initialWorkspaceMemory?.recentPaths ?? []).slice(0, WORKSPACE_MAX_PREVIEW_TABS),
  );
  const [previewResource, setPreviewResource] = useState(() => emptyKeyedResource<FilePreview>());
  const [viewMode, setViewMode] = useState<"files" | "changed">(initialViewMode);
  const selectedPath = viewMode === "changed" ? selectedChangePath : selectedFilePath;
  // Both creation and regular workspaces use the same three-layer change view;
  // keep the prop in the seam for older callers while making history collapsed
  // by default everywhere.
  const groupedChangesLayout = creationMode !== false || viewMode === "changed";
  const [gitHistoryResource, setGitHistoryResource] = useState(() => emptyKeyedResource<GitCommitView[]>());
  const [changeDetailResource, setChangeDetailResource] = useState(() => emptyKeyedResource<WorkspaceChangeDetailView>());
  const [expandedCommit, setExpandedCommit] = useState<string | null>(null);
  const [commitDetail, setCommitDetail] = useState<GitCommitDetailView | null>(null);
  const [loadingCommit, setLoadingCommit] = useState(false);
  const [selectionMenu, setSelectionMenu] = useState<{ x: number; y: number; text: string; path: string } | null>(null);
  const [treeMenu, setTreeMenu] = useState<{ x: number; y: number; path: string; isDir: boolean } | null>(null);
  const [treeBlankMenuPoint, setTreeBlankMenuPoint] = useState<ContextMenuPoint | null>(null);
  const [filter, setFilter] = useState("");
  // Leaving search (clearing the filter) resets the collapse state, so the
  // next search starts fully expanded instead of remembering which folders
  // were folded during the previous one.
  useEffect(() => {
    if (!filter.trim()) setCollapsedSearchDirs(new Set());
  }, [filter]);
  const [searchResults, setSearchResults] = useState<DirEntry[] | null>(null);
  const [scopedFilePaths, setScopedFilePaths] = useState<string[] | null>(null);
  const [scopedChangeRows, setScopedChangeRows] = useState<WorkspaceChangeListEntry[] | null>(null);
  const [treeVisible, setTreeVisible] = useState(true);
  const globalTreeWidthMemory = readWorkspaceTreeMemory(GLOBAL_TREE_WIDTH_KEY);
  const [treeWidth, setTreeWidth] = useState(globalTreeWidthMemory?.treeWidth ?? legacyTreeWidth ?? WORKSPACE_TREE_DEFAULT_WIDTH);
  const [treeWidthMode, setTreeWidthMode] = useState<WorkspaceSplitTreeWidthMode>(
    globalTreeWidthMemory?.treeWidthMode ?? "manual",
  );
  const [treeResizing, setTreeResizing] = useState(false);
  const [recentOpen, setRecentOpen] = useState(false);
  const [codeSearchRequestPending, setCodeSearchRequestPending] = useState(false);
  const [codeSearchRequestPath, setCodeSearchRequestPath] = useState<string | null>(null);
  /** Changes overview: commit history is secondary and starts collapsed. */
  const [commitHistoryOpen, setCommitHistoryOpen] = useState(false);
  const lastPreviewModeActiveRef = useRef<boolean | null>(null);
  const lastRevealRequestIdRef = useRef<number | null>(null);
  const dismissedRevealRequestIdRef = useRef<number | null>(null);
  const lastChangeRevealRequestIdRef = useRef<number | null>(null);
  const lastVerificationRevealRequestIdRef = useRef<number | null>(null);
  const verificationSummaryRef = useRef<HTMLElement | null>(null);
  const dismissedChangeRevealRequestIdRef = useRef<number | null>(null);
  const lastFileListRequestIdRef = useRef<number | null>(null);
  const dismissedFileListRequestIdRef = useRef<number | null>(null);
  const lastChangeListRequestIdRef = useRef<number | null>(null);
  const dismissedChangeListRequestIdRef = useRef<number | null>(null);
  const currentWorkspaceScopeKeyRef = useRef(workspaceScopeKey);
  const changeDetailRequestIdRef = useRef(0);
  const gitHistoryRequestIdRef = useRef(0);
  const previewRequestIdRef = useRef(0);
  const commitDetailRequestIdRef = useRef(0);
  const dirLoadGenerationRef = useRef(0);
  const dirLoadRequestIdsRef = useRef<Record<string, number>>({});
  const compactProbeInFlightRef = useRef(new Set<string>());
  const recentAnchorRef = useRef<HTMLButtonElement>(null);
  const openDirsRef = useRef(openDirs);
  const pendingTreeRevealPathRef = useRef<string | null>(null);
  const lastRestoredMemoryKeyRef = useRef(workspaceMemoryKey);
  const memoryRestorePendingRef = useRef(false);
  const workingTreeRefreshSchedulerRef = useRef<ReturnType<typeof createWorkspaceRefreshScheduler> | null>(null);
  const gitMetaRefreshSchedulerRef = useRef<ReturnType<typeof createWorkspaceRefreshScheduler> | null>(null);
  if (!workingTreeRefreshSchedulerRef.current) {
    workingTreeRefreshSchedulerRef.current = createWorkspaceRefreshScheduler(300);
  }
  if (!gitMetaRefreshSchedulerRef.current) {
    gitMetaRefreshSchedulerRef.current = createWorkspaceRefreshScheduler(750);
  }
  currentWorkspaceScopeKeyRef.current = workspaceScopeKey;
  const previewKey = selectedPath ? `${workspaceScopeKey}\u0000preview\u0000${selectedPath}` : null;
  const changeDetailKey = selectedPath ? `${workspaceScopeKey}\u0000change\u0000${selectedPath}` : null;
  const gitHistoryKey = `${workspaceScopeKey}\u0000history\u0000${selectedPath ?? ""}`;
  const preview = previewKey && previewResource.key === previewKey ? previewResource.data : null;
  const loadingPreview = previewKey != null && previewResource.key === previewKey && previewResource.status === "refreshing";
  const previewErr = previewKey && previewResource.key === previewKey ? previewResource.error : "";
  const changeDetail = changeDetailKey && changeDetailResource.key === changeDetailKey ? changeDetailResource.data : null;
  const loadingChangeDetail = changeDetailKey != null && changeDetailResource.key === changeDetailKey && changeDetailResource.status === "refreshing";
  const changeDetailErr = changeDetailKey && changeDetailResource.key === changeDetailKey ? changeDetailResource.error : "";
  const gitHistory = gitHistoryResource.key === gitHistoryKey ? gitHistoryResource.data ?? [] : [];
  const loadingHistory = gitHistoryResource.key === gitHistoryKey && gitHistoryResource.status === "refreshing";
  const gitHistoryErr = gitHistoryResource.key === gitHistoryKey ? gitHistoryResource.error : "";
  const { workspaceChanges, loadingWorkspaceChanges, workspaceChangesErr, loadWorkspaceChanges, resetWorkspaceChanges } =
    useWorkspaceChangesResource(workspaceTabId, workspaceScopeKey, workspaceRefresh.revisions.workingTree);
  const overviewResourceStatus = workspaceChangesErr && workspaceChanges
    ? { error: true, text: `${t("workspace.changesUnavailable")}: ${workspaceChangesErr}` }
    : gitHistoryErr && gitHistory.length > 0
      ? { error: true, text: `${t("workspace.historyUnavailable")}: ${gitHistoryErr}` }
      : loadingWorkspaceChanges && workspaceChanges
        ? { error: false, text: t("workspace.loadingChanges") }
        : loadingHistory && gitHistory.length > 0 ? { error: false, text: t("workspace.loading") } : null;

  useEffect(() => {
    openDirsRef.current = openDirs;
  }, [openDirs]);

  const updateOpenDirs = useCallback(
    (update: (previous: ReadonlySet<string>) => Set<string>) => {
      setOpenDirs((previous) => {
        const next = update(previous);
        rememberWorkspaceTreeOpenDirs(workspaceMemoryKey, next, workspaceMemoryVisitId);
        return next;
      });
    },
    [workspaceMemoryKey, workspaceMemoryVisitId],
  );

  useEffect(() => {
    memoryRestorePendingRef.current = true;
    const remembered = readWorkspaceTreeMemory(workspaceMemoryKey);
    const nextOpenDirs = new Set(remembered?.openDirs ?? [""]);
    setOpenDirs(nextOpenDirs);
    openDirsRef.current = nextOpenDirs;
    setRevealedRootPaths(remembered && remembered.visitId !== workspaceMemoryVisitId ? new Set() : null);
    if (remembered) touchWorkspaceTreeVisit(workspaceMemoryKey, workspaceMemoryVisitId);
    else rememberWorkspaceTreeOpenDirs(workspaceMemoryKey, nextOpenDirs, workspaceMemoryVisitId);
    if (lastRestoredMemoryKeyRef.current !== workspaceMemoryKey) {
      lastRestoredMemoryKeyRef.current = workspaceMemoryKey;
      setSelectedFilePath(remembered?.selectedFilePath ?? null);
      setSelectedChangePath(remembered?.selectedChangePath ?? null);
      const globalWidth = readWorkspaceTreeMemory(GLOBAL_TREE_WIDTH_KEY);
      setTreeWidth(globalWidth?.treeWidth ?? legacyTreeWidth ?? WORKSPACE_TREE_DEFAULT_WIDTH);
      setTreeWidthMode(globalWidth?.treeWidthMode ?? "manual");
      requestAnimationFrame(() => {
        const tree = treeRef.current;
        if (tree) tree.scrollTop = remembered?.scrollTop ?? 0;
      });
    }
  }, [creationMode, legacyTreeWidth, workspaceMemoryKey, workspaceMemoryVisitId]);

  useEffect(() => {
    if (memoryRestorePendingRef.current) return;
    rememberWorkspaceTreeState(workspaceMemoryKey, { selectedFilePath, selectedChangePath });
  }, [selectedChangePath, selectedFilePath, workspaceMemoryKey]);

  useEffect(() => {
    if (memoryRestorePendingRef.current) return;
    rememberWorkspaceTreeState(GLOBAL_TREE_WIDTH_KEY, { treeWidth, treeWidthMode });
  }, [treeWidth, treeWidthMode]);

  useEffect(() => {
    if (memoryRestorePendingRef.current) return;
    rememberWorkspaceTreeState(workspaceMemoryKey, { recentPaths });
  }, [recentPaths, workspaceMemoryKey]);

  useEffect(() => {
    memoryRestorePendingRef.current = false;
  });

  const loadDir = useCallback(async (dir: string) => {
    const requestTabId = workspaceTabId;
    const requestScopeKey = workspaceScopeKey;
    const generation = dirLoadGenerationRef.current;
    const requestId = (dirLoadRequestIdsRef.current[dir] ?? 0) + 1;
    dirLoadRequestIdsRef.current[dir] = requestId;
    const entries = asArray(await app.ListDirForTab(requestTabId, dir).catch((): DirEntry[] => []));
    if (
      currentWorkspaceScopeKeyRef.current !== requestScopeKey ||
      dirLoadGenerationRef.current !== generation ||
      dirLoadRequestIdsRef.current[dir] !== requestId
    ) return null;
    setEntriesByDir((prev) => ({ ...prev, [dir]: entries }));
    return entries;
  }, [workspaceScopeKey, workspaceTabId]);

  const loadGitHistory = useCallback(async () => {
    const requestId = ++gitHistoryRequestIdRef.current;
    const requestTabId = workspaceTabId;
    const requestScopeKey = workspaceScopeKey;
    const requestKey = `${requestScopeKey}\u0000history\u0000${selectedPath ?? ""}`;
    setGitHistoryResource((current) => beginKeyedResourceRequest(current, requestKey, requestId, workspaceRefresh.revisions.gitMeta));
    try {
      const result = await app.WorkspaceGitHistory(requestTabId, selectedPath || "");
      if (gitHistoryRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
        setGitHistoryResource((current) => resolveKeyedResourceRequest(current, requestKey, requestId, result || [], workspaceRefresh.revisions.gitMeta));
      }
    } catch (err) {
      if (gitHistoryRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
        setGitHistoryResource((current) => rejectKeyedResourceRequest(current, requestKey, requestId, String((err as { message?: unknown })?.message ?? err)));
      }
    }
  }, [selectedPath, workspaceRefresh.revisions.gitMeta, workspaceScopeKey, workspaceTabId]);

  const loadChangeDetail = useCallback(async () => {
    const requestId = ++changeDetailRequestIdRef.current;
    const requestTabId = workspaceTabId;
    const requestScopeKey = workspaceScopeKey;
    const requestPath = selectedPath;
    if (!requestPath) {
      setChangeDetailResource(emptyKeyedResource());
      return;
    }
    // Scope switch must not request the previous scope's selected change:
    // the selection is preserved for the original scope, but a request must
    // not fire for the just-switched scope before the scope effect settles.
    if (scopeSwitchPendingRef.current) return;
    const requestKey = `${requestScopeKey}\u0000change\u0000${requestPath}`;
    setChangeDetailResource((current) => beginKeyedResourceRequest(current, requestKey, requestId, workspaceRefresh.revisions.content));
    try {
      const detail = await app.WorkspaceChangeDetail(requestTabId, requestPath);
      if (changeDetailRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
        setChangeDetailResource((current) => resolveKeyedResourceRequest(current, requestKey, requestId, detail ?? {}, workspaceRefresh.revisions.content));
      }
    } catch (err) {
      if (changeDetailRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
        setChangeDetailResource((current) => rejectKeyedResourceRequest(current, requestKey, requestId, String((err as { message?: unknown })?.message ?? err)));
      }
    }
  }, [selectedPath, workspaceRefresh.revisions.content, workspaceScopeKey, workspaceTabId]);

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
    (path: string, targetMode: "files" | "changed" = viewMode) => {
      const initializeSplit = shouldInitializeWorkspaceSplitOnFileSelect({
        previewVisible: openTabs.length > 0 || selectedPath !== null,
        treeVisible,
      });
      if (initializeSplit) {
        setTreeWidth(initialWorkspaceSplitTreeWidth({
          panelWidth,
          // The tree keeps its width (default 200) instead of an even 50/50
          // split, so the preview pane gets the extra room when a file opens.
          // Reopening a file after closing its preview keeps the remembered
          // width, not snap back to an initial split.
          savedTreeWidth: treeWidth,
          treeMinWidth: WORKSPACE_TREE_MIN_WIDTH,
          previewMinWidth: WORKSPACE_PREVIEW_MIN_WIDTH,
        }));
        setTreeWidthMode("manual");
      }
      pendingTreeRevealPathRef.current = path;
      if (targetMode === "changed") setSelectedChangePath(path);
      else setSelectedFilePath(path);
      setScopedFilePaths((current) => {
        if (current) dismissedFileListRequestIdRef.current = lastFileListRequestIdRef.current;
        return null;
      });
      setScopedChangeRows((current) => {
        if (current) dismissedChangeListRequestIdRef.current = lastChangeListRequestIdRef.current;
        return null;
      });
      // Keep the active search: clicking a result opens its preview but must
      // not leave the search tree, so the user can keep browsing hits.
      setOpenTabs((tabs) => [...tabs.filter((tab) => tab !== path), path].slice(-WORKSPACE_MAX_PREVIEW_TABS));
      setRecentPaths((paths) => [...paths.filter((p) => p !== path), path].slice(-WORKSPACE_MAX_PREVIEW_TABS));
      const dirs = parentDirs(path);
      updateOpenDirs((prev) => new Set([...Array.from(prev), ...dirs]));
      dirs.forEach((dir) => void loadDir(dir));
    },
    [loadDir, openTabs.length, panelWidth, selectedPath, treeVisible, updateOpenDirs, viewMode],
  );

  // Report the open preview files upward so the dock keeps its file tab in
  // sync (single mirror tab whose label follows the active preview).
  useEffect(() => {
    onOpenFilesChange?.(openTabs, viewMode === "changed" ? selectedChangePath : selectedFilePath);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onOpenFilesChange, openTabs, selectedChangePath, selectedFilePath, viewMode]);

  useEffect(() => {
    if (!open) return;
    dirLoadGenerationRef.current += 1;
    dirLoadRequestIdsRef.current = {};
    compactProbeInFlightRef.current.clear();
    setEntriesByDir({});
    setOpenTabs([]);
    setPreviewResource(emptyKeyedResource());
    setGitHistoryResource(emptyKeyedResource());
    changeDetailRequestIdRef.current += 1;
    setChangeDetailResource(emptyKeyedResource());
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

  // The backend session for the active tab may not have finished booting when
  // the panel first mounts (or when the tab is re-activated after a switch):
  // ListDirForTab then returns an empty list even though the workspace is not
  // actually empty, and the file tree renders blank. Re-load the tree once the
  // tab reports ready, so the root + any expanded directories populate — this
  // is what a manual refresh was doing before the retry. Track the previous
  // readiness so a tab that was already ready on mount (a normal session) does
  // not double-load, while a tab that becomes ready after mount (a cold start
  // or a re-activation race) repopulates the tree.
  const previousTabReadyRef = useRef(tabReady);
  useEffect(() => {
    if (!open || !tabReady) return;
    if (previousTabReadyRef.current === tabReady) return;
    previousTabReadyRef.current = tabReady;
    const pendingDirs = [""];
    openDirsRef.current.forEach((dir) => pendingDirs.push(dir));
    pendingDirs.forEach((dir) => void loadDir(dir));
  }, [open, tabReady, loadDir]);

  useEffect(() => {
    if (!open) return;
    if (lastWorkspaceScopeKeyRef.current === workspaceScopeKey) return;
    lastWorkspaceScopeKeyRef.current = workspaceScopeKey;
    scopeSwitchPendingRef.current = true;
    workingTreeRefreshSchedulerRef.current?.cancel();
    gitMetaRefreshSchedulerRef.current?.cancel();
    changeDetailRequestIdRef.current += 1;
    gitHistoryRequestIdRef.current += 1;
    commitDetailRequestIdRef.current += 1;
    resetWorkspaceChanges();
    setChangeDetailResource(emptyKeyedResource());
    setGitHistoryResource(emptyKeyedResource());
    setCommitHistoryOpen(false);
    setExpandedCommit(null);
    setCommitDetail(null);
    setScopedChangeRows(null);
    lastChangeRevealRequestIdRef.current = null;
    dismissedChangeRevealRequestIdRef.current = null;
    lastChangeListRequestIdRef.current = null;
    dismissedChangeListRequestIdRef.current = null;
    if (viewMode === "changed") {
      setOpenTabs([]);
      setPreviewResource(emptyKeyedResource());
    }
  }, [open, resetWorkspaceChanges, viewMode, workspaceScopeKey]);

  useEffect(() => () => {
    workingTreeRefreshSchedulerRef.current?.cancel();
    gitMetaRefreshSchedulerRef.current?.cancel();
  }, []);

  // A tab/scope switch must discard the floating menus: their text and paths
  // were captured from the previous scope, while add-to-chat routes to
  // whatever tab is active at click time — a menu surviving a keyboard tab
  // switch would add the old scope's selection to the new session.
  useEffect(() => {
    setSelectionMenu(null);
    setTreeMenu(null);
    setTreeBlankMenuPoint(null);
  }, [tabId, workspaceScopeKey]);

  useEffect(() => {
    if (!open) return;
    setViewMode(initialViewMode);
    setCommitHistoryOpen(false);
    setExpandedCommit(null);
    setCommitDetail(null);
    changeDetailRequestIdRef.current += 1;
    setChangeDetailResource(emptyKeyedResource());
    setSelectionMenu(null);
    setTreeMenu(null);
    setRecentOpen(false);
    if (initialViewMode === "changed") {
      setScopedFilePaths(null);
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
    setSelectedFilePath(null);
    setOpenTabs([]);
    setPreviewResource(emptyKeyedResource());
    setFilter("");
    setExpandedCommit(null);
    setCommitDetail(null);
    setSelectionMenu(null);
    setTreeMenu(null);
    const dirs = Array.from(new Set(paths.flatMap(parentDirs)));
    updateOpenDirs((prev) => new Set([...Array.from(prev), ...dirs]));
    dirs.forEach((dir) => void loadDir(dir));
  }, [fileListRequest, loadDir, open, scopedFilePaths, updateOpenDirs, viewMode]);

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
    setSelectedChangePath(null);
    setOpenTabs([]);
    setPreviewResource(emptyKeyedResource());
    setFilter("");
    setExpandedCommit(null);
    setCommitDetail(null);
    setSelectionMenu(null);
    setTreeMenu(null);
  }, [changeListRequest, open, scopedChangeRows, viewMode]);

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
    selectFile(revealPathRequest.path, "files");
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
    setSelectedChangePath(changeRevealRequest.path);
    setOpenTabs([]);
    setPreviewResource(emptyKeyedResource());
    setFilter("");
    setExpandedCommit(null);
    setCommitDetail(null);
    setSelectionMenu(null);
    setTreeMenu(null);
  }, [changeRevealRequest, open, selectedPath, viewMode]);

  useEffect(() => {
    if (!open || !activeVerificationRevealRequest) return;
    if (lastVerificationRevealRequestIdRef.current !== activeVerificationRevealRequest.id) {
      lastVerificationRevealRequestIdRef.current = activeVerificationRevealRequest.id;
      setViewMode("changed");
      setSelectedChangePath(null);
      setOpenTabs([]);
      setPreviewResource(emptyKeyedResource());
      setFilter("");
      setExpandedCommit(null);
      setCommitDetail(null);
      setSelectionMenu(null);
      setTreeMenu(null);
      setScopedChangeRows(null);
      if (viewMode !== "changed" || selectedChangePath) return;
    }
    if (viewMode !== "changed" || selectedChangePath) return;
    const node = verificationSummaryRef.current ?? document.getElementById(WORKSPACE_TURN_VERIFICATION_ID);
    node?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [activeVerificationRevealRequest, open, selectedChangePath, viewMode]);

  useEffect(() => {
    if (!open) return;
    if (viewMode === "changed") {
      void loadWorkspaceChanges();
      // A scope switch just happened in this render batch: the previous
      // scope's selected change must not be re-requested, but the selection
      // itself is preserved for when the user returns to that scope.
      if (selectedPath && !scopeSwitchPendingRef.current) void loadChangeDetail();
      scopeSwitchPendingRef.current = false;
      if (commitHistoryOpen) void loadGitHistory();
    } else {
      changeDetailRequestIdRef.current += 1;
      setChangeDetailResource(emptyKeyedResource());
    }
  }, [commitHistoryOpen, selectedPath, viewMode, loadChangeDetail, loadGitHistory, loadWorkspaceChanges, open]);

  useEffect(() => {
    if (!selectionMenu && !treeMenu) return;
    const close = () => {
      setSelectionMenu(null);
      setTreeMenu(null);
    };
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    // Dismiss on mousedown rather than click: the trailing click a drag-selection
    // emits would otherwise close the toolbar the instant mouseup opens it. A fresh
    // mousedown only fires when the user starts another interaction, and FloatingMenu
    // stops propagation so pressing its buttons never counts as an outside press.
    window.addEventListener("mousedown", close);
    const panel = panelRef.current;
    panel?.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", close);
      panel?.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
      window.removeEventListener("keydown", onKey);
    };
  }, [selectionMenu, treeMenu]);

  const refreshWorkspaceList = useCallback(() => {
    setTreeBlankMenuPoint(null);
    setSelectionMenu(null);
    setTreeMenu(null);
    if (viewMode === "changed") {
      workingTreeRefreshSchedulerRef.current?.cancel();
      gitMetaRefreshSchedulerRef.current?.cancel();
      void loadWorkspaceChanges();
      if (selectedPath) void loadChangeDetail();
      // Manual refresh is the explicit escape hatch and includes collapsed
      // history even when ordinary file writes do not.
      void loadGitHistory();
      return;
    }
    onFileTreeRefresh?.();
    const dirs = Array.from(openDirsRef.current);
    dirs.forEach((dir) => void loadDir(dir));
  }, [loadChangeDetail, loadGitHistory, loadWorkspaceChanges, loadDir, onFileTreeRefresh, selectedPath, viewMode]);

  const refreshSelected = useCallback(() => {
    if (!selectedPath) return;
    const requestId = ++previewRequestIdRef.current;
    const requestScopeKey = workspaceScopeKey;
    const requestPath = selectedPath;
    const requestKey = `${requestScopeKey}\u0000preview\u0000${requestPath}`;
    let live = true;
    setPreviewResource((current) => beginKeyedResourceRequest(current, requestKey, requestId, workspaceRefresh.revisions.content));
    app
      .ReadFileForTab(workspaceTabId, requestPath)
      .then((next) => {
        if (live && previewRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
          setPreviewResource((current) => resolveKeyedResourceRequest(current, requestKey, requestId, next, workspaceRefresh.revisions.content));
        }
      })
      .catch((err) => {
        if (live && previewRequestIdRef.current === requestId && currentWorkspaceScopeKeyRef.current === requestScopeKey) {
          setPreviewResource((current) => rejectKeyedResourceRequest(current, requestKey, requestId, String(err?.message ?? err)));
        }
      });
    return () => {
      live = false;
    };
  }, [selectedPath, workspaceRefresh.revisions.content, workspaceScopeKey, workspaceTabId]);

  useEffect(() => {
    if (!open || !selectedPath) return;
    return refreshSelected();
  }, [open, refreshSelected, selectedPath]);

  useWorkspaceRefreshInvalidation({ commitHistoryOpen,
    filter,
    gitMetaSchedulerRef: gitMetaRefreshSchedulerRef,
    loadChangeDetail,
    loadDir,
    loadGitHistory,
    loadWorkspaceChanges,
    open,
    openDirsRef,
    refreshSelected,
    selectedPath,
    setSearchResults,
    viewMode,
    workingTreeSchedulerRef: workingTreeRefreshSchedulerRef,
    workspaceRefresh,
    workspaceScopeKey,
  });

  const toggleDir = useCallback(
    (dir: string, compactPaths: string[] = [dir]) => {
      const firstPath = compactPaths[0] ?? dir;
      const rootPath = topLevelDirPath(firstPath);
      if (revealedRootPaths !== null && !revealedRootPaths.has(rootPath)) {
        setRevealedRootPaths((current) => new Set([...(current ?? []), rootPath]));
        const remembered = openDirsRef.current;
        if (!remembered.has(firstPath)) {
          updateOpenDirs((previous) => new Set([...previous, ...compactPaths]));
          compactPaths.forEach((path) => void loadDir(path));
          return;
        }
        remembered.forEach((path) => {
          if (path === rootPath || path.startsWith(rootPath)) void loadDir(path);
        });
        return;
      }

      const isOpen = openDirsRef.current.has(firstPath);
      updateOpenDirs((previous) => {
        const next = new Set(previous);
        compactPaths.forEach((path) => {
          if (isOpen) next.delete(path);
          else next.add(path);
        });
        return next;
      });
      if (!isOpen) compactPaths.forEach((path) => void loadDir(path));
    },
    [loadDir, revealedRootPaths, updateOpenDirs],
  );

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
            <div key={change.path} className="workspace-change-row">
              <button
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
                  {change.gitStatus && <span className="workspace-change__badge workspace-change__badge--git">{workspaceGitStatusLabel(change.gitStatus, t)}</span>}
                </span>
              </button>
              {change.canSessionRevert && change.sources.includes("session") && (
                <button
                  type="button"
                  className="workspace-change__revert"
                  title={t("workspace.revertSessionFile")}
                  onClick={(e) => {
                    e.stopPropagation();
                    void (async () => {
                      const plan = await app.PreviewWorkspaceFileRevertForTab(workspaceTabId, change.path);
                      if (!plan?.ok && !plan?.canFiles && !(plan?.conflicts?.length)) {
                        return;
                      }
					  const resolution = plan?.conflicts?.length ? "overwrite_checkpoint" : "";
                      if (plan?.conflicts?.length) {
                        const ok = window.confirm(
                          t("workspace.revertSessionFileConflict", {
                            path: change.path,
                            conflicts: (plan.conflicts || []).join("\n"),
                          }),
                        );
                        if (!ok) return;
                      }
                      const result = await app.CommitWorkspaceFileRevertForTab(
                        workspaceTabId,
                        plan.planId || "",
                        resolution,
                      );
                      if (result?.ok) {
                        onSessionRevertCommitted?.(workspaceTabId, result);
                        void loadWorkspaceChanges();
                        if (selectedPath === change.path) void loadChangeDetail();
                      }
                    })();
                  }}
                >
                  {t("workspace.revertSessionFileShort")}
                </button>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );

  const changedMode = viewMode === "changed";
  const currentFileName = selectedPath ? basename(selectedPath) : t("workspace.noFile");
  const previewTitle = changedMode && !selectedPath
    ? scopedChangeRows ? t("context.sessionChanges") : t("workspace.changedTab")
    : currentFileName;
  const activeSelectedPath = selectedPath;
  // Changed overview shows just the project name (matching the file-preview
  // breadcrumb); hovering reveals the full absolute workspace root.
  const previewSubtitleCrumbs = changedMode && !selectedChangePath && !scopedChangeRows
    ? (cwd ? [{ label: basename(cwd), full: cwd, relative: basename(cwd) }] : [])
    : [];
  const previewFullPath = activeSelectedPath || "";
  const previewCrumbs = buildWorkspacePathBreadcrumbs(cwd, previewFullPath);
  // The current-file header shows the directory breadcrumbs (the file name
  // lives in the dock/top tab), but selecting the breadcrumbs should copy the
  // full project-relative path with the file name, joined by "/" — the "›"
  // separators shown on screen are display-only.
  const copyCurrentFileRelativePath = useCallback((event: ReactClipboardEvent<HTMLDivElement>) => {
    const selection = window.getSelection();
    if (!selection || selection.isCollapsed || !selectedPath || !cwd) return;
    const container = event.currentTarget;
    if (!container.contains(selection.anchorNode) || !container.contains(selection.focusNode)) return;
    event.preventDefault();
    event.clipboardData.setData("text/plain", `${basename(cwd)}/${selectedPath.replace(/^[/\\]+/, "")}`);
  }, [cwd, selectedPath]);
  const recentFiles = useMemo(() => [...recentPaths].reverse(), [recentPaths]);

  const workspaceSearchFallbackSequence = workspaceRefreshFallbackSequence(workspaceRefresh);

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
  }, [filter, viewMode, scopedFilePaths, open, workspaceRefresh.revisions.tree, workspaceSearchFallbackSequence, workspaceScopeKey, workspaceTabId]);

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

  const treeRows = useMemo<WorkspaceTreeRowData[]>(() => {
    if (flattened) {
      // Search results render as a directory tree: each hit hangs under its
      // folder chain, so it is obvious which directory tree a file lives in.
      // Directories default to expanded and can be collapsed.
      const fileRows = new Map<string, WorkspaceTreeRowData>();
      const dirPaths = new Set<string>();
      for (const row of flattened) {
        if (row.entry.isDir) {
          // Directory keys are slash-free so they match the parentPath keys
          // used by file rows (a/ vs a would otherwise split the buckets and
          // drop every file from the tree).
          dirPaths.add(row.path.replace(/\/$/, ""));
        } else {
          fileRows.set(row.path, { ...row, depth: 0, key: `f:${row.path}`, active: false });
        }
        let dir = parentPath(row.path);
        while (dir) {
          dirPaths.add(dir);
          dir = parentPath(dir);
        }
      }
      const children = new Map<string, { dirs: string[]; files: string[] }>();
      const ensure = (dir: string) => {
        let bucket = children.get(dir);
        if (!bucket) {
          bucket = { dirs: [], files: [] };
          children.set(dir, bucket);
        }
        return bucket;
      };
      for (const dir of dirPaths) ensure(parentPath(dir)).dirs.push(dir);
      for (const file of fileRows.keys()) ensure(parentPath(file)).files.push(file);
      for (const bucket of children.values()) {
        bucket.dirs.sort((a, b) => a.localeCompare(b));
        bucket.files.sort((a, b) => a.localeCompare(b));
      }
      const acc: WorkspaceTreeRowData[] = [];
      const buildSearchDir = (dir: string, depth: number) => {
        // Collapse single-child directory chains (dir → dir → … with no
        // matching files in between) into one row "a / b / c", so deep
        // workdir-style paths don't eat vertical space. The chain head is the
        // collapse key; expanding reveals the chain's tail children.
        const compactNames = [basename(dir)];
        const chainHead = dir;
        let bucket = children.get(dir) ?? { dirs: [], files: [] };
        while (bucket.dirs.length === 1 && bucket.files.length === 0) {
          const sub = bucket.dirs[0];
          compactNames.push(basename(sub));
          bucket = children.get(sub) ?? { dirs: [], files: [] };
        }
        const hasChildren = bucket.dirs.length > 0 || bucket.files.length > 0;
        // A directory that only matched by name (no matching descendants)
        // renders as a leaf: isOpen undefined hides the chevron, so it cannot
        // expand into an empty folder.
        const isOpen = hasChildren ? !collapsedSearchDirs.has(chainHead) : undefined;
        acc.push({
          key: `d:${chainHead}`,
          path: chainHead,
          depth,
          entry: { name: compactNames.join(" / "), isDir: true },
          active: false,
          isOpen,
        });
        if (!hasChildren || !isOpen) return;
        for (const sub of bucket.dirs) buildSearchDir(sub, depth + 1);
        for (const file of bucket.files) {
          const row = fileRows.get(file);
          if (!row) continue;
          acc.push({ ...row, depth: depth + 1, active: selectedPath === file, key: `f:${file}` });
        }
      };
      const rootBucket = children.get("") ?? { dirs: [], files: [] };
      for (const dir of rootBucket.dirs) buildSearchDir(dir, 0);
      for (const file of rootBucket.files) {
        const row = fileRows.get(file);
        if (!row) continue;
        acc.push({ ...row, depth: 0, active: selectedPath === file, key: `f:${file}` });
      }
      return acc;
    }
    const acc: WorkspaceTreeRowData[] = [];
    const build = (dir: string, depth: number) => {
      const entries = entriesByDir[dir] ?? [];
      for (const entry of entries) {
        const firstPath = entryPath(dir, entry);
        if (!entry.isDir) {
          acc.push({
            key: firstPath,
            path: firstPath,
            depth,
            entry,
            active: selectedPath === firstPath,
            isOpen: false,
          });
          continue;
        }

        const compactPaths = [firstPath];
        const compactNames = [entry.name];
        let lastPath = firstPath;
        let lastEntry = entry;
        while (true) {
          const children = entriesByDir[lastPath];
          if (!children || children.length !== 1 || !children[0]?.isDir) break;
          lastEntry = children[0];
          lastPath = entryPath(lastPath, lastEntry);
          compactPaths.push(lastPath);
          compactNames.push(lastEntry.name);
        }
        const rootPath = topLevelDirPath(firstPath);
        const isRevealed = revealedRootPaths === null || revealedRootPaths.has(rootPath);
        const isOpen = isRevealed && openDirs.has(firstPath);
        acc.push({
          key: lastPath,
          path: lastPath,
          depth,
          entry: lastEntry,
          active: selectedPath === lastPath,
          isOpen,
          compactPaths,
          displayName: compactNames.join(" / "),
        });
        if (isOpen) {
          build(lastPath, depth + 1);
        }
      }
    };
    build("", 0);
    return acc;
  }, [flattened, entriesByDir, openDirs, revealedRootPaths, selectedPath, collapsedSearchDirs]);
  const getTreeRowKey = useCallback((index: number) => treeRows[index]?.key ?? index, [treeRows]);

  const virtualizer = useVirtualizer({
    count: treeRows.length,
    getScrollElement: () => treeRef.current,
    estimateSize: () => 24,
    getItemKey: getTreeRowKey,
    overscan: 10,
    directDomUpdates: true,
  });
  // Restore the persisted scroll position once the tree has grown tall enough
  // to actually reach it. The tree loads asynchronously layer by layer: the
  // first render usually has only the top-level rows, so scrolling then would
  // clamp the saved offset to the (tiny) current scrollable range and lose the
  // position forever. Keep a pending target and retry on every tree change
  // until the tree's total height exceeds it, then scroll once.
  const pendingScrollRestoreRef = useRef<number | null>(null);
  // Reset the pending restore whenever the view mode changes: the saved offset
  // belongs to the previous mode's tree, and re-applying it to a shorter list
  // (e.g. files -> changed) scrolls past the end and leaves a blank band at
  // the top. Each mode restores its own offset on its own data.
  const previousViewModeRef = useRef(viewMode);
  if (previousViewModeRef.current !== viewMode) {
    previousViewModeRef.current = viewMode;
    pendingScrollRestoreRef.current = null;
  }
  useEffect(() => {
    if (!open || treeRows.length === 0) return;
    if (pendingScrollRestoreRef.current == null) {
      const saved = readWorkspaceTreeMemory(workspaceMemoryKey)?.scrollTop;
      if (saved == null || !Number.isFinite(saved) || saved <= 0) return;
      pendingScrollRestoreRef.current = saved;
    }
    const target = pendingScrollRestoreRef.current;
    // A saved offset is only reachable once the tree's total height exceeds
    // the offset by at least one viewport: the maximum scroll position is
    // totalSize - viewportHeight. Using totalSize < target would clear the
    // pending restore once content just barely exceeds the offset, but before
    // it provides enough scrollable space — the browser then clamps the scroll
    // and the deep-scroll restore is silently lost. Use the virtualizer's own
    // viewport rect (reliable after remount) rather than reading clientHeight
    // at restore time, which can be 0 while the tree is still settling.
    const viewportHeight = virtualizer.scrollRect?.height ?? treeRef.current?.clientHeight ?? 0;
    if (virtualizer.getTotalSize() - viewportHeight < target) return; // not enough scrollable space yet
    virtualizer.scrollToOffset(target, { align: "start" });
    pendingScrollRestoreRef.current = null;
  }, [open, treeRows.length, virtualizer.getTotalSize(), virtualizer.scrollRect?.height, workspaceMemoryKey, virtualizer]);

  const virtualTreeItems = virtualizer.getVirtualItems();
  const compactProbePaths = flattened
    ? []
    : virtualTreeItems
      .map((item) => treeRows[item.index])
      .filter((row): row is WorkspaceTreeRowData => Boolean(row?.entry.isDir && entriesByDir[row.path] === undefined))
      .map((row) => row.path);
  const compactProbeKey = compactProbePaths.join("\u0000");

  useEffect(() => {
    if (!open || !compactProbeKey) return;
    compactProbePaths.forEach((path) => {
      if (compactProbeInFlightRef.current.has(path)) return;
      compactProbeInFlightRef.current.add(path);
      void loadDir(path).finally(() => compactProbeInFlightRef.current.delete(path));
    });
  }, [compactProbeKey, loadDir, open]);

  const searchPlaceholder = t(scopedFilePaths ? "workspace.filterReferencedFiles" : changedMode ? "workspace.filterChanges" : "workspace.filter");

  // The files view keeps its two-column layout when the panel is wide enough:
  // tree on the right, file-detail pane on the left — empty ("no file
  // selected") until a file is picked, which is how a freshly added file tab
  // looks. When the panel is too narrow for both panes, fall back to showing
  // the tree alone until a file is actually selected (otherwise a narrow
  // panel with no selection would be blank).
  const embeddedDockMode = !showViewTabs;
  const showFileTools = true;
  const effectiveTreeWidth = useMemo(
    () =>
      resolveWorkspaceSplitTreeWidth({
        mode: treeWidthMode,
        currentTreeWidth: treeWidth,
        panelWidth,
        treeMinWidth: WORKSPACE_TREE_MIN_WIDTH,
        previewMinWidth: WORKSPACE_PREVIEW_MIN_WIDTH,
      }),
    [panelWidth, treeWidth, treeWidthMode],
  );
  // Fit is judged against the tree's ACTUAL width and a comfortable preview
  // minimum (300, not the 140 absolute floor): with a file selected, dragging
  // the panel narrow enough that the preview would drop below ~300px hides
  // the tree and lets the preview take the full panel — a sliver of preview
  // beside a wide tree is pointless. Uses the ResizeObserver-measured width
  // so the tree hides live while the panel is being dragged.
  const effectivePanelWidth = livePanelWidth ?? panelWidth;
  const splitPanesFit = useMemo(
    () =>
      workspaceSplitCanFit({
        panelWidth: effectivePanelWidth,
        treeMinWidth: effectiveTreeWidth,
        previewMinWidth: WORKSPACE_PREVIEW_COMFORT_WIDTH,
      }),
    [effectivePanelWidth, effectiveTreeWidth],
  );
  // Files view: once a file is selected the preview must show (two columns
  // when both fit, full-panel preview otherwise). With no selection the
  // detail pane is empty, so only keep the two columns when the panel leaves
  // a comfortable width for it — a narrow panel shows the tree alone, which
  // is the expected fresh-file-tab look.
  const filePreviewActive = viewMode === "changed"
    ? false
    : selectedPath !== null
      ? true
      : (effectivePanelWidth ?? 0) - effectiveTreeWidth >= WORKSPACE_PREVIEW_COMFORT_WIDTH;
  const changeDetailActive = changedMode && expandedCommit !== null;
  const previewVisible = changedMode || filePreviewActive;
  const actualTreeVisible = changedMode ? false : treeVisible && (!previewVisible || splitPanesFit);
  const previewModeActive = open && (filePreviewActive || changeDetailActive);
  const maxTreeWidthForPanel = useMemo(
    () => Math.max(WORKSPACE_TREE_MIN_WIDTH, (panelWidth ?? WORKSPACE_DUAL_PANEL_TARGET_WIDTH) - WORKSPACE_PREVIEW_MIN_WIDTH),
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
    // Reopening the hidden tree keeps the previous width (the user's last
    // resize / remembered global width) instead of resetting to a 50/50
    // split — treeWidth is untouched and the mode goes manual so the
    // persisted width applies.
    setTreeWidthMode("manual");
    setTreeVisible(true);
  }, []);

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
    if (viewMode === "changed") {
      setSelectedChangePath(null);
    } else {
      const nextPreviewTabs = closeWorkspacePreviewTab(openTabs, selectedFilePath);
      setSelectedFilePath(nextPreviewTabs.selectedPath);
      setOpenTabs(nextPreviewTabs.openTabs);
    }
    setPreviewResource(emptyKeyedResource());
    setSelectionMenu(null);
    setTreeMenu(null);
    setRecentOpen(false);
    setTreeVisible(true);
  }, [changeRevealRequest, openTabs, revealPathRequest, selectedFilePath, viewMode]);

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
          treeMinWidth: WORKSPACE_TREE_MIN_WIDTH,
          previewMinWidth: WORKSPACE_PREVIEW_MIN_WIDTH,
          treeOnRight: true,
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
        setSavedTreeWidth(effectiveTreeWidth + (event.key === "ArrowLeft" ? 16 : -16));
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

  useEffect(() => {
    setCodeSearchRequestPending(false);
    setCodeSearchRequestPath(null);
  }, [selectedPath]);

  const consumeCodeSearchRequest = useCallback(() => {
    setCodeSearchRequestPending(false);
  }, []);

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
    if (!selectedPath || (loadingPreview && !preview) || previewErr || preview?.err || preview?.binary || preview?.kind) return;
    const text = selectedTextFromPreview();
    if (text.trim() === "") return;
    event.preventDefault();
    event.stopPropagation();
    setSelectionMenu({ x: event.clientX, y: event.clientY, text, path: selectedPath });
  };

  // Selecting code with the mouse pops the "Add to Chat" button right away,
  // so a snippet is one click from the composer instead of right-click →
  // menu item. The right-click menu (openSelectionMenu) stays as a fallback.
  const showSelectionToolbar = (event: ReactMouseEvent<HTMLDivElement>) => {
    // Mouseup on the floating button bubbles back here through the portal's
    // React tree; let the button handle it.
    if ((event.target as HTMLElement | null)?.closest(".floating-menu")) return;
    if (!selectedPath || (loadingPreview && !preview) || previewErr || preview?.err || preview?.binary || preview?.kind) return;
    const text = selectedTextFromPreview();
    if (text.trim() === "") return;
    setSelectionMenu({ x: event.clientX, y: event.clientY + 8, text, path: selectedPath });
  };

  const addSelectionToChat = () => {
    if (!selectionMenu) return;
    if (onAddCodeToChat) onAddCodeToChat(selectionMenu.path, selectionMenu.text);
    else onAddToChat?.(formatSelectionReference(selectionMenu.path, selectionMenu.text));
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
      const body = file.truncated ? `${file.body}\n\n${t("workspace.truncated")}` : file.body;
      if (onAddCodeToChat) onAddCodeToChat(target.path, body);
      else onAddToChat?.(formatSelectionReference(target.path, body));
    } catch {
      if (currentWorkspaceScopeKeyRef.current !== requestScopeKey) return;
      onAddToChat?.(formatWorkspaceReference(target.path, false));
    }
  };

  const activateTreeRow = (row: WorkspaceTreeRowData) => {
    if (row.entry.isDir) {
      if (flattened) {
        // Search tree: clicking a folder collapses/expands its hits.
        setCollapsedSearchDirs((current) => {
          const next = new Set(current);
          if (next.has(row.path)) next.delete(row.path);
          else next.add(row.path);
          return next;
        });
        return;
      }
      toggleDir(row.path, row.compactPaths ?? [row.path]);
    } else if (selectedPath === row.path) {
      // Deselect: the detail pane goes back to "no file selected" (empty);
      // the two-column layout stays (files view always shows tree + detail).
      setSelectedFilePath(null);
    } else {
      selectFile(row.path);
    }
  };

  const isMarkdown = selectedPath?.toLowerCase().endsWith(".md") ?? false;
  const codePreviewActive = Boolean(
    selectedPath &&
      !changedMode &&
      preview &&
      !(loadingPreview && !preview) &&
      !previewErr &&
      !preview.err &&
      !preview.kind &&
      !preview.binary &&
      !isMarkdown,
  );
  // The preview body must keep its flex-column layout while a code file is
  // loading. Switching it to the padded block layout mid-load and back again
  // makes WebKit leave stale gaps between rows once the viewer mounts.
  const codePreviewLayoutActive = Boolean(
    selectedFilePath &&
      !changedMode &&
      !isMarkdown &&
      !previewErr &&
      !preview?.err &&
      !preview?.kind &&
      !preview?.binary,
  );
  const openCodeSearch = () => {
    if (!codePreviewActive || !selectedPath) return;
    setCodeSearchRequestPath(selectedPath);
    setCodeSearchRequestPending(true);
  };
  const treeBlankMenuItems: ContextMenuItem[] = [
    {
      key: "refresh-tree",
      icon: <RefreshCw size={13} />,
      label: t(viewMode === "changed" ? "workspace.refreshChanges" : "workspace.refreshTree"),
      onSelect: refreshWorkspaceList,
    },
  ];

  return (
    <MarkdownImageTabContext.Provider value={workspaceTabId}>
    <aside
      ref={panelRef}
      className={`workspace-panel${embeddedDockMode ? " workspace-panel--embedded" : ""}${changedMode ? " workspace-panel--detail-only" : ""}${changedMode && !selectedPath ? " workspace-panel--changed-overview" : ""}${previewVisible && actualTreeVisible ? " workspace-panel--split-preview" : ""}${actualTreeVisible ? "" : " workspace-panel--tree-hidden"}${previewVisible ? "" : " workspace-panel--preview-hidden"}${treeResizing ? " workspace-panel--tree-resizing" : ""}`}
      aria-label={t("workspace.title")}
      style={panelStyle}
      onKeyDownCapture={(event) => {
        if (
          codePreviewActive
          && (event.ctrlKey || event.metaKey)
          && event.key.toLowerCase() === "f"
        ) {
          event.preventDefault();
          event.stopPropagation();
          openCodeSearch();
        }
      }}
    >
      {previewVisible && <section className="workspace-preview">
        <header className="workspace-preview__head">
          <div className="workspace-current-file" aria-label={t("workspace.currentFile")}>
            <div className="workspace-current-file__text" onCopy={copyCurrentFileRelativePath}>
              {!selectedPath && (
                <span className="workspace-current-file__name">{previewTitle}</span>
              )}
              <WorkspacePathBreadcrumbs crumbs={previewSubtitleCrumbs} />
              <WorkspacePathBreadcrumbs crumbs={previewCrumbs} />
            </div>
            <Tooltip label={t("workspace.recentFiles")}>
              <button
                ref={recentAnchorRef}
                className={`workspace-iconbtn workspace-current-file__recent${recentOpen ? " workspace-current-file__recent--open" : ""}`}
                type="button"
                aria-label={t("workspace.recentFiles")}
                aria-expanded={recentOpen}
                onClick={() => setRecentOpen((open) => !open)}
              >
                <History size={15} />
              </button>
            </Tooltip>
          </div>

          <div className="workspace-preview__window-actions">
            {codePreviewActive && (
              <Tooltip label={t("workspace.searchPlaceholder")}>
                <button
                  className="workspace-iconbtn"
                  type="button"
                  aria-label={t("workspace.searchPlaceholder")}
                  aria-keyshortcuts="Control+F Meta+F"
                  onClick={openCodeSearch}
                >
                  <Search size={15} />
                </button>
              </Tooltip>
            )}
            <Tooltip label={maximized ? t("workspace.restore") : t("workspace.maximize")}>
              <button className="workspace-iconbtn" onClick={onToggleMaximized}>
                {maximized ? <Minimize2 size={15} /> : <Maximize2 size={15} />}
              </button>
            </Tooltip>
            {previewVisible && !changedMode && (
              <Tooltip label={actualTreeVisible ? t("workspace.hideTree") : t("workspace.showTree")}>
                <button
                  className={`workspace-iconbtn${actualTreeVisible ? " workspace-iconbtn--on" : ""}`}
                  type="button"
                  aria-label={actualTreeVisible ? t("workspace.hideTree") : t("workspace.showTree")}
                  aria-pressed={actualTreeVisible}
                  onClick={toggleTreeRail}
                >
                  <FolderTree size={15} />
                </button>
              </Tooltip>
            )}
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
                    setSelectedFilePath(path);
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

        <div
          className={`workspace-preview__body${codePreviewLayoutActive ? " workspace-preview__body--code" : ""}`}
          ref={previewBodyRef}
          onContextMenu={openSelectionMenu}
          onMouseUp={showSelectionToolbar}
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
                      setSelectedChangePath(null);
                      setExpandedCommit(null);
                      setCommitDetail(null);
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
              {visibleCompletionSummary && <WorkspaceTurnVerification ref={verificationSummaryRef} summary={visibleCompletionSummary} qualityFloor={qualityFloor} />}
              {workspaceGitWarning && (
                <div className="workspace-note workspace-note--warning" role="status">
                  {workspaceGitWarning}
                </div>
              )}
              {overviewResourceStatus && (
                <div className={`workspace-resource-status${overviewResourceStatus.error ? " workspace-resource-status--error" : ""}`} role={overviewResourceStatus.error ? "alert" : "status"}>
                  {overviewResourceStatus.text}
                </div>
              )}
              {groupedChangesLayout ? (
                <>
                  {sessionChanges.length > 0 && renderChangeScope(t("context.sessionChanges"), sessionChanges)}
                  {gitWorkingChanges.length > 0 && renderChangeScope(t("workspace.workingChanges"), gitWorkingChanges)}
                  {!loadingWorkspaceChanges && !workspaceChangesErr && !hasFileChanges && !workspaceGitWarning && (
                    <div className="workspace-empty">{t("context.noChanges")}</div>
                  )}
                  {loadingWorkspaceChanges && !workspaceChanges && (
                    <div className="workspace-empty">{t("workspace.loadingChanges")}</div>
                  )}
                  {workspaceChangesErr && !workspaceChanges && (
                    <div className="workspace-empty workspace-empty--error">{t("workspace.changesUnavailable")}: {workspaceChangesErr}</div>
                  )}
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
                      {commitHistoryOpen && (loadingHistory && gitHistory.length === 0 ? (
                        <div className="workspace-empty">{t("workspace.loading")}</div>
                      ) : gitHistoryErr && gitHistory.length === 0 ? (
                        <div className="workspace-empty workspace-empty--error">{t("workspace.historyUnavailable")}: {gitHistoryErr}</div>
                      ) : gitHistory.length === 0 ? (
                        <div className="workspace-empty">{t("workspace.noCommitHistory")}</div>
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
                      ))}
                    </section>
                </>
              ) : (
                <>
                  {sessionChanges.length > 0 && renderChangeScope(t("workspace.changedTab"), sessionChanges)}
                  {gitWorkingChanges.length > 0 && renderChangeScope(t("workspace.workingChanges"), gitWorkingChanges)}
                  {loadingHistory && gitHistory.length === 0 ? (
                    <div className="workspace-empty">{t("workspace.loading")}</div>
                  ) : gitHistoryErr && gitHistory.length === 0 ? (
                    <div className="workspace-empty workspace-empty--error">{t("workspace.historyUnavailable")}: {gitHistoryErr}</div>
                  ) : gitHistory.length === 0 && !hasFileChanges ? (
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
              <section className="workspace-current-change">
                <header className="workspace-current-change__head">
                  <div>
                    <strong>{t("workspace.currentChanges")}</strong>
                    {changeDetail?.source && (
                      <span>{t(changeDetail.source === "git" ? "workspace.currentChangesSourceGit" : "workspace.currentChangesSourceSession")}</span>
                    )}
                  </div>
                  {(changeDetail?.added || changeDetail?.removed) ? (
                    <small>
                      <span className="workspace-current-change__added">+{changeDetail.added ?? 0}</span>
                      <span className="workspace-current-change__removed">-{changeDetail.removed ?? 0}</span>
                    </small>
                  ) : null}
                </header>
                <div className="workspace-current-change__body">
                  {loadingChangeDetail && changeDetail && <div className="workspace-resource-status" role="status">{t("workspace.loading")}</div>}
                  {changeDetailErr && changeDetail && <div className="workspace-resource-status workspace-resource-status--error">{t("workspace.changeDetailUnavailable")}: {changeDetailErr}</div>}
                  {loadingChangeDetail && !changeDetail ? (
                    <div className="workspace-empty">{t("workspace.loading")}</div>
                  ) : changeDetailErr && !changeDetail ? (
                    <div className="workspace-empty workspace-empty--error">{t("workspace.changeDetailUnavailable")}: {changeDetailErr}</div>
                  ) : changeDetail?.truncated ? (
                    <div className="workspace-empty">{t("workspace.changeDetailTooLarge")}</div>
                  ) : changeDetail?.binary ? (
                    <div className="workspace-empty">{t("workspace.binaryChange")}</div>
                  ) : changeDetail?.diff ? (
                    changeDetail.diff.includes("@@") ? (
                      <DiffView diff={changeDetail.diff} language={languageFor(selectedPath)} />
                    ) : (
                      <CodeViewer value={changeDetail.diff} language="diff" />
                    )
                  ) : (
                    <div className="workspace-empty">{t("workspace.noCurrentDiff")}</div>
                  )}
                </div>
              </section>
              <section className={`workspace-commit-history${commitHistoryOpen ? " workspace-commit-history--open" : ""}`}>
                <button
                  className="workspace-commit-history__toggle"
                  type="button"
                  aria-expanded={commitHistoryOpen}
                  onClick={() => {
                    setCommitHistoryOpen((value) => !value);
                    setExpandedCommit(null);
                  }}
                >
                  {commitHistoryOpen ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                  <span>{t("workspace.commitHistory")}</span>
                  <small>{loadingHistory && gitHistory.length === 0 ? t("workspace.loading") : t("workspace.commitHistoryMeta", { count: gitHistory.length })}</small>
                </button>
                {commitHistoryOpen && (
                  loadingHistory && gitHistory.length === 0 ? (
                    <div className="workspace-empty">{t("workspace.loading")}</div>
                  ) : gitHistoryErr && gitHistory.length === 0 ? (
                    <div className="workspace-empty workspace-empty--error">{t("workspace.historyUnavailable")}: {gitHistoryErr}</div>
                  ) : gitHistory.length === 0 ? (
                    <div className="workspace-empty">{t("workspace.noCommitHistory")}</div>
                  ) : (
                    <div className="workspace-git-history__list">
                      {gitHistory.map((commit) => (
                        <div key={commit.hash} className={`workspace-git-history__item${expandedCommit === commit.hash ? " workspace-git-history__item--expanded" : ""}`}>
                          <button className="workspace-git-history__head" onClick={() => void toggleCommit(commit.hash)}>
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
                                <div className="workspace-empty">{t("workspace.noCommitDetail")}</div>
                              )}
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  )
                )}
              </section>
            </div>
          ) : !selectedPath ? (
            <div className="workspace-empty">{t("workspace.pickFile")}</div>
          ) : loadingPreview && !preview ? (
            <div className="workspace-empty">{t("workspace.loading")}</div>
          ) : preview?.err || (previewErr && !preview) ? (
            <div className="workspace-empty workspace-empty--error">
              {/no such file|not found|enoent/i.test(previewErr || preview?.err || "") ? t("workspace.fileDeleted") : (previewErr || preview?.err)}
            </div>
          ) : preview?.kind ? (
            <WorkspaceMediaPreview preview={preview} />
          ) : preview?.binary ? (
            <div className="workspace-empty">{t("workspace.binary")}</div>
          ) : preview ? (
            <>
              {loadingPreview && <div className="workspace-resource-status" role="status">{t("workspace.loading")}</div>}
              {previewErr && <div className="workspace-resource-status workspace-resource-status--error">{previewErr}</div>}
              {preview.truncated && <div className="workspace-note">{t("workspace.truncated")}</div>}
              {isMarkdown ? (
                <Markdown text={preview.body} />
              ) : (
                <CodeViewer
                  value={preview.body || " "}
                  language={languageFor(selectedPath)}
                  scrollMode="expand"
                  sourceSize={preview.size}
                  showLineNumbers
                  searchRequestPending={codeSearchRequestPending && codeSearchRequestPath === selectedPath}
                  onSearchRequestConsumed={consumeCodeSearchRequest}
                />
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
                  }}
                >
                  <GitBranch size={13} />
                  {t("workspace.changedTab")}
                </button>
              </div>
            )}
            <Tooltip label={t("workspace.refreshChanges")}>
              <button
                className="workspace-iconbtn"
                type="button"
                aria-label={t("workspace.refreshChanges")}
                aria-busy={loadingPreview || loadingHistory}
                onClick={() => {
                  refreshWorkspaceList();
                  void refreshSelected();
                }}
              >
                <RefreshCw size={14} />
              </button>
            </Tooltip>
            {workspaceRefresh.watchState !== "active" && (
              <span
                className="workspace-watch-status"
                role="status"
                title={t(workspaceRefresh.watchState === "degraded" ? "workspace.watchDegraded" : "workspace.watchUnavailable")}
                aria-label={t(workspaceRefresh.watchState === "degraded" ? "workspace.watchDegraded" : "workspace.watchUnavailable")}
              >
                •
              </span>
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
          onScroll={onWorkspaceTreeScroll}
          onContextMenu={openTreeBlankMenu}
          style={{
            height: "100%",
            overflow: "auto",
            position: "relative",
          }}
        >
          {treeRows.length > 0 ? (
            <div
              ref={virtualizer.containerRef}
              className="workspace-tree__sizer"
              style={{
                width: "100%",
                position: "relative",
              }}
            >
              {virtualTreeItems.map((row) => {
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
                    }}
                  >
                    <WorkspaceTreeRow
                      row={item}
                      onActivate={activateTreeRow}
                      onDragStart={startTreeDrag}
                      onContextMenu={openTreeMenu}
                    />
                  </div>
                );
              })}
            </div>
          ) : tabReady ? (
            <div className="workspace-empty">{t("workspace.emptyTree")}</div>
          ) : (
            <div className="workspace-empty" role="status">{t("workspace.loading")}</div>
          )}
        </div>
      </section>
      {treeMenu && (
        <WorkspaceTreeMenu
          target={treeMenu}
          workspaceTabId={workspaceTabId}
          isScopeCurrent={() => currentWorkspaceScopeKeyRef.current === workspaceScopeKey}
          onClose={() => setTreeMenu(null)}
          onOpenInTerminal={onOpenInTerminal}
          onAddReference={addTreeReferenceToChat}
          onAddFile={() => void addTreeFileToChat()}
        />
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
    </MarkdownImageTabContext.Provider>
  );
}
