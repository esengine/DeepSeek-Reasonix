import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, PointerEvent as ReactPointerEvent } from "react";
import {
  ChevronDown,
  ChevronRight,
  Columns2,
  Copy,
  ExternalLink,
  FileText,
  Folder,
  FolderOpen,
  Maximize2,
  Minus,
  Minimize2,
  MoreHorizontal,
  PanelRightClose,
  Plus,
  RefreshCw,
  Search,
  X,
} from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { loadLayoutSize, saveLayoutSize } from "../lib/layoutPreferences";
import type { DirEntry, FilePreview } from "../lib/types";
import { CodeViewer } from "./CodeViewer";
import { Markdown } from "./Markdown";

const WORKSPACE_TREE_DEFAULT_WIDTH = 280;
const WORKSPACE_TREE_MIN_WIDTH = 220;
const WORKSPACE_TREE_MAX_WIDTH = 420;
const WORKSPACE_PREVIEW_MIN_WIDTH = 360;

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
}: {
  open: boolean;
  cwd?: string;
  maximized: boolean;
  panelWidth?: number;
  onClose: () => void;
  onToggleMaximized: () => void;
}) {
  const t = useT();
  const panelRef = useRef<HTMLElement>(null);
  const filterRef = useRef<HTMLInputElement>(null);
  const [entriesByDir, setEntriesByDir] = useState<Record<string, DirEntry[]>>({});
  const [openDirs, setOpenDirs] = useState<Set<string>>(() => new Set([""]));
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [openTabs, setOpenTabs] = useState<string[]>([]);
  const [preview, setPreview] = useState<FilePreview | null>(null);
  const [loadingPreview, setLoadingPreview] = useState(false);
  const [filter, setFilter] = useState("");
  const [treeVisible, setTreeVisible] = useState(true);
  const [treeWidth, setTreeWidth] = useState(loadWorkspaceTreeWidth);
  const [treeResizing, setTreeResizing] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);

  const loadDir = useCallback(async (dir: string) => {
    const entries = await app.ListDir(dir).catch(() => []);
    setEntriesByDir((prev) => ({ ...prev, [dir]: entries ?? [] }));
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
    setFilter("");
    setTreeVisible(true);
    void loadDir("");
  }, [cwd, loadDir, open]);

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

  const refreshTree = useCallback(() => {
    Object.keys(entriesByDir).forEach((dir) => void loadDir(dir));
    refreshSelected();
  }, [entriesByDir, loadDir, refreshSelected]);

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
      }
      return next;
    });
  };

  const revealSelected = () => {
    if (!selectedPath) return;
    void app.RevealWorkspacePath(selectedPath);
  };

  const openSelectedExternal = () => {
    if (!selectedPath) return;
    void app.OpenWorkspacePath(selectedPath);
  };

  const copySelectedPath = () => {
    if (!selectedPath) return;
    void navigator.clipboard?.writeText(selectedPath);
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

  const effectiveTreeWidth = useMemo(() => clampWorkspaceTreeWidth(treeWidth, panelWidth), [panelWidth, treeWidth]);
  const previewVisible = openTabs.length > 0 || selectedPath !== null;

  const panelStyle = useMemo(
    () => ({ "--workspace-tree-width": `${effectiveTreeWidth}px` }) as CSSProperties,
    [effectiveTreeWidth],
  );

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

  const renderRows = (dir: string, depth: number): JSX.Element[] => {
    const entries = entriesByDir[dir] ?? [];
    return entries.flatMap((entry) => {
      const path = entryPath(dir, entry);
      const isOpen = openDirs.has(path);
      const active = selectedPath === path;
      const row = (
        <button
          className={`workspace-tree__row${active ? " workspace-tree__row--active" : ""}`}
          key={path}
          onClick={() => (entry.isDir ? toggleDir(path) : selectFile(path))}
          title={path}
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
              <button
                className={`workspace-tab${selectedPath === tab ? " workspace-tab--active" : ""}`}
                key={tab}
                onClick={() => setSelectedPath(tab)}
                title={tab}
              >
                <FileText size={14} />
                <span>{basename(tab)}</span>
                <span
                  className="workspace-tab__close"
                  role="button"
                  tabIndex={0}
                  title={t("workspace.closeTab")}
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
            ))}
            <button className="workspace-tab workspace-tab--new" onClick={openPickerTab} title={t("workspace.newTab")}>
              <Plus size={14} />
            </button>
          </div>

          <div className="workspace-preview__window-actions">
            <button className="workspace-iconbtn" onClick={onToggleMaximized} title={maximized ? t("workspace.restore") : t("workspace.maximize")}>
              {maximized ? <Minimize2 size={15} /> : <Maximize2 size={15} />}
            </button>
            <button className="workspace-iconbtn" onClick={onClose} title={t("workspace.minimize")}>
              <Minus size={15} />
            </button>
            <button
              className="workspace-iconbtn workspace-iconbtn--on"
              onClick={() => setTreeVisible((value) => !value)}
              title={treeVisible ? t("workspace.hideTree") : t("workspace.showTree")}
            >
              {treeVisible ? <PanelRightClose size={15} /> : <Columns2 size={15} />}
            </button>
          </div>
        </header>

        <div className="workspace-preview__meta">
          <button
            className="workspace-crumb"
            onClick={() => {
              setFilter("");
              setTreeVisible(true);
              setOpenDirs((prev) => new Set([...Array.from(prev), ""]));
            }}
            title={cwd}
          >
            {shortCwd(cwd) || t("workspace.title")}
          </button>
          {pathParts.map((part, index) => {
            const isLast = index === pathParts.length - 1;
            const dir = pathParts.slice(0, index + 1).join("/") + "/";
            return (
              <span className="workspace-crumb-group" key={`${part}-${index}`}>
                <span>›</span>
                <button
                  className={`workspace-crumb${isLast ? " workspace-crumb--current" : ""}`}
                  onClick={() => {
                    if (isLast) return;
                    setTreeVisible(true);
                    setFilter("");
                    setOpenDirs((prev) => new Set([...Array.from(prev), ...breadcrumbDirs, dir]));
                    void loadDir(dir);
                  }}
                  title={isLast ? (selectedPath ?? undefined) : dir}
                >
                  {part}
                </button>
              </span>
            );
          })}
          {preview && preview.size > 0 && <span className="workspace-preview__size">{formatBytes(preview.size)}</span>}
        </div>

        <div className="workspace-preview__body">
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
          title={t("workspace.resizeTree")}
        />
      )}

      <section className="workspace-files">
        <div className="workspace-files__tools">
          <div className="workspace-menu-wrap">
            <button className="workspace-iconbtn" onClick={() => setMenuOpen((value) => !value)} title={t("workspace.more")}>
              <MoreHorizontal size={15} />
            </button>
            {menuOpen && (
              <div className="workspace-menu">
                <button
                  onClick={() => {
                    refreshTree();
                    setMenuOpen(false);
                  }}
                >
                  <RefreshCw size={13} />
                  {t("workspace.refresh")}
                </button>
                <button
                  onClick={() => {
                    copySelectedPath();
                    setMenuOpen(false);
                  }}
                  disabled={!selectedPath}
                >
                  <Copy size={13} />
                  {t("workspace.copyPath")}
                </button>
                <button
                  onClick={() => {
                    setOpenTabs(selectedPath ? [selectedPath] : []);
                    setMenuOpen(false);
                  }}
                >
                  <X size={13} />
                  {t("workspace.closeOtherTabs")}
                </button>
              </div>
            )}
          </div>
          <button className="workspace-iconbtn" onClick={openSelectedExternal} disabled={!selectedPath} title={t("workspace.openExternal")}>
            <ExternalLink size={15} />
          </button>
          <button className="workspace-iconbtn" onClick={revealSelected} disabled={!selectedPath} title={t("workspace.reveal")}>
            <FolderOpen size={15} />
          </button>
        </div>

        <div className="workspace-search">
          <Search size={14} />
          <input ref={filterRef} value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={t("workspace.filter")} />
        </div>
        <div className="workspace-tree">
          {flattened
            ? flattened.map(({ path, entry }) => {
                const cleanPath = path.replace(/\/$/, "");
                const dir = parentPath(path);
                return (
                  <button
                    className={`workspace-tree__row workspace-tree__row--search${selectedPath === path ? " workspace-tree__row--active" : ""}`}
                    key={path}
                    onClick={() => (entry.isDir ? toggleDir(path) : selectFile(path))}
                    title={cleanPath}
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
    </aside>
  );
}
