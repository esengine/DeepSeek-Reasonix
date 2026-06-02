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
  Columns2,
  FileText,
  Folder,
  GitBranch,
  Maximize2,
  MessageSquarePlus,
  Minus,
  Minimize2,
  PanelRightClose,
  Plus,
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
import { FloatingMenu, FloatingMenuItems } from "./FloatingMenu";
import { Markdown } from "./Markdown";
import { Tooltip } from "./Tooltip";

const WORKSPACE_TREE_MIN_WIDTH = 220;
const WORKSPACE_TREE_DEFAULT_WIDTH = WORKSPACE_TREE_MIN_WIDTH;
const WORKSPACE_TREE_MAX_WIDTH = 420;
const WORKSPACE_PREVIEW_MIN_WIDTH = 420;
const WORKSPACE_CONTEXT_MENU_FILE_HEIGHT = 92;
const WORKSPACE_CONTEXT_MENU_REF_HEIGHT = 48;

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
  changesRefreshKey,
}: {
  open: boolean;
  cwd?: string;
  maximized: boolean;
  panelWidth?: number;
  onClose: () => void;
  onToggleMaximized: () => void;
  onPreviewModeChange?: (active: boolean) => void;
  onAddToChat?: (text: string) => void;
  changesRefreshKey?: number;
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
  const [viewMode, setViewMode] = useState<"files" | "changed">("files");
  const [changes, setChanges] = useState<WorkspaceChangesView | null>(null);
  const [loadingChanges, setLoadingChanges] = useState(false);
  const [selectionMenu, setSelectionMenu] = useState<{ x: number; y: number; text: string; path: string } | null>(null);
  const [treeMenu, setTreeMenu] = useState<{ x: number; y: number; path: string; isDir: boolean } | null>(null);
  const changesRequestRef = useRef(0);
  const [filter, setFilter] = useState("");
  const [treeVisible, setTreeVisible] = useState(true);
  const [treeWidth, setTreeWidth] = useState(loadWorkspaceTreeWidth);
  const [treeResizing, setTreeResizing] = useState(false);

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

  const selectFile = useCallback(
    (path: string) => {
      setSelectedPath(path);
      setFilter("");
      setOpenTabs((tabs) => (tabs.includes(path) ? tabs : [...tabs, path]));
      const dirs = parentDirs(path);
      setOpenDirs((prev) => new Set([...Array.from(prev), ...dirs]));
      dirs.forEach((dir) => {
        if (!entriesByDir[dir]) void loadDir(dir);
      });
    },
    [entriesByDir, loadDir],
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
    void loadDir("");
  }, [cwd, loadDir, open]);

  useEffect(() => {
    if (!open) return;
    void loadChanges();
  }, [changesRefreshKey, cwd, loadChanges, open]);

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

  const openPickerTab = () => {
    setSelectedPath(null);
    setPreview(null);
    setFilter("");
    setSelectionMenu(null);
    setTreeMenu(null);
    setTreeVisible(true);
    requestAnimationFrame(() => filterRef.current?.focus());
  };

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
      }
      return next;
    });
  };

  const breadcrumbDirs = selectedPath ? parentDirs(selectedPath) : [""];
  const pathParts = selectedPath?.split("/").filter(Boolean) ?? [];
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

  const effectiveTreeWidth = useMemo(() => clampWorkspaceTreeWidth(treeWidth, panelWidth), [panelWidth, treeWidth]);
  const previewVisible = openTabs.length > 0 || selectedPath !== null;
  const previewModeActive = open && previewVisible;

  const panelStyle = useMemo(
    () => ({ "--workspace-tree-width": `${effectiveTreeWidth}px` }) as CSSProperties,
    [effectiveTreeWidth],
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
        nextWidth = clampWorkspaceTreeWidth(rect.right - moveEvent.clientX, rect.width);
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
        setSavedTreeWidth(effectiveTreeWidth + (event.key === "ArrowLeft" ? 16 : -16));
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
    if (!selectedPath || loadingPreview || preview?.err || preview?.binary) return;
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
    setSelectionMenu(null);
    setTreeMenu({ x: event.clientX, y: event.clientY, path, isDir });
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
      if (file.err || file.binary) {
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
        <Tooltip key={path} label={path} fill>
          <button
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
        </Tooltip>
      );
      if (!entry.isDir || !isOpen) return [row];
      return [row, ...renderRows(path, depth + 1)];
    });
  };

  const isMarkdown = selectedPath?.toLowerCase().endsWith(".md") ?? false;

  return (
    <aside
      ref={panelRef}
      className={`workspace-panel${treeVisible ? "" : " workspace-panel--tree-hidden"}${previewVisible ? "" : " workspace-panel--preview-hidden"}${treeResizing ? " workspace-panel--tree-resizing" : ""}`}
      aria-label={t("workspace.title")}
      style={panelStyle}
    >
      {previewVisible && <section className="workspace-preview">
        <header className="workspace-preview__head">
          <div className="workspace-tabs">
            {openTabs.map((tab) => (
              <Tooltip key={tab} label={tab}>
                <button
                  className={`workspace-tab${selectedPath === tab ? " workspace-tab--active" : ""}`}
                  onClick={() => setSelectedPath(tab)}
                >
                  <FileText size={14} className="workspace-tab__icon" />
                  <span className="workspace-tab__name">{basename(tab)}</span>
                  <span
                    className="workspace-tab__close"
                    role="button"
                    tabIndex={0}
                    aria-label={t("workspace.closeTab")}
                    onClick={(e) => {
                      e.stopPropagation();
                      closeTab(tab);
                    }}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        e.stopPropagation();
                        closeTab(tab);
                      }
                    }}
                  >
                    <X size={12} />
                  </span>
                </button>
              </Tooltip>
            ))}
            <Tooltip label={t("workspace.newTab")}>
              <button className="workspace-tab workspace-tab--new" onClick={openPickerTab}>
                <Plus size={14} />
              </button>
            </Tooltip>
          </div>

          <div className="workspace-preview__window-actions">
            <Tooltip label={maximized ? t("workspace.restore") : t("workspace.maximize")}>
              <button className="workspace-iconbtn" onClick={onToggleMaximized}>
                {maximized ? <Minimize2 size={15} /> : <Maximize2 size={15} />}
              </button>
            </Tooltip>
            <Tooltip label={t("workspace.minimize")}>
              <button className="workspace-iconbtn" onClick={onClose}>
                <Minus size={15} />
              </button>
            </Tooltip>
            <Tooltip label={treeVisible ? t("workspace.hideTree") : t("workspace.showTree")}>
              <button
                className="workspace-iconbtn workspace-iconbtn--on"
                onClick={() => setTreeVisible((value) => !value)}
              >
                {treeVisible ? <PanelRightClose size={15} /> : <Columns2 size={15} />}
              </button>
            </Tooltip>
          </div>
        </header>

        <div className="workspace-preview__meta">
          <Tooltip label={cwd}>
            <button
              className="workspace-crumb"
              onClick={() => {
                setFilter("");
                setTreeVisible(true);
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

      {treeVisible && previewVisible && (
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
        <div className="workspace-files__tools">
          <Tooltip label={previewVisible ? t("workspace.hideTree") : t("workspace.close")}>
            <button
              className="workspace-iconbtn workspace-iconbtn--on"
              onClick={hideTreeOrClosePanel}
            >
              <PanelRightClose size={15} />
            </button>
          </Tooltip>
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
          <Tooltip label={t("workspace.refreshChanges")}>
            <button className="workspace-iconbtn" onClick={() => void loadChanges()}>
              <RefreshCw size={14} />
            </button>
          </Tooltip>
        </div>

        <div className="workspace-search">
          <Search size={14} />
          <input ref={filterRef} value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={t("workspace.filter")} />
        </div>
        {viewMode === "changed" && changes && !changes.gitAvailable && changes.gitErr && (
          <div className="workspace-note workspace-note--compact">{t("workspace.gitUnavailable")}</div>
        )}
        <div className="workspace-tree">
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
    </aside>
  );
}
