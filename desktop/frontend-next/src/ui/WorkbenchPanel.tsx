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
import { WorkbenchStatus } from "./WorkbenchStatus";
import { WorkbenchFileTree } from "./WorkbenchFileTree";
import { WorkbenchChangesPage } from "./WorkbenchChangesPage";
import { UnifiedDiff } from "./UnifiedDiff";
import { leave, slideAway, useEnteringTabs, useOpenSlide, useOverflowTabs, useSwitchFade } from "./workbench_motion";
import { WorkbenchTabOverflow } from "./WorkbenchTabOverflow";
import { WorkbenchCloseAsk } from "./WorkbenchCloseAsk";
import { WorkbenchState } from "./WorkbenchState";
import { AgentBrowserPanel, UNREAD_TABS } from "./BrowserPanel";
import { ManualBrowsers } from "./ManualBrowsers";
import { WorkbenchTabIcon } from "./WorkbenchTabIcon";
import { WorkbenchTerminals } from "./WorkbenchTerminals";
import { useTerminalTabs } from "./useTerminalTabs";
import { useOnBump } from "./useOnBump";
import { StudioIcon } from "./StudioIcon";
import { LazyMarkdown } from "./LazyMarkdown";
import type { LocalRefs } from "./Markdown";
import { useGlance } from "./glance";
import { useWorkbenchFiles } from "./useWorkbenchFiles";
import { documentRefs, parseGrantedFile } from "./grantedfiles";
import { keyOf, labelOf, type Surface } from "./workbench_tabs";
import { useTabNames } from "./useTabNames";
import { useTabClose } from "./useTabClose";
import { BrowserTabMenu } from "./BrowserTabMenu";
import { WorkbenchFileMenu, type FileMenu } from "./WorkbenchFileMenu";
import { useWorkbenchTextMenu } from "./useWorkbenchTextMenu";
import { WorkbenchGit } from "./WorkbenchGit";
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
import { ExplorerResize } from "./ExplorerResize";
import { AgentRunView } from "./AgentRunView";
import { JobRunView } from "./JobRunView";
import { useAgentTabs, type AgentRequest } from "./useAgentTabs";
import { readRecentFiles, rememberFile } from "./recentFiles";

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
  revealFiles = 0,
  toggleFiles = 0,
  onCloseManual,
  onDismiss,
  onSurfaces,
  onActiveFile,
  onExternal,
  onAddToChat,
  onQuoteSelection,
  onSettings,
  fullMode,
  fileRequest,
  agentRequest,
  onShowCall,
  terminalRequest = 0,
}: {
  onShowCall?: (callId: string) => void;
  // Counts the presses that ask for a terminal.
  terminalRequest?: number;
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
  // Bumped to show the explorer, where revealChanges brings the changes page forward.
  revealFiles?: number;
  /** A press on the Files button: opens the explorer, or folds it when it is the open one. */
  toggleFiles?: number;
  onCloseManual: () => void;
  // Closes the workbench itself, whichever way it was opened.
  onDismiss?: () => void;
  // How many surfaces the strip holds, for the pane's own tab to count.
  onSurfaces: (n: number) => void;
  onActiveFile?: (path: string | undefined) => void;
  onExternal: (url: string) => void;
  onAddToChat?: (reference: string) => void;
  // Text selected in an open file, with the file it came from, on its way to the composer.
  onQuoteSelection?: (text: string, path: string) => void;
  onSettings?: (at?: string) => void;
  fullMode?: { full: boolean; onToggle: () => void };
  fileRequest?: { path: string; consumed?: () => void };
  agentRequest?: AgentRequest;
}) {
  const preview = useHtmlPreview(port, allTabs, remote);
  const tabs = preview.tabs;
  const [files, setFiles] = useState<string[]>([]),
    [directories, setDirectories] = useState<string[]>([]),
    [openFiles, setOpenFiles] = useState<string[]>([]);
  const [showFiles, setShowFiles] = useState(false);
  const [launcher, setLauncher] = useState(false);
  const newTab = useRef<HTMLButtonElement>(null);
  const toggleLauncher = () => { supersede(); setLauncher((open) => !open); };
  const [cursor, setCursor] = useState<[string, number]>(["", 1]);
  const [reread, setReread] = useState(0);
  const chooseFiles = () => {
    supersede(); setLauncher(false); setShowFiles(true);
    requestAnimationFrame(() => frame.current?.querySelector<HTMLInputElement>(".workbench-search input")?.focus());
  };
  const toggleExplorer = () => { supersede(); setLauncher(false); setShowFiles((on) => launcher || !on); };
  const [changesOpen, setChangesOpen] = useState(false);
  const openChanges = () => {
    supersede(); setLauncher(false); setShowFiles(false); setChangesOpen(true);
    setDismissed((v) => (v.has("changes:") ? new Set([...v].filter((k) => k !== "changes:")) : v)); setSelected("changes:");
  };
  useOnBump(revealChanges, openChanges);
  useOnBump(revealFiles, chooseFiles);
  useOnBump(toggleFiles, toggleExplorer);
  const [query, setQuery] = useState(""),
    [selected, setSelected] = useState("");
  // The list and the canvas sit side by side, so a file stays beside its tree;
  // any other surface takes the whole column and steps the list aside.
  const select = useCallback((key: string) => {
    if (!key.startsWith("file:")) setShowFiles(false);
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
  const terminals = useTerminalTabs({ port, shown, pick, setDismissed, onExternal, onError: setFailed, request: terminalRequest });
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
  // fromBar: opened from the file's own toolbar, so what goes wrong is said beside the file.
  const [menu, setMenu] = useState<(FileMenu & { scope: typeof scope; id: number; fromBar?: boolean }) | null>(null);
  const closeMenu = useCallback(() => setMenu(null), []);
  useEffect(closeMenu, [closeMenu, scope, shown]);
  const reveal = (path: string) => {
    const say = menu?.fromBar ? setFailed : setListFailed;
    setMenu(null);
    port.revealInFileManager(path).then(() => say(""), (e) => say(reason(e)));
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
  const filesAsk = useMemo(() => ({ revealChanges, revealFiles, fileRequest }), [revealChanges, revealFiles, fileRequest]);
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
      [
        ...(changesOpen ? [{ kind: "changes" } as const] : []),
        ...browsers.map((id) => ({ kind: "manual", id }) as const),
        ...tabs.map((tab) => ({ kind: "browser", tab }) as const),
        ...openFiles.map((path) => ({ kind: "file", path }) as const),
        ...agents,
        ...terminals.tabs,
      ].filter((s) => !dismissed.has(keyOf(s))),
    [changesOpen, browsers, tabs, openFiles, agents, terminals.tabs, dismissed],
  );
  const active =
    surfaces.find((s) => keyOf(s) === selected) ??
    surfaces.find((s) => s.kind === "browser" && s.tab.active) ??
    surfaces[0];
  // A document opens rendered: it is read far more often than edited, and the
  // source view is one click away.
  const filePath = active?.kind === "file" ? active.path : "";
  useEffect(() => {
    onActiveFile?.(shown && !launcher && filePath ? filePath : undefined);
    return () => onActiveFile?.(undefined);
  }, [filePath, shown, launcher, onActiveFile]);
  const textMenu = useWorkbenchTextMenu({
    path: filePath, mode, scope, shown: shown && !launcher,
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
  const { file, draft, diff, busy, error: fileFailed, errorCode, missing, setDraft, save, saveAt, dropAt, unsaved } = useWorkbenchFiles({
    port, workspaceRoot, repos: gitRepos.repos, path: !source || (hasDocument && mode === "document") ? "" : filePath, openPaths: openFiles, shown, refresh: JSON.stringify([changeKey, glance, running, wrote, reread]),
    poll: (readable && mode === "read") || (previewable && mode === "preview"),
  });
  const unpreviewableText = !hasDocument && errorCode === "workspace.file_unreadable";
  const revision = file?.revision;
  useEffect(() => {
    if (mode !== "preview" || !previewable || !shown || launcher || previewTab || (!revision && !fileFailed && !failed)) return;
    if (missing) {
      setMode("file");
      return;
    }
    openPreview(filePath, revision).catch((e) => {
      setFailed(reason(e));
      setMode("file");
    });
  }, [mode, previewable, shown, launcher, previewTab, revision, fileFailed, missing, failed, filePath, openPreview, tabs]);
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
  const canvas = useRef<HTMLDivElement>(null);
  const frame = useRef<HTMLElement>(null);
  useOpenSlide(frame, shown);
  const [asking, setAsking] = useState<{ path: string; anchor: HTMLElement } | null>(null);
  const tabKeys = surfaces.map(keyOf);
  const entering = useEnteringTabs(tabKeys);
  const [strip, overflow] = useOverflowTabs(tabKeys);
  useSwitchFade(canvas, launcher ? "launcher" : active ? keyOf(active) : "");
  const activeKey = active ? keyOf(active) : "";
  useEffect(() => {
    strip.current?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [activeKey, surfaces.length]);
  // Picking a file is asking to read it; the list stays where it is.
  const [recentFiles, setRecentFiles] = useState(() => readRecentFiles(workspaceRoot ?? ""));
  const openFile = useCallback((path: string) => {
    setRecentFiles(rememberFile(workspaceRoot ?? "", path));
    setOpenFiles((v) => (v.includes(path) ? v : [...v, path]));
    setDismissed((v) => {
      const n = new Set(v);
      n.delete(`file:${path}`);
      return n;
    });
    pick(`file:${path}`);
  }, [pick, workspaceRoot]);
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
  const { close: closeTabs, closing, closeFailure } = useTabClose({ port, surfaces, selected, showFiles: showFiles || launcher, setBrowsers, setOpenFiles, setAgents, setDismissed, setSelected, setLauncher, onCloseManual, onCloseSelected: () => setFailed("") });
  const close = (targets: Surface[]) => { supersede(); return terminals.guard(targets, closeTabs); };
  const dismissTabMenu = useCallback(() => setTabMenu(null), []);
  const [tabMenu, setTabMenu] = useState<{ surface: Surface; x: number; y: number; anchor: HTMLElement } | null>(null);
  const pages = surfaces.filter((s) => s.kind !== "file");
  const tabAt = tabMenu ? surfaces.findIndex((s) => keyOf(s) === keyOf(tabMenu.surface)) : -1;
  const tabOthers = tabMenu ? pages.filter((s) => keyOf(s) !== keyOf(tabMenu.surface)) : [];
  const tabRight = tabAt < 0 ? [] : surfaces.slice(tabAt + 1).filter((s) => s.kind !== "file");
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
    if (surface.kind === "file" || closing || document.getSelection()?.isCollapsed === false) return;
    e.preventDefault();
    e.stopPropagation();
    const anchor = e.currentTarget.matches("button") ? e.currentTarget : e.currentTarget.querySelector<HTMLElement>(".workbench-tab-pick")!;
    const rect = anchor.getBoundingClientRect();
    const pointed = "clientX" in e && (e.clientX !== 0 || e.clientY !== 0);
    setTabMenu({ surface, x: pointed ? e.clientX : rect.left, y: pointed ? e.clientY : rect.bottom, anchor });
  };
  return (
    <section className="workbench" ref={frame} aria-label={t("common.workbench")} data-action-keydown="layer.dismiss" onKeyDown={(e) => {
      if (launcher && e.key === "Escape") { e.stopPropagation(); toggleLauncher(); newTab.current?.focus(); }
    }}>
      <header className="workbench-tabs">
        <TooltipButton
          className="workbench-files"
          data-action="workbench.files"
          aria-pressed={showFiles && !launcher}
          aria-label={t("common.files")}
          title={showFiles && !launcher ? t("workbench.hide_file_tree") : t("workbench.show_file_tree")}
          onClick={toggleExplorer}
        >
          <StudioIcon name="folder" />
          {changes.length > 0 && <small className="workbench-files-count">{changes.length}</small>}
        </TooltipButton>
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
            data-dirty={surface.kind === "file" && unsaved.includes(surface.path) ? "" : undefined}
            data-key={keyOf(surface)} data-enter={entering.has(keyOf(surface)) ? "" : undefined}
            data-action-contextmenu="workbench.browser-menu" data-target={keyOf(surface)}
            onContextMenu={(e) => offerTabMenu(e, surface)}
          >
            {renaming === keyOf(surface) ? (
              <span className="workbench-tab-pick">
                <StudioIcon name="globe" />
                <input
                  className="workbench-tab-in"
                  aria-label={t("workbench.rename_tab")}
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
                  ? `${surface.tab.active ? `${t("workbench.the_model_is_working_on")}
` : ""}${surface.tab.url}`
                  : labelFor(surface)
              }
              aria-haspopup={surface.kind === "file" ? undefined : "menu"}
              aria-expanded={surface.kind === "file" ? undefined : tabMenu !== null && keyOf(tabMenu.surface) === keyOf(surface)}
              onClick={() => pick(keyOf(surface))}
              onDoubleClick={surface.kind === "browser" ? () => setRenaming(keyOf(surface)) : undefined}
              data-action-dblclick="workbench.rename-tab"
              onKeyDown={(e) => {
                if (e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) offerTabMenu(e, surface);
                else if (e.key === "F2" && surface.kind === "browser") { e.preventDefault(); setRenaming(keyOf(surface)); }
              }}
            >
              <WorkbenchTabIcon surface={surface} />
              <span>{labelFor(surface)}</span>
              {surface.kind === "terminal" && surface.busy && <i className="workbench-tab-busy" aria-hidden />}
              {surface.kind === "changes" && changes.length > 0 && <small className="workbench-tab-count">{changes.length}</small>}
            </button>
            )}
            <button
              className="workbench-tab-close"
              data-action="workbench.close"
              data-target={keyOf(surface)}
              aria-label={t("common.disable_name", { name: labelFor(surface) })}
              title={t("common.close")}
              disabled={closing}
              onClick={(e) => {
                const tab = e.currentTarget.closest(".workbench-tab");
                if (surface.kind === "file" && unsaved.includes(surface.path)) setAsking({ path: surface.path, anchor: e.currentTarget });
                else leave(tab, () => void close([surface]));
              }}
            >
              <StudioIcon name="close" />
            </button>
          </div>
        ))}
        {launcher && (
          <div className="workbench-tab" role="tab" aria-selected="true" data-new="">
            <span className="workbench-tab-pick"><StudioIcon name="plus" /><span>{t("workbench.new_tab_label")}</span></span>
          </div>
        )}
        </div>
        <WorkbenchTabOverflow tabs={overflow.flatMap((k) => { const s = surfaces.find((x) => keyOf(x) === k); return s ? [{ key: k, label: labelFor(s) }] : []; })}
          onPick={(k) => { pick(k); strip.current?.querySelector(`[data-key="${CSS.escape(k)}"]`)?.scrollIntoView({ inline: "nearest" }); }} />
        <TooltipButton
          className="workbench-new"
          data-action="workbench.new"
          ref={newTab}
          aria-label={t("workbench.new_tab")}
          title={t("workbench.new_tab")}
          aria-expanded={launcher}
          onClick={toggleLauncher}
        >
          <StudioIcon name="plus" />
        </TooltipButton>
        {fullMode && <BrowserFullToggle {...fullMode} />}
        {onDismiss && (
          <TooltipButton className="workbench-dismiss" data-action="workbench.dismiss" aria-label={t("app.close_workbench")} title={t("app.close_workbench")} onClick={() => slideAway(frame.current, onDismiss)}>
            <StudioIcon name="close" />
          </TooltipButton>
        )}
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
              onBrowser={() => void addBrowser()} onTerminal={() => void terminals.open()}
              recent={recentFiles} onOpenFile={openFile}
            />
          )}
          <div className="workbench-content" ref={canvas} hidden={launcher || !active} data-action-contextmenu="workbench.quote-selection" {...textMenu.readerProps}>
          <ManualBrowsers ids={browsers} active={active?.kind === "manual" ? active.id : ""} onAddress={noteHost} onExternal={onExternal} scheme={scheme} />
          <WorkbenchTerminals port={port} hook={terminals} active={active?.kind === "terminal" ? active.id : ""} shown={shown && !launcher}
            scheme={scheme} onQuote={onQuoteSelection} onClose={(targets) => void close(targets)} />
          {active?.kind === "browser" && (
            <AgentBrowserPanel
              tabs={[active.tab]}
              shown={shown && !launcher}
              showTabs={false} editPage={preview.edit}
              onOpenDocument={(path) => port.openDocument(path)}
            />
          )}
          {active?.kind === "changes" && (
            <WorkbenchChangesPage port={port} changes={changes} branch={gitRepos.repos.find((r) => r.dir === "")?.branch} shown={shown && !launcher}
              remote={remote} onOpen={(path) => { openFile(path); setMode("diff"); }} onChanged={onTreeChanged} />
          )}
          {active?.kind === "job" && <JobRunView key={active.id} port={port} id={active.id} name={active.name} shown={shown && !launcher} />}
          {active?.kind === "agent" && <AgentRunView key={active.ref} port={port} run={active} shown={shown && !launcher} onShowCall={onShowCall} />}
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
                openWith={hub && !remote ? <OpenWithMenu hub={hub} root={workspaceRoot} platform={platform} onError={(e) => setFailed(reason(e))} /> : undefined}
                onMore={grantedFile ? undefined : (anchor) => {
                  const r = anchor.getBoundingClientRect();
                  setMenu({ path: active.path, folder: false, scope, id: ++menuId.current, anchor, x: r.right - 220, y: r.bottom + 4, fromBar: true });
                }}
              />
              {missing && mode !== "document" ? (
                <WorkbenchState icon="file" title={t("workbench.file_is_gone")} detail={t("workbench.it_may_have_been_moved")} alert>
                  <button type="button" data-action="workbench.reread" onClick={() => setReread((n) => n + 1)}>{t("workbench.read_again")}</button>
                  <button type="button" data-weak="" data-action="workbench.close" data-target={keyOf(active)} onClick={() => void close([active])}>{t("workbench.close_tab")}</button>
                </WorkbenchState>
              ) : (fileFailed || failed) && mode !== "document" && !unpreviewableText && !(mode === "preview" && previewTab) && (
                <div className="workbench-error" role="alert">{fileFailed || failed}</div>
              )}
              {mode === "document" || unpreviewableText ? (
                <Suspense fallback={<div className="workbench-empty">{t("common.loading")}</div>}>
                  <DocumentPreview key={filePath} port={port} path={filePath} shown={shown && !launcher}
                    view={documentState(documentStates, filePath)}
                    refresh={JSON.stringify([changeKey, glance, running, wrote])}
                    onOpenExternal={!remote && port.opensFiles() && /\.(pdf|html?|svg|docx?|xlsx?|pptx?)$/i.test(filePath) ? () => {
                      const path = grantedFile ? `${grantedFile.root}/${grantedFile.rel}` : filePath;
                      void port.openDocument(path).catch((e) => setFailed(reason(e)));
                    } : undefined} onSettings={onSettings} />
                  {failed && <div className="workbench-error" role="alert">{failed}</div>}
                </Suspense>
              ) : mode === "preview" ? (
                previewTab ? (
                  <AgentBrowserPanel tabs={[previewTab]} shown={shown && !launcher} showTabs={false} onOpenDocument={(path) => port.openDocument(path)} editPage={preview.edit} />
                ) : (
                  <div className="workbench-empty">{t("common.loading")}</div>
                )
              ) : busy && !file ? (
                <div className="workbench-empty">{t("common.loading")}</div>
              ) : mode === "diff" ? (
                <div className="workbench-diff" tabIndex={0}>
                  <UnifiedDiff diff={diff} label={active.path} target={active.path} onLine={() => setMode("file")} />
                </div>
              ) : mode === "read" && file && file.path === (grantedFile?.rel ?? active.path) ? (
                <div className="workbench-read" tabIndex={0}>
                  <LazyMarkdown text={draft} local={docRefs} />
                </div>
              ) : file && file.path === (grantedFile?.rel ?? active.path) ? (
                <Suspense fallback={<div className="workbench-empty">{t("common.loading")}</div>}>
                  <CodeEditor
                    key={active.path}
                    path={grantedFile?.rel ?? active.path}
                    value={draft}
                    dark={scheme === "dark"}
                    onChange={setDraft}
                    onSave={() => void save()}
                    readOnly={!!grantedFile || !!file.readOnly}
                    onTextContextMenu={textMenu.offer}
                    onLine={(n) => setCursor([active.path, n])}
                    onQuote={onQuoteSelection ? (text) => onQuoteSelection(text, active.path) : undefined}
                  />
                  <WorkbenchStatus path={active.path} line={cursor[0] === active.path ? cursor[1] : 1} dirty={draft !== file.content} />
                </Suspense>
              ) : null}
            </>
          )}
          </div>
        </main>
        <aside className="workbench-explorer">
          <ExplorerResize />
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
            openWith={hub && !remote ? <OpenWithMenu hub={hub} root={workspaceRoot} platform={platform} onError={(e) => setListFailed(reason(e))} /> : undefined}
          />
          <label className="workbench-search">
            <StudioIcon name="search" />
            <input
              aria-label={t("workbench.search_files")}
              data-action="workbench.search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t("workbench.search_files")}
            />
            {query && (
              <button data-action="workbench.clear-search" aria-label={t("common.clear_search")} onClick={() => setQuery("")}>
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
            onOpenPage={changes.length > 0 ? openChanges : undefined}
          />
          <WorkbenchFileTree rows={rows} collapsed={collapsed} workspaceRoot={workspaceRoot} changes={changes}
            current={active?.kind === "file" ? active.path : ""} onFolder={(path) => void toggleFolder(path)} onFile={openFile} onMenu={offerMenu} />
          {menu?.scope === scope && shown && (
            <WorkbenchFileMenu key={menu.id} menu={menu} root={workspaceRoot}
              revealable={revealable} platform={platform} onReveal={reveal} onAddToChat={onAddToChat} onClose={closeMenu} />
          )}
        </aside>
      </div>
      {textMenu.menu}
      {asking && (
        <WorkbenchCloseAsk name={asking.path.split("/").at(-1) || asking.path} anchor={asking.anchor} onCancel={() => setAsking(null)}
          onSave={async () => { const ok = await saveAt(asking.path); if (ok) { setAsking(null); void close([{ kind: "file", path: asking.path }]); } return ok; }}
          onDrop={() => { dropAt(asking.path); setAsking(null); void close([{ kind: "file", path: asking.path }]); }} />
      )}
      {tabMenu && <BrowserTabMenu menu={tabMenu} others={tabOthers.length} right={tabRight.length} all={pages.length} label={labelFor(tabMenu.surface)} onDismiss={dismissTabMenu} onClose={(mode) => {
        const target = tabMenu.surface;
        returning.current = keyOf(target);
        setTabMenu(null);
        void close(mode === "one" ? [target] : mode === "others" ? tabOthers : mode === "right" ? tabRight : pages);
      }} />}
    </section>
  );
}
