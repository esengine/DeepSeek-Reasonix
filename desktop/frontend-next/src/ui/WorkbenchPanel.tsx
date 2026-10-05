import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type MouseEvent } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { host } from "../port/host";
import type {
  AgentPort,
  BrowserTab,
  WorkspaceChange,
  WorkspaceFile,
} from "../port/port";
import { openPage, pageForEmptyColumn } from "./browser_open";
import { AgentBrowserPanel, ManualBrowserPanel, UNREAD_TABS } from "./BrowserPanel";
import { DiffView } from "./cards/DiffView";
import { StudioIcon } from "./StudioIcon";
import { LazyMarkdown } from "./LazyMarkdown";
import type { LocalRefs } from "./Markdown";
import { docRef } from "./docrefs";
import { useGlance } from "./glance";
import { useMarkdownPoll } from "./useMarkdownPoll";
import { pinToViewport } from "./place";
import { keyOf, labelOf, type Surface } from "./workbench_tabs";
import { useTabClose } from "./useTabClose";
import { BrowserTabMenu } from "./BrowserTabMenu";
import { WorkbenchExplorerHead } from "./WorkbenchExplorerHead";
import { WorkbenchFileBar, type FileMode } from "./WorkbenchFileBar";
import { useHtmlPreview } from "./useHtmlPreview";
import { setShowsHiddenFiles, showsHiddenFiles } from "../state/prefs";
import { useCommitCard } from "./CommitCard";

// The editor and its grammars load with the first file opened, not with Studio.
const CodeEditor = lazy(() => import("./CodeEditor"));

type TreeRow = {
  kind: "folder" | "file";
  path: string;
  name: string;
  depth: number;
};

/** Folders first at every level, with the tree still reading as a tree. Sorting
 *  flat paths as text interleaves `bin/` with `boot.test.exe`, because text is
 *  the only thing it compares. Segment by segment, whichever row is a directory
 *  where the two paths part wins there, and its children stay under it. */
function before(a: TreeRow, b: TreeRow): number {
  const x = a.path.split("/"), y = b.path.split("/");
  for (let i = 0; i < Math.min(x.length, y.length); i++) {
    if (x[i] === y[i]) continue;
    const dirs = [i < x.length - 1 || a.kind === "folder", i < y.length - 1 || b.kind === "folder"];
    return dirs[0] === dirs[1] ? x[i].localeCompare(y[i]) : dirs[0] ? -1 : 1;
  }
  return x.length - y.length;
}

function treeRows(
  files: string[],
  directories: string[],
  query: string,
  closed: Set<string>,
): TreeRow[] {
  const matches = query
    ? files.filter((path) =>
        path.toLocaleLowerCase().includes(query.toLocaleLowerCase()),
      )
    : files;
  const folders = new Set<string>(directories);
  for (const path of matches)
    for (let i = 1, bits = path.split("/"); i < bits.length; i++)
      folders.add(bits.slice(0, i).join("/"));
  const hidden = (path: string) =>
    [...closed].some(
      (folder) => path === folder || path.startsWith(`${folder}/`),
    );
  const rows: TreeRow[] = [...folders]
    .filter(
      (path) => ![...closed].some((parent) => path.startsWith(`${parent}/`)),
    )
    .map((path) => ({
      kind: "folder",
      path,
      name: path.split("/").at(-1)!,
      depth: path.split("/").length - 1,
    }));
  rows.push(
    ...matches
      .filter((path) => !hidden(path.split("/").slice(0, -1).join("/")))
      .map((path) => ({
        kind: "file" as const,
        path,
        name: path.split("/").at(-1)!,
        depth: path.split("/").length - 1,
      })),
  );
  return rows.sort(before);
}

export function WorkbenchPanel({
  port,
  tabs: allTabs,
  manual,
  shown,
  scheme,
  changes,
  onTreeChanged,
  running = false,
  wrote = 0,
  remote = false,
  onCloseManual,
  onSurfaces,
  onExternal,
}: {
  port: AgentPort;
  tabs: BrowserTab[];
  manual: boolean;
  shown: boolean;
  scheme: "light" | "dark";
  changes: WorkspaceChange[];
  /** Called after the panel committed, so the list is read again. */
  onTreeChanged?: () => void;
  /** Whether a turn is in flight: one that settles can have written files git
   *  does not list, so the change set alone does not say the tree moved. */
  running?: boolean;
  /** Finished calls that may have written, so the tree follows a turn mid-way. */
  wrote?: number;
  /** The workspace is on another machine, so its paths mean nothing here. */
  remote?: boolean;
  onCloseManual: () => void;
  // How many surfaces the strip holds, for the pane's own tab to count.
  onSurfaces: (n: number) => void;
  onExternal: (url: string) => void;
}) {
  const preview = useHtmlPreview(port, allTabs, remote);
  const tabs = preview.tabs;
  const [files, setFiles] = useState<string[]>([]),
    [directories, setDirectories] = useState<string[]>([]),
    [openFiles, setOpenFiles] = useState<string[]>([]);
  // Docked, the body is too narrow for two columns, so the explorer is hidden
  // — and the workbench is always docked now, which left the files with no way
  // in at all. The toggle is that way in, at any width.
  const [showFiles, setShowFiles] = useState(false);
  const commit = useCommitCard(port, () => onTreeChanged?.());
  const [query, setQuery] = useState(""),
    [selected, setSelected] = useState("");
  // Docked, the file list hides the canvas a page is drawn in, so choosing any
  // surface steps the list aside.
  const pick = useCallback((key: string) => {
    setShowFiles(false);
    setFailed("");
    setSelected(key);
  }, []);
  const [dismissed, setDismissed] = useState<Set<string>>(() => new Set()),
    [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
  // The explorer's own failure, drawn in the explorer: the file view's error
  // sits in a canvas the docked file list hides.
  const [listFailed, setListFailed] = useState("");
  const [hidden, setHidden] = useState(showsHiddenFiles);
  const openFolders = useRef<string[]>([]);
  openFolders.current = directories.filter((path) => !collapsed.has(path));
  const [browsers, setBrowsers] = useState<string[]>([]),
    [hosts, setHosts] = useState<Record<string, string>>({});
  const minted = useRef(0);
  const [mode, setMode] = useState<FileMode>("file");
  const [file, setFile] = useState<WorkspaceFile | null>(null),
    [draft, setDraft] = useState(""),
    [diff, setDiff] = useState("");
  const [busy, setBusy] = useState(false),
    [failed, setFailed] = useState("");
  // Showing a file where it lives needs a shell on the kernel's own machine.
  const revealable = !remote && port.revealsFiles();
  const [platform, setPlatform] = useState("");
  useEffect(() => void host().describe().then((h) => setPlatform(h.platform)), []);
  const [menu, setMenu] = useState<{ path: string; x: number; y: number } | null>(null);
  useEffect(() => {
    if (!menu) return;
    // Enter and Space are the focused item being chosen, not the menu dismissed.
    const shut = (e: Event) => {
      if (e instanceof KeyboardEvent && (e.key === "Enter" || e.key === " ")) return;
      setMenu(null);
    };
    addEventListener("click", shut);
    addEventListener("keydown", shut);
    addEventListener("blur", shut);
    return () => {
      removeEventListener("click", shut);
      removeEventListener("keydown", shut);
      removeEventListener("blur", shut);
    };
  }, [menu]);
  const reveal = (path: string) => {
    setMenu(null);
    port.revealInFileManager(path).then(() => setListFailed(""), (e) => setListFailed(reason(e)));
  };
  // The system menu stays wherever there is text to copy: this one is a shortcut.
  const offerReveal = (e: MouseEvent<HTMLElement>, path: string) => {
    if (!revealable || document.getSelection()?.isCollapsed === false) return;
    e.preventDefault();
    // The context-menu key reports no pointer, so the row itself is the anchor.
    const row = e.currentTarget.getBoundingClientRect();
    const keyed = e.clientX === 0 && e.clientY === 0;
    setMenu({ path, x: keyed ? row.left + 24 : e.clientX, y: keyed ? row.bottom : e.clientY });
  };
  const changeKey = changes
    .map((change) => `${change.status}:${change.path}`)
    .join("\n");
  // The kernel reads the disk on every listing and keeps nothing, so the tree is
  // only as stale as the last time it asked. A delete in another application
  // tells nobody here; looking back at the window is when it can have happened.
  const glance = useGlance();
  useEffect(() => {
    if (!shown) return;
    let live = true;
    void (async () => {
      try {
        const top = await port.workspaceFiles("", query, hidden);
        let files = top.files;
        let dirs = top.directories;
        // A reload is the tree read again, not the reader's place in it lost:
        // the folders they had open are read again, parents before children,
        // and one that is gone or unreadable now simply closes.
        const open = new Set<string>();
        if (!query) {
          const depth = (path: string) => path.split("/").length;
          for (const path of [...openFolders.current].sort((a, b) => depth(a) - depth(b))) {
            if (!dirs.includes(path)) continue;
            try {
              const inner = await port.workspaceFiles(path, "", hidden);
              files = [...files, ...inner.files];
              dirs = [...dirs, ...inner.directories];
              open.add(path);
            } catch {
              // Left closed; the next click says why.
            }
          }
        }
        if (!live) return;
        setFiles([...new Set(files)]);
        setDirectories([...new Set(dirs)]);
        setCollapsed(query ? new Set() : new Set(dirs.filter((path) => !open.has(path))));
        setListFailed("");
      } catch (e) {
        if (live) setListFailed(reason(e));
      }
    })();
    return () => {
      live = false;
    };
  }, [port, shown, changeKey, query, hidden, glance, running, wrote]);
  // The button in the chrome says "show me the browser", not "show me this one
  // browser". When the agent has a page, that page is the browser; the start
  // page is only for an empty column, and steps aside unused once the agent
  // opens something. Closing the panel is not closing the pages: they come
  // back with it, and only a tab's own × takes one away.
  const agentPage = tabs.find((tab) => tab.active) ?? tabs[0];
  const agentTarget = agentPage?.target ?? "";
  const agentAt = agentPage ? `${agentPage.target} ${agentPage.url}` : "";
  const visited = useRef(hosts);
  visited.current = hosts;
  const dropBlankStart = useCallback(() => setBrowsers((open) => open.filter((id) => !!visited.current[id])), []);
  const agentNow = useRef(agentTarget);
  agentNow.current = agentTarget;
  useEffect(() => {
    if (!manual) return;
    if (agentNow.current) {
      dropBlankStart();
      pick(`browser:${agentNow.current}`);
      return;
    }
    // A shell that draws views opens a view here too: a framed page is refused
    // by any site sending X-Frame-Options or frame-ancestors, as many do.
    if (host().drawsBrowserViews()) {
      pageForEmptyColumn(port)
        .then((tab) => pick(`browser:${tab.target}`))
        .catch((e) => setFailed(reason(e)));
      return;
    }
    setBrowsers((open) => (open.length ? open : ["b0"]));
    setSelected((at) => (at.startsWith("manual:") ? at : "manual:b0"));
  }, [manual, dropBlankStart, pick, port]);
  // A page that has just appeared is the one somebody wants to see, whoever
  // opened it: the agent's popup, or a link the person followed, which opens
  // beside the agent's tab without becoming the one it acts on.
  const known = useRef<Set<string> | null>(null);
  useEffect(() => {
    if (tabs === UNREAD_TABS) return;
    const ids = tabs.map((tab) => tab.target);
    if (known.current === null) {
      known.current = new Set(ids);
      return;
    }
    const fresh = ids.filter((id) => !known.current!.has(id));
    for (const id of ids) known.current.add(id);
    if (fresh.length) {
      dropBlankStart();
      pick(`browser:${fresh[fresh.length - 1]}`);
    }
  }, [tabs, dropBlankStart, pick]);
  // Where the agent is looking, followed as it moves: a new page, or the same
  // tab sent somewhere else.
  const lastAgent = useRef<string | null>(null);
  useEffect(() => {
    const before = lastAgent.current;
    lastAgent.current = agentAt;
    if (!before || !agentAt || before === agentAt) return;
    dropBlankStart();
    pick(`browser:${agentTarget}`);
  }, [agentAt, agentTarget, dropBlankStart, pick]);
  const noteHost = useCallback(
    (id: string, host: string) =>
      setHosts((v) => (v[id] === host ? v : { ...v, [id]: host })),
    [],
  );
  const surfaces = useMemo<Surface[]>(
    () =>
      [
        ...browsers.map((id) => ({ kind: "manual", id }) as const),
        ...tabs.map((tab) => ({ kind: "browser", tab }) as const),
        ...openFiles.map((path) => ({ kind: "file", path }) as const),
      ].filter((s) => !dismissed.has(keyOf(s))),
    [browsers, tabs, openFiles, dismissed],
  );
  const active =
    surfaces.find((s) => keyOf(s) === selected) ??
    surfaces.find((s) => s.kind === "browser" && s.tab.active) ??
    surfaces[0];
  // A document opens rendered: it is read far more often than edited, and the
  // source view is one click away.
  const filePath = active?.kind === "file" ? active.path : "";
  const readable = /\.(md|markdown|mdx)$/i.test(filePath);
  const previewable = !!filePath && preview.can(filePath);
  useEffect(() => {
    setMode((m) => {
      const kept = (m === "read" && !readable) || (m === "preview" && !previewable) ? "file" : m;
      return kept === "file" ? (readable ? "read" : previewable ? "preview" : "file") : kept;
    });
  }, [filePath, readable, previewable]);
  const previewTab = previewable ? preview.tabFor(filePath) : undefined;
  const { open: openPreview, sync: syncPreview } = preview;
  const revision = file?.path === filePath ? file.revision : undefined;
  useEffect(() => {
    if (mode !== "preview" || !previewable || !shown || previewTab || (!revision && !failed)) return;
    openPreview(filePath, revision).catch((e) => {
      setFailed(reason(e));
      setMode("file");
    });
  }, [mode, previewable, shown, previewTab, revision, failed, filePath, openPreview, tabs]);
  useEffect(() => {
    if (mode === "preview" && revision) syncPreview(filePath, revision);
  }, [mode, filePath, revision, syncPreview]);
  const rows = useMemo(
    () => treeRows(files, directories, query, collapsed),
    [files, directories, query, collapsed],
  );
  useEffect(() => onSurfaces(surfaces.length), [onSurfaces, surfaces.length]);
  // A strip wider than the column scrolls, and the tab being shown is always
  // brought into it: a selected tab nobody can see reads as a tab that closed.
  const strip = useRef<HTMLDivElement>(null);
  const activeKey = active ? keyOf(active) : "";
  useEffect(() => {
    strip.current?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [activeKey, surfaces.length]);
  const held = useRef({ file, draft });
  held.current = { file, draft };
  // Revisions are opaque, so only order says which answer is current: a read
  // counts only if no save or later read has been issued since it started.
  const issued = useRef(0);
  const openPath = active?.kind === "file" ? active.path : "";
  // The file is read again on the same occasions as the tree, because the agent
  // and other applications write it without telling this view. Unsaved edits
  // win: overwriting a draft with the disk would lose work nobody asked to drop.
  useEffect(() => {
    if (!openPath) return;
    const prior = held.current.file?.path === openPath ? held.current.file : null;
    if (prior && (!shown || held.current.draft !== prior.content)) return;
    let live = true;
    const ticket = ++issued.current;
    if (!prior) {
      setBusy(true);
      setFailed("");
    }
    Promise.all([
      port.workspaceFile(openPath),
      port
        .changeDiff(openPath)
        .catch(() => ({ path: openPath, diff: "", truncated: false })),
    ])
      .then(
        ([next, patch]) => {
          if (!live || ticket !== issued.current) return;
          const now = held.current;
          if (prior && (now.file?.path !== openPath || now.draft !== now.file.content)) return;
          setFailed("");
          if (!prior || now.file?.revision !== next.revision) {
            setFile(next);
            setDraft(next.content);
          }
          setDiff(patch.diff);
        },
        (e) => live && ticket === issued.current && setFailed(reason(e)),
      )
      .finally(() => live && !prior && setBusy(false));
    return () => {
      live = false;
    };
  }, [port, openPath, shown, changeKey, glance, running, wrote]);
  useMarkdownPoll({ port, filePath: shown && ((readable && mode === "read") || (previewable && mode === "preview")) ? filePath : "", held, issued, setFailed, setFile, setDraft });
  // Picking a file is asking to read it. Docked, the list and the file share one
  // column, so the list steps aside; side by side it stays where it is.
  const openFile = (path: string) => {
    setOpenFiles((v) => (v.includes(path) ? v : [...v, path]));
    setDismissed((v) => {
      const n = new Set(v);
      n.delete(`file:${path}`);
      return n;
    });
    pick(`file:${path}`);
  };
  // A rendered document's own links open beside it and its images load from the
  // tree. Held stable per document, so typing re-renders the text alone.
  const openRef = useRef(openFile);
  openRef.current = openFile;
  const docRefs = useMemo<LocalRefs>(
    () => ({
      link: (href) => {
        const to = docRef(filePath, href);
        return to ? () => openRef.current(to) : null;
      },
      image: (src) => {
        const at = docRef(filePath, src);
        return at ? port.workspaceImageURL(at) : null;
      },
    }),
    [filePath, port],
  );
  const toggleFolder = async (path: string) => {
    if (!collapsed.has(path)) {
      setCollapsed((v) => new Set(v).add(path));
      return;
    }
    try {
      const result = await port.workspaceFiles(path, "", hidden);
      setFiles((v) => [...new Set([...v, ...result.files])]);
      setDirectories((v) => [...new Set([...v, ...result.directories])]);
      setCollapsed((v) => {
        const n = new Set(v);
        n.delete(path);
        result.directories.forEach((dir) => n.add(dir));
        return n;
      });
      setListFailed("");
    } catch (e) {
      setListFailed(reason(e));
    }
  };
  // One browser: a tab opened here is a tab the agent can read and drive, and
  // it is drawn as a view rather than framed — which is the only way a site
  // that refuses to be framed opens at all. A shell with no views has no such
  // page to give, so there the iframe surface is still what there is.
  const addBrowser = async () => {
    if (!host().drawsBrowserViews()) {
      const id = `b${(minted.current += 1)}`;
      setBrowsers((v) => [...v, id]);
      pick(`manual:${id}`);
      return;
    }
    try {
      const tab = await openPage(port, "about:blank");
      pick(`browser:${tab.target}`);
    } catch (e) {
      setFailed(reason(e));
    }
  };
  const { close, closing, closeFailure } = useTabClose({ port, surfaces, selected, showFiles, setBrowsers, setOpenFiles, setDismissed, setSelected, onCloseManual, onCloseSelected: () => setFailed("") });
  const dismissTabMenu = useCallback(() => setTabMenu(null), []);
  const [tabMenu, setTabMenu] = useState<{ surface: Surface; x: number; y: number; anchor: HTMLElement } | null>(null);
  const save = async () => {
    if (!file || draft === file.content) return;
    setBusy(true);
    setFailed("");
    issued.current++;
    try {
      setFile(await port.saveWorkspaceFile({ ...file, content: draft }));
    } catch (e) {
      setFailed(reason(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="workbench" aria-label={t("工作台")}>
      <header className="workbench-tabs">
        <div
          className="workbench-tablist"
          role="tablist"
          ref={strip}
          onWheel={(e) => {
            if (e.deltaY && strip.current) strip.current.scrollLeft += e.deltaY;
          }}
        >
        {surfaces.map((surface) => (
          <div
            className="workbench-tab"
            key={keyOf(surface)}
            role="tab"
            aria-selected={surface === active}
            data-live={surface.kind === "browser" && surface.tab.active ? "" : undefined}
            data-action-contextmenu="workbench.browser-menu" data-target={keyOf(surface)}
            onContextMenu={(e) => {
              if (surface.kind === "file" || closing) return;
              e.preventDefault();
              const anchor = e.currentTarget.querySelector<HTMLElement>(".workbench-tab-pick")!;
              const rect = anchor.getBoundingClientRect();
              setTabMenu({ surface, x: e.clientX || rect.left, y: e.clientY || rect.bottom, anchor });
            }}
          >
            <button
              className="workbench-tab-pick"
              data-action="workbench.tab"
              data-target={keyOf(surface)}
              title={
                surface.kind === "browser"
                  ? `${surface.tab.active ? `${t("模型正在操作这个页面")}
` : ""}${surface.tab.url}`
                  : labelOf(surface, hosts)
              }
              onClick={() => pick(keyOf(surface))}
            >
              <StudioIcon name={surface.kind === "file" ? "file" : "globe"} />
              <span>{labelOf(surface, hosts)}</span>
            </button>
            <button
              className="workbench-tab-close"
              data-action="workbench.close"
              data-target={keyOf(surface)}
              aria-label={t("关闭 {name}", { name: labelOf(surface, hosts) })}
              title={t("关闭")}
              disabled={closing}
              onClick={() => void close([surface])}
            >
              <StudioIcon name="close" />
            </button>
          </div>
        ))}
        </div>
        <button
          className="workbench-files"
          data-action="workbench.files"
          aria-pressed={showFiles}
          aria-label={t("文件")}
          title={t("文件")}
          onClick={() => setShowFiles((on) => !on)}
        >
          <StudioIcon name="folder" />
        </button>
        <button
          className="workbench-new"
          data-action="workbench.new-browser"
          aria-label={t("新建浏览器标签")}
          title={t("新建浏览器标签")}
          onClick={() => void addBrowser()}
        >
          <StudioIcon name="plus" />
        </button>
      </header>
      {closeFailure && <div className="workbench-error workbench-close-error" role="alert">{closeFailure}</div>}
      <div className="workbench-body" data-files={showFiles ? "" : undefined}>
        <main className="workbench-canvas">
          {failed && active?.kind !== "file" && (
            <div className="workbench-error" role="alert">
              {failed}
            </div>
          )}
          {!active && (
            <div className="workbench-empty">
              <StudioIcon name="file" />
              <span>{t("从右侧选择文件，或打开浏览器")}</span>
            </div>
          )}
          {/* Every open browser stays mounted: a tab switch must not throw away
              where that page had got to. */}
          {browsers.map((id) => (
            <ManualBrowserPanel
              key={id}
              id={id}
              hidden={!(active?.kind === "manual" && active.id === id)}
              onAddress={noteHost}
              onExternal={onExternal}
              scheme={scheme}
            />
          ))}
          {active?.kind === "browser" && (
            <AgentBrowserPanel
              tabs={[active.tab]}
              shown={shown}
              showTabs={false}
            />
          )}
          {active?.kind === "file" && (
            <>
              <WorkbenchFileBar
                path={active.path}
                mode={mode}
                onMode={setMode}
                readable={readable}
                previewable={previewable}
                canSave={!busy && !!file && draft !== file.content}
                onSave={() => void save()}
              />
              {failed && !(mode === "preview" && previewTab) && (
                <div className="workbench-error" role="alert">
                  {failed}
                </div>
              )}
              {mode === "preview" ? (
                previewTab ? (
                  <AgentBrowserPanel tabs={[previewTab]} shown={shown} showTabs={false} />
                ) : (
                  <div className="workbench-empty">{t("正在读取…")}</div>
                )
              ) : busy && !file ? (
                <div className="workbench-empty">{t("正在读取…")}</div>
              ) : mode === "diff" ? (
                <div className="workbench-diff">
                  <DiffView path={active.path} diff={diff} />
                </div>
              ) : mode === "read" && file && file.path === active.path ? (
                <div className="workbench-read">
                  <LazyMarkdown text={draft} local={docRefs} />
                </div>
              ) : file && file.path === active.path ? (
                <Suspense fallback={<div className="workbench-empty">{t("正在读取…")}</div>}>
                  <CodeEditor
                    key={active.path}
                    path={active.path}
                    value={draft}
                    dark={scheme === "dark"}
                    onChange={setDraft}
                    onSave={() => void save()}
                  />
                </Suspense>
              ) : null}
            </>
          )}
        </main>
        <aside className="workbench-explorer">
          <WorkbenchExplorerHead
            port={port}
            count={files.length + directories.length}
            revealable={revealable}
            hidden={hidden}
            onHidden={(on) => {
              setShowsHiddenFiles(on);
              setHidden(on);
            }}
            onReveal={() => reveal("")}
          />
          <label className="workbench-search">
            <StudioIcon name="search" />
            <input
              aria-label={t("搜索文件")}
              data-action="workbench.search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t("搜索文件")}
            />
            {query && (
              <button data-action="workbench.clear-search" aria-label={t("清除搜索")} onClick={() => setQuery("")}>
                <StudioIcon name="close" />
              </button>
            )}
          </label>
          {listFailed && (
            <div className="workbench-error" role="alert">
              {listFailed}
            </div>
          )}
          {/* What differs, without opening anything. The tree marks a changed
              file only once its folder is expanded, so a change three levels
              down was invisible until someone went looking for it. */}
          {changes.length > 0 && (
            <div className="workbench-changes">
              <div className="workbench-explorer-head">
                <span>{t("改动")}</span>
                <small>{changes.length}</small>
                {onTreeChanged && commit.button}
              </div>
              {commit.card}
              {changes.map((item) => (
                <button
                  className="workbench-change-row"
                  data-action="workbench.diff"
                  data-target={item.path}
                  key={`c:${item.path}`}
                  title={item.path}
                  aria-current={active?.kind === "file" && active.path === item.path && mode === "diff" ? "page" : undefined}
                  onClick={() => {
                    openFile(item.path);
                    setMode("diff");
                  }}
                >
                  <em data-status={item.status}>{item.status || "M"}</em>
                  <span>{item.path.slice(item.path.lastIndexOf("/") + 1)}</span>
                  {/* Absent counts print nothing: a binary file did not change
                      by zero lines, git simply did not count it. */}
                  {item.insertions !== undefined && <i data-io="up">+{item.insertions}</i>}
                  {item.deletions !== undefined && <i data-io="down">-{item.deletions}</i>}
                </button>
              ))}
            </div>
          )}
          <div className="workbench-filelist">
            {rows.map((row) =>
              row.kind === "folder" ? (
                <button
                  className="workbench-tree-row"
                  data-action-click="workbench.folder"
                  data-target={row.path}
                  key={`d:${row.path}`}
                  style={{ paddingInlineStart: 8 + row.depth * 14 }}
                  aria-expanded={!collapsed.has(row.path)}
                  onClick={() => void toggleFolder(row.path)}
                  data-action-contextmenu="workbench.menu"
                  onContextMenu={(e) => offerReveal(e, row.path)}
                >
                  <StudioIcon
                    name={collapsed.has(row.path) ? "chevron" : "down"}
                  />
                  <StudioIcon name="folder" />
                  <span>{row.name}</span>
                </button>
              ) : (
                <button
                  className="workbench-tree-row"
                  data-action-click="workbench.file"
                  data-target={row.path}
                  key={row.path}
                  style={{ paddingInlineStart: 8 + row.depth * 14 }}
                  title={row.path}
                  aria-current={
                    active?.kind === "file" && active.path === row.path
                      ? "page"
                      : undefined
                  }
                  onClick={() => openFile(row.path)}
                  data-action-contextmenu="workbench.menu"
                  onContextMenu={(e) => offerReveal(e, row.path)}
                >
                  <span />
                  <StudioIcon name="file" />
                  <span>{row.name}</span>
                  {changes.find((item) => item.path === row.path) && (
                    <em>
                      {changes.find((item) => item.path === row.path)?.status ||
                        "M"}
                    </em>
                  )}
                </button>
              ),
            )}
            {!rows.length && (
              <p className="workbench-no-files">{t("没有匹配的文件")}</p>
            )}
          </div>
          {menu && createPortal(
            <div
              className="tabmenu"
              role="menu"
              ref={(el) => {
                if (el) pinToViewport(el, menu.x, menu.y);
              }}
              onClick={(e) => e.stopPropagation()}
            >
              <button role="menuitem" autoFocus data-action="workbench.reveal" data-target={menu.path} onClick={() => reveal(menu.path)}>
                {platform === "darwin" ? t("在访达中显示") : platform === "windows" ? t("在文件资源管理器中显示") : t("在系统文件管理器中显示")}
              </button>
            </div>,
            document.body,
          )}
        </aside>
      </div>
      {tabMenu && <BrowserTabMenu menu={tabMenu} others={surfaces.some((s) => s.kind !== "file" && keyOf(s) !== keyOf(tabMenu.surface))} onDismiss={dismissTabMenu} onClose={(mode) => {
        const target = tabMenu.surface;
        setTabMenu(null);
        void close(mode === "one" ? [target] : surfaces.filter((s) => s.kind !== "file" && (mode === "all" || keyOf(s) !== keyOf(target))));
      }} />}
    </section>
  );
}
