import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { host } from "../port/host";
import type {
  AgentPort,
  BrowserTab,
  WorkspaceChange,
} from "../port/port";
import { openPage, observePageTabs } from "./browser_open";
import { useBrowserSelection } from "./useBrowserSelection";
import { useColumnOpening } from "./useColumnOpening";
import { WorkbenchLauncher } from "./WorkbenchLauncher";
import { AgentBrowserPanel, ManualBrowserPanel, UNREAD_TABS } from "./BrowserPanel";
import { DiffView } from "./cards/DiffView";
import { useOnBump } from "./useOnBump";
import { StudioIcon } from "./StudioIcon";
import { LazyMarkdown } from "./LazyMarkdown";
import type { LocalRefs } from "./Markdown";
import { useGlance } from "./glance";
import { useWorkbenchFiles } from "./useWorkbenchFiles";
import { documentRefs, parseGrantedFile } from "./grantedfiles";
import { hasTabMenu, keyOf, labelOf, type Surface } from "./workbench_tabs";
import { extensionSurfaces, releaseTabs, WorkbenchExtensionBody, type WorkbenchExtensions } from "./workbench_extensions";
import { useTabNames } from "./useTabNames";
import { useTabClose } from "./useTabClose";
import { BrowserTabMenu } from "./BrowserTabMenu";
import { WorkbenchFileMenu, type FileMenu } from "./WorkbenchFileMenu";
import { useWorkbenchTextMenu } from "./useWorkbenchTextMenu";
import { WorkbenchGit } from "./WorkbenchGit";
import { workspaceDrag } from "./filedrag";
import { useGitRepos } from "./useGitRepos";
import { WorkbenchExplorerHead } from "./WorkbenchExplorerHead";
import { WorkbenchFileBar, type FileMode } from "./WorkbenchFileBar";
import { OpenWithMenu } from "./OpenWithMenu";
import type { HubPort } from "../port/hub";
import { useHtmlPreview } from "./useHtmlPreview";
import { setShowsHiddenFiles, showsHiddenFiles } from "../state/prefs";
import { BrowserFullToggle } from "./BrowserFullToggle";
import { documentView, sourceView } from "./document/registry";
import { TooltipButton } from "./TooltipButton";
import { documentState, type DocumentViewState } from "./document/viewState";
const DocumentPreview = lazy(() => import("./document/DocumentPreview"));

// The editor and its grammars load with the first file opened, not with Studio.
const CodeEditor = lazy(() => import("./CodeEditor"));

import { treeRows } from "./workbench_tree";
import { AgentRunView } from "./AgentRunView";
import { useAgentTabs, type AgentRequest } from "./useAgentTabs";

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
  workspaceRoot = "",
  hub,
  revealChanges = 0,
  toggleFiles = 0,
  onCloseManual,
  onSurfaces,
  onActiveFile,
  onExternal,
  onAddToChat,
  onQuoteSelection,
  fullMode,
  fileRequest,
  agentRequest,
  extensions,
}: {
  port: AgentPort;
  tabs: BrowserTab[];
  manual: boolean;
  /** Counts the title-bar Browser presses; a change asks for a browser, not just the column. */
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
  workspaceRoot?: string;
  hub?: Pick<HubPort, "openWith" | "openWithTargets">;
  revealChanges?: number;
  /** A press on the Files button: opens the explorer, or folds it when it is the open one. */
  toggleFiles?: number;
  onCloseManual: () => void;
  // How many surfaces the strip holds, for the pane's own tab to count.
  onSurfaces: (n: number) => void;
  onActiveFile?: (path: string | undefined) => void;
  onExternal: (url: string) => void;
  onAddToChat?: (reference: string) => void;
  // Text selected in an open file, with the file it came from, on its way to the composer.
  onQuoteSelection?: (text: string, path: string) => void;
  fullMode?: { full: boolean; onToggle: () => void };
  extensions?: WorkbenchExtensions;
  fileRequest?: { path: string; consumed?: () => void };
  agentRequest?: AgentRequest;
}) {
  const preview = useHtmlPreview(port, allTabs, remote);
  const tabs = preview.tabs;
  const [files, setFiles] = useState<string[]>([]),
    [directories, setDirectories] = useState<string[]>([]),
    [openFiles, setOpenFiles] = useState<string[]>([]);
  const [showFiles, setShowFiles] = useState(false);
  // The file picked out of the explorer, if that is how it was opened: closing
  // it comes back to the list it was stepped aside from.
  const fromExplorer = useRef("");
  const [launcher, setLauncher] = useState(false);
  const newTab = useRef<HTMLButtonElement>(null);
  const toggleLauncher = () => { supersede(); setLauncher((open) => !open); };
  const chooseFiles = () => { supersede(); setLauncher(false); setShowFiles(true); };
  const toggleExplorer = () => { supersede(); setLauncher(false); setShowFiles((on) => launcher || !on); };
  useOnBump(revealChanges, chooseFiles);
  useOnBump(toggleFiles, toggleExplorer);
  const [query, setQuery] = useState(""),
    [selected, setSelected] = useState("");
  // Docked, the file list hides the canvas a page is drawn in, so choosing any
  // surface steps the list aside.
  const select = useCallback((key: string) => {
    setShowFiles(false);
    setLauncher(false);
    setFailed("");
    setSelected(key);
  }, []);
  const { pick, follow, supersede, current } = useBrowserSelection(port, select, allTabs);
  const [dismissed, setDismissed] = useState<Set<string>>(() => new Set()),
    [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
  const { agents, setAgents } = useAgentTabs(agentRequest, pick, setDismissed);
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
  const { renaming, setRenaming, labelFor, nameTab } = useTabNames((surface) => labelOf(surface, hosts));
  const [failed, setFailed] = useState("");
  // Showing a file where it lives needs a shell on the kernel's own machine.
  const revealable = !remote && port.revealsFiles();
  const [platform, setPlatform] = useState("");
  useEffect(() => void host().describe().then((h) => setPlatform(h.platform)), []);
  const scope = useMemo(() => ({ port, workspaceRoot }), [port, workspaceRoot]);
  const documentStates = useMemo(() => new Map<string, DocumentViewState>(), [scope]);
  useEffect(() => {
    for (const path of documentStates.keys()) if (!openFiles.includes(path)) documentStates.delete(path);
  }, [documentStates, openFiles]);
  const listedScope = useRef(scope);
  const currentScope = useRef(scope);
  currentScope.current = scope;
  const folderRequests = useMemo(() => new Map<string, symbol>(), [scope]);
  const menuId = useRef(0);
  const [menu, setMenu] = useState<(FileMenu & { scope: typeof scope; id: number }) | null>(null);
  const closeMenu = useCallback(() => setMenu(null), []);
  useEffect(closeMenu, [closeMenu, scope, shown]);
  const reveal = (path: string) => {
    setMenu(null);
    port.revealInFileManager(path).then(() => setListFailed(""), (e) => setListFailed(reason(e)));
  };
  // The system menu stays wherever there is text to copy: this one is a shortcut.
  const offerMenu = (e: MouseEvent<HTMLElement>, path: string, folder: boolean) => {
    if (listedScope.current !== scope || document.getSelection()?.isCollapsed === false) return;
    e.preventDefault();
    // The context-menu key reports no pointer, so the row itself is the anchor.
    const row = e.currentTarget.getBoundingClientRect();
    const keyed = e.clientX === 0 && e.clientY === 0;
    setMenu({ path, folder, scope, id: ++menuId.current, anchor: e.currentTarget, x: keyed ? row.left + 24 : e.clientX, y: keyed ? row.bottom : e.clientY });
  };
  const changeKey = changes
    .map((change) => `${change.status}:${change.path}`)
    .join("\n");
  // The kernel reads the disk on every listing and keeps nothing, so the tree is
  // only as stale as the last time it asked. A delete in another application
  // tells nobody here; looking back at the window is when it can have happened.
  const glance = useGlance();
  const gitRepos = useGitRepos(port, glance);
  useEffect(() => {
    if (!shown) return;
    folderRequests.clear();
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
        listedScope.current = scope;
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
  }, [port, scope, folderRequests, shown, changeKey, query, hidden, glance, running, wrote]);
  // Closing the column preserves its tabs; only each tab’s close button removes it.
  const agentPage = tabs.find((tab) => tab.active) ?? tabs[0];
  const agentTarget = agentPage?.target ?? "";
  const agentAt = agentPage ? `${agentPage.target} ${agentPage.url}` : "";
  const visited = useRef(hosts);
  visited.current = hosts;
  const dropBlankStart = useCallback(() => setBrowsers((open) => open.filter((id) => !!visited.current[id])), []);
  const agentNow = useRef(agentTarget);
  agentNow.current = agentTarget;
  const filesAsk = useMemo(() => ({ revealChanges, fileRequest }), [revealChanges, fileRequest]);
  useColumnOpening({ manual, filesAsk, port, agentNow, dropBlankStart, follow, current, setFailed });
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
      fresh.forEach((id) => follow(`browser:${id}`));
    }
  }, [tabs, dropBlankStart, follow, port]);
  // Where the agent is looking, followed as it moves: a new page, or the same
  // tab sent somewhere else.
  const lastAgent = useRef<string | null>(null);
  useEffect(() => {
    const before = lastAgent.current;
    lastAgent.current = agentAt;
    if (!before || !agentAt || before === agentAt) return;
    dropBlankStart();
    follow(`browser:${agentTarget}`);
  }, [agentAt, agentTarget, dropBlankStart, follow]);
  // Preview claims hide fresh tabs until settlement; both followers must capture
  // the revealed notification before its openings retire.
  useEffect(() => { if (allTabs !== UNREAD_TABS) observePageTabs(port, allTabs, !preview.pending); }, [port, allTabs, preview.pending]);
  const noteHost = useCallback(
    (id: string, host: string) =>
      setHosts((v) => (v[id] === host ? v : { ...v, [id]: host })),
    [],
  );
  const surfaces = useMemo<Surface[]>(
    () =>
      ([
        ...browsers.map((id) => ({ kind: "manual", id }) as const),
        ...tabs.map((tab) => ({ kind: "browser", tab }) as const),
        ...openFiles.map((path) => ({ kind: "file", path }) as const),
        ...agents.map((run) => ({ kind: "agent", ...run }) as const),
      ] as Surface[]).filter((s) => !dismissed.has(keyOf(s))).concat(extensionSurfaces(extensions)),
    [browsers, tabs, openFiles, agents, dismissed, extensions],
  );
  const active =
    surfaces.find((s) => keyOf(s) === selected) ??
    surfaces.find((s) => s.kind === "browser" && s.tab.active) ??
    surfaces[0];
  // A document opens rendered: it is read far more often than edited, and the
  // source view is one click away.
  const filePath = active?.kind === "file" ? active.path : "";
  useEffect(() => {
    onActiveFile?.(shown && !launcher && !showFiles && filePath ? filePath : undefined);
    return () => onActiveFile?.(undefined);
  }, [filePath, shown, launcher, showFiles, onActiveFile]);
  const textMenu = useWorkbenchTextMenu({
    path: filePath, mode, scope, shown: shown && !launcher && !showFiles,
    onQuote: onQuoteSelection, onError: setFailed,
  });
  const grantedFile = parseGrantedFile(filePath);
  const readable = /\.(md|markdown|mdx)$/i.test(grantedFile?.rel ?? filePath);
  const hasDocument = documentView(grantedFile?.rel ?? filePath);
  const source = sourceView(grantedFile?.rel ?? filePath);
  const previewable = !!filePath && !grantedFile && preview.can(filePath);
  useEffect(() => {
    setMode((m) => {
      return m === "diff" && source ? m : hasDocument ? "document" : readable ? "read" : "file";
    });
  }, [filePath, readable, hasDocument, source]);
  const previewTab = previewable ? preview.tabFor(filePath) : undefined;
  const { open: openPreview, sync: syncPreview } = preview;
  const { file, draft, diff, busy, error: fileFailed, errorCode, missing, setDraft, save } = useWorkbenchFiles({
    port, workspaceRoot, repos: gitRepos.repos, path: !source || (hasDocument && mode === "document") ? "" : filePath, openPaths: openFiles, shown, refresh: JSON.stringify([changeKey, glance, running, wrote]),
    poll: (readable && mode === "read") || (previewable && mode === "preview"),
  });
  const unpreviewableText = !hasDocument && errorCode === "workspace.file_unreadable";
  const revision = file?.revision;
  useEffect(() => {
    if (mode !== "preview" || !previewable || !shown || showFiles || launcher || previewTab || (!revision && !fileFailed && !failed)) return;
    if (missing) {
      setMode("file");
      return;
    }
    openPreview(filePath, revision).catch((e) => {
      setFailed(reason(e));
      setMode("file");
    });
  }, [mode, previewable, shown, showFiles, launcher, previewTab, revision, fileFailed, missing, failed, filePath, openPreview, tabs]);
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
  // Picking a file is asking to read it. Docked, the list and the file share one
  // column, so the list steps aside; side by side it stays where it is.
  const openFile = useCallback((path: string) => {
    setOpenFiles((v) => (v.includes(path) ? v : [...v, path]));
    setDismissed((v) => {
      const n = new Set(v);
      n.delete(`file:${path}`);
      return n;
    });
    // A file picked while the explorer is shown leaves that list stepped aside
    // rather than closed, so closing this file comes back to the list.
    fromExplorer.current = showFiles ? path : "";
    pick(`file:${path}`);
  }, [pick, showFiles]);
  useEffect(() => { if (fileRequest) { openFile(fileRequest.path); fileRequest.consumed?.(); } }, [fileRequest, openFile]);
  // A rendered document's own links open beside it and its images load from the
  // tree. Held stable per document, so typing re-renders the text alone.
  const openRef = useRef(openFile);
  openRef.current = openFile;
  const docRefs = useMemo<LocalRefs>(() => documentRefs(filePath, port, (path) => openRef.current(path)), [filePath, port]);
  const toggleFolder = async (path: string) => {
    if (listedScope.current !== scope) return;
    const ticket = Symbol();
    folderRequests.set(path, ticket);
    const current = () => currentScope.current === scope && folderRequests.get(path) === ticket;
    if (!collapsed.has(path)) {
      setCollapsed((v) => new Set(v).add(path));
      return;
    }
    try {
      const result = await port.workspaceFiles(path, "", hidden);
      if (!current()) return;
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
      if (current()) setListFailed(reason(e));
    }
  };
  // Native views support sites that refuse framing; web hosts retain the iframe fallback.
  const openingBrowser = useRef<Promise<BrowserTab> | null>(null);
  const addBrowser = async () => {
    supersede();
    const valid = current();
    if (!host().drawsBrowserViews()) {
      const id = `b${(minted.current += 1)}`;
      setBrowsers((v) => [...v, id]);
      pick(`manual:${id}`);
      return;
    }
    const page = openingBrowser.current ??= openPage(port, "about:blank");
    try {
      const tab = await page;
      if (valid()) pick(`browser:${tab.target}`);
    } catch (e) {
      if (valid()) setFailed(reason(e));
    } finally {
      if (openingBrowser.current === page) openingBrowser.current = null;
    }
  };
  const { close: closeTabs, closing, closeFailure } = useTabClose({ port, surfaces, selected, showFiles: showFiles || launcher, fromExplorer, setBrowsers, setOpenFiles, setAgents, setDismissed, setSelected, setShowFiles, setLauncher, onCloseManual, onCloseSelected: () => setFailed("") });
  const close = (targets: Surface[]) => { supersede(); return closeTabs(releaseTabs(targets, extensions)); };
  const dismissTabMenu = useCallback(() => setTabMenu(null), []);
  const [tabMenu, setTabMenu] = useState<{ surface: Surface; x: number; y: number; anchor: HTMLElement } | null>(null);
  const pages = surfaces.filter(hasTabMenu);
  const tabAt = tabMenu ? surfaces.findIndex((s) => keyOf(s) === keyOf(tabMenu.surface)) : -1;
  const tabOthers = tabMenu ? pages.filter((s) => keyOf(s) !== keyOf(tabMenu.surface)) : [];
  const tabRight = tabAt < 0 ? [] : surfaces.slice(tabAt + 1).filter(hasTabMenu);
  const returning = useRef("");
  useEffect(() => {
    if (tabMenu && tabAt < 0) setTabMenu(null);
    if (tabMenu || closing || !returning.current) return;
    const picks = [...document.querySelectorAll<HTMLButtonElement>(".workbench-tab-pick")];
    const back = picks.find((b) => b.dataset.target === returning.current) ?? picks.find((b) => b.closest('[aria-selected="true"]'));
    returning.current = "";
    (back ?? newTab.current)?.focus();
  }, [tabMenu, tabAt, surfaces, closing]);
  const offerTabMenu = (e: MouseEvent<HTMLElement> | KeyboardEvent<HTMLElement>, surface: Surface) => {
    if (!hasTabMenu(surface) || closing || document.getSelection()?.isCollapsed === false) return;
    e.preventDefault();
    e.stopPropagation();
    const anchor = e.currentTarget.matches("button") ? e.currentTarget : e.currentTarget.querySelector<HTMLElement>(".workbench-tab-pick")!;
    const rect = anchor.getBoundingClientRect();
    const pointed = "clientX" in e && (e.clientX !== 0 || e.clientY !== 0);
    setTabMenu({ surface, x: pointed ? e.clientX : rect.left, y: pointed ? e.clientY : rect.bottom, anchor });
  };
  return (
    <section className="workbench" aria-label={t("工作台")} data-action-keydown="layer.dismiss" onKeyDown={(e) => {
      if (launcher && e.key === "Escape") { e.stopPropagation(); toggleLauncher(); newTab.current?.focus(); }
    }}>
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
            aria-selected={!launcher && surface === active}
            data-live={surface.kind === "browser" && surface.tab.active ? "" : undefined}
            data-action-contextmenu="workbench.browser-menu" data-target={keyOf(surface)}
            onContextMenu={(e) => offerTabMenu(e, surface)}
          >
            {renaming === keyOf(surface) ? (
              <span className="workbench-tab-pick">
                <StudioIcon name="globe" />
                <input
                  className="workbench-tab-in"
                  aria-label={t("重命名页签")}
                  autoFocus
                  defaultValue={labelFor(surface)}
                  ref={(el) => el?.select()}
                  data-action-blur="workbench.rename-tab" data-action-keydown="workbench.rename-tab" data-target={keyOf(surface)}
                  onBlur={(e) => nameTab(surface, e.currentTarget.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") { e.preventDefault(); e.currentTarget.blur(); }
                    else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); e.currentTarget.value = labelFor(surface); setRenaming(""); }
                  }}
                />
              </span>
            ) : (
            <button
              className="workbench-tab-pick"
              data-action-click="workbench.tab"
              data-action-keydown="workbench.browser-menu"
              data-target={keyOf(surface)}
              title={
                surface.kind === "browser"
                  ? `${surface.tab.active ? `${t("模型正在操作这个页面")}
` : ""}${surface.tab.url}`
                  : labelFor(surface)
              }
              aria-haspopup={hasTabMenu(surface) ? "menu" : undefined}
              aria-expanded={hasTabMenu(surface) ? tabMenu !== null && keyOf(tabMenu.surface) === keyOf(surface) : undefined}
              onClick={() => pick(keyOf(surface))}
              onDoubleClick={surface.kind === "browser" ? () => setRenaming(keyOf(surface)) : undefined}
              data-action-dblclick="workbench.rename-tab"
              onKeyDown={(e) => {
                if (e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) offerTabMenu(e, surface);
                else if (e.key === "F2" && surface.kind === "browser") { e.preventDefault(); setRenaming(keyOf(surface)); }
              }}
            >
              <StudioIcon name={surface.kind === "file" ? "file" : surface.kind === "agent" ? "branch" : surface.kind.startsWith("extension") ? "plug" : "globe"} />
              <span>{labelFor(surface)}</span>
            </button>
            )}
            {surface.kind !== "extensions" && <button
              className="workbench-tab-close"
              data-action="workbench.close"
              data-target={keyOf(surface)}
              aria-label={t("关闭 {name}", { name: labelFor(surface) })}
              title={t("关闭")}
              disabled={closing}
              onClick={() => void close([surface])}
            >
              <StudioIcon name="close" />
            </button>}
          </div>
        ))}
        </div>
        <TooltipButton
          className="workbench-files"
          data-action="workbench.files"
          aria-pressed={showFiles && !launcher}
          aria-label={t("文件")}
          title={showFiles && !launcher ? t("收起文件树") : t("展开文件树")}
          onClick={toggleExplorer}
        >
          <StudioIcon name="folder" />
        </TooltipButton>
        <TooltipButton
          className="workbench-new"
          data-action="workbench.new"
          ref={newTab}
          aria-label={t("新建标签")}
          title={t("新建标签")}
          aria-expanded={launcher}
          onClick={toggleLauncher}
        >
          <StudioIcon name="plus" />
        </TooltipButton>
        {fullMode && <BrowserFullToggle {...fullMode} />}
      </header>
      {closeFailure && <div className="workbench-error workbench-close-error" role="alert">{closeFailure}</div>}
      <div className="workbench-body" data-files={showFiles && !launcher ? "" : undefined}>
        <main className="workbench-canvas">
          {failed && (launcher || active?.kind !== "file") && (
            <div className="workbench-error" role="alert">
              {failed}
            </div>
          )}
          {(launcher || !active) && (
            <WorkbenchLauncher
              key={String(launcher)} focus={launcher}
              changes={changes.length}
              onFiles={chooseFiles}
              onChanges={() => changes[0] && openFile(changes[0].path)}
              onBrowser={() => void addBrowser()}
            />
          )}
          <div className="workbench-content" hidden={launcher || !active} data-action-contextmenu="workbench.quote-selection" {...textMenu.readerProps}>
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
              shown={shown && !launcher}
              showTabs={false}
              onOpenDocument={(path) => port.openDocument(path)}
            />
          )}
          {extensions && active && <WorkbenchExtensionBody surface={active} ext={extensions} shown={shown && !launcher} />}
          {active?.kind === "agent" && <AgentRunView key={active.ref} port={port} run={active} shown={shown && !launcher && !showFiles} />}
          {active?.kind === "file" && (
            <>
              <WorkbenchFileBar
                path={grantedFile ? `${grantedFile.root.replace(/[\\/]+$/, "")}/${grantedFile.rel}` : active.path}
                mode={mode}
                onMode={setMode}
                readable={readable}
                previewable={previewable}
                document={hasDocument}
                source={source}
                canSave={!busy && !file?.readOnly && file?.path === (grantedFile?.rel ?? active.path) && draft !== file.content}
                readOnlyIn={grantedFile?.root ?? file?.root}
                onSave={() => void save()}
                openWith={hub && !remote ? <OpenWithMenu hub={hub} root={workspaceRoot} onError={(e) => setFailed(reason(e))} /> : undefined}
                onReveal={revealable && !grantedFile ? () => { setFailed(""); port.revealInFileManager(active.path).catch((e) => setFailed(reason(e))); } : undefined}
              />
              {(fileFailed || failed) && mode !== "document" && !unpreviewableText && !(mode === "preview" && previewTab) && (
                <div className="workbench-error" role="alert">
                  {fileFailed || failed}
                </div>
              )}
              {mode === "document" || unpreviewableText ? (
                <Suspense fallback={<div className="workbench-empty">{t("正在读取…")}</div>}>
                  <DocumentPreview key={filePath} port={port} path={filePath} shown={shown && !showFiles && !launcher}
                    view={documentState(documentStates, filePath)}
                    refresh={JSON.stringify([changeKey, glance, running, wrote])}
                    onOpenExternal={!remote && port.opensFiles() && /\.(pdf|html?|svg|docx?|xlsx?|pptx?)$/i.test(filePath) ? () => {
                      const path = grantedFile ? `${grantedFile.root}/${grantedFile.rel}` : filePath;
                      void port.openDocument(path).catch((e) => setFailed(reason(e)));
                    } : undefined} />
                  {failed && <div className="workbench-error" role="alert">{failed}</div>}
                </Suspense>
              ) : mode === "preview" ? (
                previewTab ? (
                  <AgentBrowserPanel tabs={[previewTab]} shown={shown && !launcher} showTabs={false} onOpenDocument={(path) => port.openDocument(path)} />
                ) : (
                  <div className="workbench-empty">{t("正在读取…")}</div>
                )
              ) : busy && !file ? (
                <div className="workbench-empty">{t("正在读取…")}</div>
              ) : mode === "diff" ? (
                <div className="workbench-diff" tabIndex={0}>
                  <DiffView path={active.path} diff={diff} />
                </div>
              ) : mode === "read" && file && file.path === (grantedFile?.rel ?? active.path) ? (
                <div className="workbench-read" tabIndex={0}>
                  <LazyMarkdown text={draft} local={docRefs} />
                </div>
              ) : file && file.path === (grantedFile?.rel ?? active.path) ? (
                <Suspense fallback={<div className="workbench-empty">{t("正在读取…")}</div>}>
                  <CodeEditor
                    key={active.path}
                    path={grantedFile?.rel ?? active.path}
                    value={draft}
                    dark={scheme === "dark"}
                    onChange={setDraft}
                    onSave={() => void save()}
                    readOnly={!!grantedFile || !!file.readOnly}
                    onTextContextMenu={textMenu.offer}
                  />
                </Suspense>
              ) : null}
            </>
          )}
          </div>
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
            openWith={hub && !remote ? <OpenWithMenu hub={hub} root={workspaceRoot} onError={(e) => setListFailed(reason(e))} /> : undefined}
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
          <WorkbenchGit
            port={port}
            workspaceRoot={workspaceRoot}
            changes={changes}
            repos={gitRepos.repos}
            failure={gitRepos.failure}
            onRetry={gitRepos.retry}
            wrote={wrote}
            current={active?.kind === "file" && mode === "diff" ? active.path : ""}
            onOpen={(path) => {
              openFile(path);
              setMode("diff");
            }}
            onChanged={onTreeChanged}
            remote={remote}
          />
          <div className="workbench-filelist">
            {rows.map((row) =>
              row.kind === "folder" ? (
                <button
                  className="workbench-tree-row"
                  data-action-click="workbench.folder"
                  {...workspaceDrag(workspaceRoot, row.path)}
                  data-target={row.path}
                  key={`d:${row.path}`}
                  style={{ paddingInlineStart: 8 + row.depth * 14 }}
                  aria-expanded={!collapsed.has(row.path)}
                  onClick={() => void toggleFolder(row.path)}
                  data-action-contextmenu="workbench.menu"
                  onContextMenu={(e) => offerMenu(e, row.path, true)}
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
                  {...workspaceDrag(workspaceRoot, row.path)}
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
                  onContextMenu={(e) => offerMenu(e, row.path, false)}
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
          {menu?.scope === scope && shown && (
            <WorkbenchFileMenu key={menu.id} menu={menu} root={workspaceRoot}
              revealable={revealable} platform={platform} onReveal={reveal} onAddToChat={onAddToChat} onClose={closeMenu} />
          )}
        </aside>
      </div>
      {textMenu.menu}
      {tabMenu && <BrowserTabMenu menu={tabMenu} others={tabOthers.length > 0} right={tabRight.length > 0} onDismiss={dismissTabMenu} onClose={(mode) => {
        const target = tabMenu.surface;
        returning.current = keyOf(target);
        setTabMenu(null);
        void close(mode === "one" ? [target] : mode === "others" ? tabOthers : mode === "right" ? tabRight : pages);
      }} />}
    </section>
  );
}
