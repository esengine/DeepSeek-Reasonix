import { useBranchSession } from "./useBranchSession";
import { CompositionNewContext } from "./CompositionNewContext";
import { keepSame } from "./stable";
import { usePaneCallbacks } from "./usePaneCallbacks";
import { watchTreeWake } from "./treewake";
import { usePinnedSessions } from "./usePinnedSessions";
import { AutoArchiveNotice } from "./AutoArchiveNotice";
import { LevelUpNotice } from "./LevelUpNotice";
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type CSSProperties } from "react";
import { reason } from "../i18n/kernel";
import { t } from "../i18n";
import type { AccountState, AgentPort, ProviderSetup } from "../port/port";
import type { HubPort, RuntimeView } from "../port/hub";
import { workspaceContains } from "../port/hub";
import { Chrome } from "./Chrome";
import { RailToggle } from "./RailToggle";
import { useFirstRunRead, useLaunchHealth } from "./launchhealth";
import { Nav } from "./Nav";
import { useTreeWindow, type TreePaging } from "./treewindow";
import { useNavRail } from "./navrail";
import { feedbackEntryTab } from "./feedbackentry";
import { ErrorBar } from "./ErrorBar";
import { noteFault } from "./fault";
import { ToastStack } from "./ToastStack";
import { useLinkRouting } from "./links";
import { Pane, type PaneReport } from "./Pane";
import { clearDraftForSession } from "./drafts";
import { DOCK, Gutter, RAIL, dockMax, keepWidth, widthOf } from "./Gutter";
import { useWindowKeys, type Shortcut } from "./windowkeys";
import { useEscStop } from "../state/escstop";
import { startUpdateWatch } from "../state/updatewatch";
import { StopHint } from "./StopHint";
import { useFindAsk } from "./usefindask";
import { folded as roomGaveUp, onRoomWidth } from "./viewport";
import { useFoldAway } from "./foldaway";
import { useDrawerCloses } from "./drawer";
import { RenameSession } from "./RenameSession";
import { RemoteAsk } from "./RemoteAsk";
import { useFeedbackUnread } from "./useFeedbackUnread";
import { Feedback, type FeedbackTab } from "./Feedback";
import { usePaneForSheets } from "./usePaneForSheets";
import { BrowserLogin } from "./BrowserLogin";
import type { RemoteAsk as RemoteAskT, RemoteHost } from "../port/remote";
import { Boundary } from "./Boundary";
import { SettingsUnavailable } from "./SettingsUnavailable";
import { useMachineBooks } from "./machinebooks";
import { usePaint } from "./paint";
import { useZoomKeys } from "./zoomkeys";
import { rememberActivePane, savedActivePane } from "./activepane";
import { Sidebar } from "./Sidebar";
import { PeerThreadsProvider } from "./peerthreads";
import { WallpaperStage } from "./WallpaperStage";import { useAddWorkspace } from "./addws";
import { AddWorkspacePrompt } from "./AddWorkspacePrompt";
import { PaneTabs } from "./PaneTabs";
import { paneReader } from "../state/paneread";
import { onShowsSessionTabsChange, showsSessionTabs } from "../state/prefs";
import { useLiveGrace } from "./useLiveGrace";
import { isUnread, useViewed, useUnreadBadge } from "./unread";
import { useFirstRun } from "./useFirstRun";
import { Welcome } from "./Welcome";
import { useSessionSwitch } from "./sessionswitch";
import { singleFlight } from "./singleflight";
import { useTreeRead } from "./useTreeRead";
import { useSidebarOpen } from "./sidebaropen";
import { useOpenPane } from "./useOpenPane";
import { useAdoptPane } from "./team/useAdoptPane";
import { TeamLayer } from "./team/TeamLayer";
import { useOpenRemotePane } from "./useOpenRemotePane";
import { useNewConversation } from "./useNewConversation";
import { markSettled } from "../boot/gate";
import type { PaneView } from "../state/paneview";
import { usePaneSessionRefs } from "./usePaneSessionRefs";

// Start fetching the settings chunk with the shell instead of waiting for the
// first click. It remains a separate chunk (and keeps its failure boundary),
// but opening settings no longer produces a veil while the module catches up.
const settingsModule = import("./Settings").then(
  (module) => ({ module, error: null as unknown }),
  (error: unknown) => ({ module: null, error }),
);
const Settings = lazy(async () => {
  const loaded = await settingsModule;
  if (loaded.error) throw loaded.error;
  return { default: loaded.module!.Settings };
});

const NO_REPORT: PaneReport = {
  status: null,
  workspace: null,
  title: "",
  steer: 0,
  run: "idle",
  live: false,
  cost: "",
  contextPercent: null,
  context: null,
  mcp: [],
  wallet: "",
  view: "flow",
  rows: 0,
  surfaces: 0,
};

// App is the window around the panes, not a session itself: the workspace tree,
// the chrome, the settings sheet and the theme are the window's, while every
// conversation — its transcript, metrics and event stream — lives in the Pane
// that owns it. That split is what lets two sessions run side by side.
export function App({ hub }: { hub: HubPort }) {
  const sessionTabs = useSyncExternalStore(onShowsSessionTabsChange, showsSessionTabs, showsSessionTabs);
  const [runtimes, setRuntimes] = useState<RuntimeView[]>([]);
  const [panesRead, setPanesRead] = useState(false);
  const [active, setActive] = useState(savedActivePane);
  const treeWindow = useTreeWindow();
  const { tree, treeRead, answered, readTree, reloadTree, retryTree } = useTreeRead(hub, treeWindow);
  // Null until asked, and null again where this kernel refuses remote panes —
  // which is what keeps the section out of a browser rather than drawing a
  // heading over a feature that cannot work there.
  const [remotes, setRemotes] = useState<RemoteHost[] | null>(null);
  // Connects in flight. A link reports "connecting" only once the dial starts,
  // so without this the poll would look at an idle list and stand down exactly
  // when the step list is what the user is waiting on.
  const [opening, setOpening] = useState(0);
  // A connect stopped for a question. One at a time by construction: the link
  // that asked is blocked until it is answered.
  const [ask, setAsk] = useState<RemoteAskT | null>(null);
  // 窄到放不下工作区栏时它是收起的，而不是消失的：栏一旦从 DOM 里拿掉，把手也
  // 跟着没了，剩下的入口只有一个没人知道的快捷键。
  const [rail, setRail] = useState(() => !roomGaveUp("rail"));
  const [pinnedSessions, togglePinnedSession, unpinSession] = usePinnedSessions(hub, tree, treeRead, (e) => setError(reason(e)));
  const [railW, setRailW] = useState(() => widthOf(RAIL));
  const [dockW, setDockW] = useState(() => widthOf(DOCK));
  const [dockLimit, setDockLimit] = useState(() => dockMax(document.body.clientWidth));
  const [report, setReport] = useState<PaneReport>(NO_REPORT);
  const [filesRequests, setFilesRequests] = useState<Record<string, number>>({});
  const [viewRequests, setViewRequests] = useState<Record<string, { to: PaneView; n: number }>>({});
  const [sessionRefs, referenceSession] = usePaneSessionRefs(active, (id) => panePorts.has(id));
  const [error, setError] = useState("");
  const [renaming, setRenaming] = useState<{ path: string; title: string } | null>(null);
  // false = closed, true = open at its last section, a string = open there.
  const [settings, setSettings] = useState<string | boolean>(false);
  const find = useFindAsk(settings, setSettings);
  const [feedback, setFeedback] = useState<FeedbackTab | null>(null);
  const [feedbackUnread, setFeedbackUnread] = useState(0);
  const [browser, setBrowser] = useState(false);
  const [setup, setSetup] = useState<ProviderSetup | null | undefined>(undefined);
  // undefined until asked; false means the opening sequence is still owed.
  const [welcomed, setWelcomed] = useState<boolean | undefined>(undefined);
  const [account, setAccount] = useState<AccountState | null>(null);
  const [accountUnread, setAccountUnread] = useState("");
  // Bumped when a pane is rebound to another transcript. It rides the Pane key,
  // so the takeover remounts it: every bit of what is on screen belonged to the
  // conversation it just left.
  const [takeover, setTakeover] = useState<Record<string, number>>({});
  // Every pane's run state, so a tab can show that the conversation behind it
  // is still working. Read through a ref by the report handler, which must stay
  // stable or each frame would re-render every pane.
  const [runs, setRuns] = useState<Record<string, { run: string; live: boolean }>>({});
  const sw = useSessionSwitch();
  const activeRef = useRef("");
  activeRef.current = active;
  useEffect(() => rememberActivePane(active), [active]);
  const reportsRef = useRef<Record<string, PaneReport>>({});
  const runsRef = useRef(runs);
  runsRef.current = runs;

  const fail = useCallback((e: unknown) => {
    const shown = reason(e);
    noteFault(e, shown);
    setError(shown);
  }, []);
  // Asked at the moment a confirmation opens, never subscribed to: runs moves
  // on every usage round, and handing the sidebar that would rebuild a tree of
  // a few hundred sessions each frame — which is what its memo is there for.
  const liveIds = useCallback((ids: string[]) => ids.filter((id) => runsRef.current[id]?.live), []);

  const { listedIds, noteReport } = useLiveGrace(runsRef, runs);

  const onReport = useCallback((id: string, next: PaneReport) => {
    noteReport(id, next.live);
    setRuns((prev) =>
      prev[id]?.run === next.run && prev[id]?.live === next.live ? prev : { ...prev, [id]: { run: next.run, live: next.live } },
    );
    // Every pane's last report, kept in a ref so a background pane's usage
    // round does not re-render the window — and so switching tabs has
    // something to read. Rendering from state here is the same figure at the
    // cost of a window render per background beat.
    reportsRef.current[id] = next;
    if (id === activeRef.current) setReport(next);
  }, [noteReport]);

  // The window's title, run pill and status belong to the pane in front. They
  // were only ever written when a pane reported, so switching to an idle pane
  // left the title naming the conversation that had last spoken — a pane that
  // has nothing to say never says it again. Freshness follows the switch here
  // rather than waiting for the new pane to happen to report.
  useEffect(() => {
    setReport(reportsRef.current[active] ?? NO_REPORT);
  }, [active]);

  const paging = useMemo<TreePaging>(
    () => ({
      more: (root) => treeWindow.grow(root, readTree),
      view: (v) => { if (treeWindow.view(v)) void reloadTree(); },
    }),
    [treeWindow, readTree, reloadTree],
  );

  // Held beside this machine's tree, and re-read on the same beat: see
  // useMachineBooks for why that beat is one rather than two.
  const { trees: remoteTrees, reload: reloadRemoteTrees, read: readRemoteTree } = useMachineBooks(hub, remotes);

  // Panes and tree move together: opening a session marks its row live, closing
  // one hands the row back.
  const readPanes = useMemo(() => paneReader(() => hub.runtimes()), [hub]);
  const reloadPanes = useCallback(async () => {
    const list = await readPanes(); if (!list) return;
    // Setup and the welcome are read through a pane's port. With none open
    // there is nothing to ask, and waiting on them would leave the window
    // blank; the next pane to open reads both again.
    if (list.length === 0) {
      setSetup((cur) => (cur === undefined ? null : cur));
      setWelcomed((cur) => cur ?? true);
    }
    setRuntimes((cur) => keepSame(cur, list));
    setPanesRead(true);
    setActive((cur) => (list.some((rt) => rt.id === cur) ? cur : (list[0]?.id ?? "")));
    await reloadTree();
    void reloadRemoteTrees();
  }, [hub, readPanes, reloadTree, reloadRemoteTrees]);
  const refreshPanes = useMemo(() => singleFlight(reloadPanes), [reloadPanes]);

  useEffect(() => {
    void refreshPanes();
  }, [refreshPanes]);

  useEffect(() => {
    const refresh = () => { void refreshPanes(); };
    const stop = hub.onTreeChanged?.(refresh);
    const stopWake = watchTreeWake(refresh);
    return () => { stop?.(); stopWake(); };
  }, [hub, refreshPanes]);

  const reloadRemotes = useCallback(
    () =>
      hub
        .remoteHosts()
        .then(setRemotes)
        .catch(() => setRemotes(null)),
    [hub],
  );

  useEffect(() => {
    void reloadRemotes();
  }, [reloadRemotes]);

  // One timer per answer, not one that runs regardless: a link mid-connect
  // changes by the second, a settled one only when something breaks, and three
  // idle hosts should cost nothing at all.
  useEffect(() => {
    if (!remotes?.length) return;
    const working = opening > 0 || remotes.some((h) => h.status === "connecting" || h.status === "reconnecting");
    if (!working && remotes.every((h) => h.status === "idle")) return;
    const timer = setTimeout(() => void reloadRemotes(), working ? 400 : 5000);
    return () => clearTimeout(timer);
  }, [remotes, opening, reloadRemotes]);

  useEffect(() => hub.onRemoteAsk(setAsk), [hub]);

  const answerRemote = useCallback(
    (id: string, ok: boolean, text: string) => {
      setAsk(null);
      hub.answerRemote(id, ok, text);
    },
    [hub],
  );

  const openRemotePane = useOpenRemotePane({ hub, reloadPanes, reloadRemotes, sw, setActive, setOpening });

  // One port per pane, held across renders — a fresh instance would resubscribe
  // the event stream and drop the frames in between.
  const panePorts = useMemo(() => {
    const map = new Map<string, AgentPort>();
    for (const rt of runtimes) map.set(rt.id, hub.portFor(rt));
    return map;
  }, [hub, runtimes]);
  const activePort = panePorts.get(active) ?? panePorts.values().next().value ?? null;
  const machinePort = useMemo(() => hub.machinePort(), [hub]);
  const machineRt = runtimes.find((rt) => !rt.host);
  const localRt = runtimes.find((rt) => rt.id === active && !rt.host) ?? machineRt;
  const localWorkspacePort = useMemo(() => hub.machinePort(localRt?.id), [hub, localRt?.id]);

  useLaunchHealth(activePort, setup, welcomed);

  // Asked on open, on focus (at most every 30 s) and every 10 min while a report
  // is unsettled, never otherwise; a failed refresh stays silent and backs off.
  useEffect(() => startUpdateWatch(machinePort), [machinePort]);
  useFeedbackUnread(machineRt ? machinePort : null, machineRt?.id ?? "", feedbackUnread, setFeedbackUnread, feedback !== null);

  useFirstRunRead(activePort, setSetup, setWelcomed);

  // A kernel that does not carry sign-in refuses this with a code and a
  // sentence. Dropping it left the panel saying "checking…" for the rest of
  // the session about a question that had already been answered.
  const reloadAccount = useCallback(() => {
    activePort
      ?.account()
      .then((a) => {
        setAccount(a);
        setAccountUnread("");
      })
      .catch((e) => {
        setAccount(null);
        setAccountUnread(reason(e));
      });
  }, [activePort]);
  useEffect(reloadAccount, [reloadAccount]);

  useEffect(() => {
    if (setup !== undefined && welcomed !== undefined) markSettled();
  }, [setup, welcomed]);

  const running = report.run === "running";
  const { theme, setTheme, scheme, contrast, setContrast, weight, setWeight, effects, look, onLook, pack, reloadThemes } =
    usePaint(hub, runtimes, running, fail);
  // A pane with no session file has never been written to — the empty one every
  // window opens with. Opening a conversation takes it over instead of parking
  // a blank column next to it.
  // A transcript can be thousands of pixels tall. Capturing it into a View
  // Transition made a simple sidebar click pay for a full-page texture before
  // the active id changed. Selection feedback should be immediate.
  const reloadBooks = useCallback(() => Promise.all([reloadTree(), reloadRemoteTrees()]), [reloadTree, reloadRemoteTrees]);
  const viewed = useViewed({ runtimes, ports: panePorts, active: settings || feedback ? "" : active, tree, remote: remoteTrees, reload: reloadBooks });
  useUnreadBadge(viewed.tree, viewed.remote);
  const focusPane = useCallback(
    (id: string) => {
      sw.cancel();
      setActive(id);
      viewed.view(id);
    },
    [sw.cancel, viewed.view],
  );
  // Settings is the next layer over the whole screen and had entry but no exit: it
  // simply vanished on unmount. A view transition can animate out an element that
  // is absent from the new state, so the tree need not stay mounted.
  const showPrefs = useCallback((sec?: string) => setSettings(sec ?? true), []);
  // The host book is edited in settings and read by the sidebar, and nothing
  // else would tell it a machine was added: an idle host reports no change to
  // poll for, so a folder added there stayed invisible until the next launch.
  const hidePrefs = useCallback(() => {
    setSettings(false);
    void reloadRemotes();
  }, [reloadRemotes]);

  const openFiles = useCallback(() => {
    if (!panePorts.has(active)) {
      setError(t("请先打开一个会话，再查看它的文件。"));
      return;
    }
    hidePrefs();
    if (roomGaveUp("scene")) setRail(false);
    setFilesRequests((requests) => ({ ...requests, [active]: (requests[active] ?? 0) + 1 }));
  }, [active, panePorts, hidePrefs]);

  const showView = useCallback((to: PaneView) => {
    if (!panePorts.has(active)) return;
    hidePrefs();
    setViewRequests((requests) => ({ ...requests, [active]: { to, n: (requests[active]?.n ?? 0) + 1 } }));
  }, [active, panePorts, hidePrefs]);

  const openPane = useOpenPane({ hub, runtimes, panePorts, active, reportsRef, runsRef, setRuntimes, setActive, setTakeover, reloadPanes: refreshPanes, sw });
  const adoptPane = useAdoptPane(setRuntimes, focusPane, sw);

  // Awaitable because deleting a conversation has to close its pane first and
  // then wait: the kernel refuses to erase a transcript its runtime still holds,
  // so firing the close off and deleting in the same breath races the teardown.
  // Batched because the reload behind it walks every session on disk, and a
  // folder's worth of panes must not pay for that once each.
  const closePanes = useCallback(
    async (ids: string[]) => {
      for (const id of ids) await hub.close(id);
      await reloadPanes();
    },
    [hub, reloadPanes],
  );

  const openPeerSession = useCallback((path: string) => void openPane({ sessionPath: path }).catch(fail), [openPane, fail]);

  const openFromSidebar = useSidebarOpen<PaneView>(runtimes, openPane, setActive, setViewRequests, "flow");
  const adder = useAddWorkspace(hub, reloadTree, fail, (workspace) => openPane({ root: workspace.root }));
  // 每个窗口都有一个根 —— 没选过项目时那是它碰巧启动的地方。两者读起来一样，
  // 于是「从哪加项目」这句问题永远问不出口；只有内核说的 remembered 分得开。
  const [claimed, setClaimed] = useState(() => localStorage.getItem("rx-claim") === "off");
  const { focusFor, openProject, keepHere, openSettings, openFeedback, openSessionFor } = usePaneCallbacks({
    focusPane, setRail, addFolder: () => adder.add(), setClaimed, showPrefs, setFeedback, runtimes, openPane, fail,
  });
  const needsProject = treeRead && !claimed && tree.every((ws) => !ws.remembered);

  useFoldAway("rail", setRail);
  const navRail = useNavRail(rail);
  useDrawerCloses(setRail, active, settings);
  useEffect(() => onRoomWidth((width) => setDockLimit(dockMax(width))), []);
  const shownDockW = Math.min(dockW, dockLimit);

  const onRailW = useCallback((w: number) => { setRailW(w); keepWidth(RAIL, w); }, []);
  const onDockW = useCallback((w: number) => { setDockW(w); keepWidth(DOCK, w); }, []);

  useLinkRouting(activePort, useCallback(() => setBrowser(true), []), fail);

  // The window's shortcuts, named by the action each one performs — the same
  // identity the control on screen carries, because they are the same thing
  // asked for two ways. Written out rather than branched so the census can read
  // the set: a chain of ifs is a set nothing can enumerate.
  const zoomKeys = useZoomKeys(look, onLook);
  const shortcuts: Shortcut[] = useMemo(
    () => [
      { chord: "\\", fields: true, action: "rail.toggle", run: () => setRail((v) => !v) },
      { chord: ",", fields: true, action: "chrome.settings", run: showPrefs },
      { chord: "f", action: "transcript.find", run: find.open },
      { chord: "1", fields: true, action: "pane.view", run: () => showView("flow") },
      { chord: "2", fields: true, action: "pane.view", run: () => showView("analysis") },
      { chord: "3", fields: true, action: "pane.view", run: () => showView("browser") },
      ...zoomKeys,
    ],
    [showPrefs, find.open, showView, zoomKeys],
  );
  const closeBrowser = useCallback(() => setBrowser(false), []);
  const stopTurn = useCallback(() => activePort?.cancel(), [activePort]);
  const escMode = useEscStop();
  const [stopArmed, setStopArmed] = useState(false);
  useWindowKeys(shortcuts, { browser, settings: !!settings, running, confirm: escMode === "confirm", closeBrowser, stop: stopTurn, onArm: setStopArmed });

  // A setting changed in the pane is a fact about the session behind it, and
  // the pane is what holds that fact. Without a nudge it keeps polling only
  // while a turn runs — so approving mode, preset and model all changed on disk
  // while the screen went on showing what they were when it opened.
  const [settingsPulse, setSettingsPulse] = useState(0);
  const onSettingsChanged = useCallback(() => {
    // Removing the last model source is a setting too, and it has to land the
    // window back on connecting one rather than on a composer with no model.
    activePort?.providerSetup().then(setSetup).catch(() => {});
    reloadAccount();
    setSettingsPulse((n) => n + 1);
    void reloadPanes();
  }, [activePort, reloadAccount, reloadPanes]);

  // A pane's label comes from the tree row it opened, so the sidebar and the
  // tab never disagree. An unnamed session gets a number rather than a third
  // "新会话" — with several open, identical labels are the same as no labels.
  const titleFor = useCallback(
    (rt: RuntimeView, at: number) => {
      for (const ws of rt.host ? viewed.remote[rt.host] ?? [] : tree) {
        for (const session of ws.sessions) {
          // An unwritten one has only a file name; the numbered fallback reads better.
          const held = rt.host ? workspaceContains(ws, rt.root) && session.path === rt.sessionPath : session.runtimeId === rt.id;
          if (held && (session.title || session.turns)) return session.title || session.name;
        }
      }
      return at === 0 ? t("新会话") : t("新会话 {n}", { n: at + 1 });
    },
    [tree, viewed.remote],
  );

  const tabs = useMemo(
    () =>
      runtimes.map((rt, i) => ({
        rt,
        title: titleFor(rt, i),
        run: runs[rt.id]?.run ?? "idle",
        live: runs[rt.id]?.live ?? false,
        unread: isUnread(rt, viewed.tree, viewed.remote),
      })),
    [runtimes, titleFor, runs, viewed.tree, viewed.remote],
  );
  // The folder only earns tab space when the panes actually span more than one.
  const manyRoots = useMemo(() => new Set(runtimes.map((rt) => rt.root)).size > 1, [runtimes]);
  const activeRuntime = runtimes.find((rt) => rt.id === active);
  // A failed request floats above the composer of the pane in front, where the
  // eye already is; with no pane on screen it takes the main area's corner.
  const activePaneShown = !!activeRuntime && panePorts.has(activeRuntime.id);
  const errorBar = useMemo(() => (error ? <ErrorBar text={error} onDismiss={() => setError("")} onReport={() => setFeedback("send")} /> : null), [error]);
  const bar = useMemo(
    () => errorBar ?? <LevelUpNotice port={machineRt ? machinePort : null} tick={feedback === null ? 0 : 1} fallback={<AutoArchiveNotice tree={tree} />} />,
    [errorBar, tree, machineRt, machinePort, feedback],
  );
  const activeWorkspace = tree.find((ws) => !activeRuntime?.host && workspaceContains(ws, activeRuntime?.root)) ?? tree[0];
  const newSessionRoot = activeWorkspace?.root;
  const startConversation = useNewConversation(active, runtimes, newSessionRoot, openPane, openRemotePane);

  usePaneForSheets({
    settings: !!settings,
    feedback: feedback !== null,
    panesRead,
    panes: runtimes.length,
    root: newSessionRoot,
    openPane,
    fail: (e) => {
      setSettings(false);
      setFeedback(null);
      fail(e);
    },
  });

  // The tab strip has nowhere to await: closing is the end of the gesture there,
  // so a refusal has to land in the error bar rather than in a caller.
  const dropPanes = useCallback((ids: string[]) => void closePanes(ids).catch(fail), [closePanes, fail]);

  const renameSession = useCallback(
    (path: string, title: string, mode: "inline" | "dialog" = "dialog") => {
      if (!path) return;
      if (mode === "inline") void hub.renameSession(path, title).then(reloadTree).catch(fail);
      else setRenaming({ path, title });
    },
    [hub, reloadTree, fail],
  );

  const archiveSession = useCallback(
    async (path: string, archived: boolean, runtimeId?: string) => {
      if (runtimeId) await closePanes([runtimeId]);
      await hub.archiveSession(path, archived);
      if (archived) clearDraftForSession("", path);
      if (archived) unpinSession(path);
      await reloadTree();
    },
    [closePanes, hub, reloadTree, unpinSession],
  );

  const branchSession = useBranchSession({ panePorts, runtimes, runsRef, openPane });

  const first = useFirstRun({ port: activePort, setup, welcomed, setSetup, setWelcomed, reloadPanes });

  if (setup === undefined || welcomed === undefined) return <div className="app" data-run="idle" />;
  if (first.gate) return first.gate;
  if (!welcomed && activePort) {
    return (
      <Welcome
        variant="short"
        replay
        onDone={() => {
          setWelcomed(true);
          void activePort.markWelcomed().catch(() => {});
        }}
      />
    );
  }

  return (
    <CompositionNewContext.Provider value={(id) => {const rt=runtimes.find((v)=>v.id===active);void (rt?.host?openRemotePane(rt.host,rt.root,undefined,id):openPane({root:newSessionRoot,agentComposition:id})).catch(fail);}}>
    <PeerThreadsProvider port={activePort} hub={hub} onOpen={openPeerSession}>
    <div
      className="app"
      data-run={report.run}
      data-rail={rail ? "on" : "off"}
      data-nav={navRail.shown ? "on" : "off"}
      data-browser={browser ? "on" : "off"}
      data-plan={report.status?.plan ? "on" : "off"}
      data-apv={report.status?.toolApprovalMode ?? "ask"}
      data-prefs={settings ? "" : undefined}
      data-tabs={runtimes.length > 1 ? "" : undefined}
      style={{ "--rail-open": `${railW}px`, "--dock-open": `${shownDockW}px` } as CSSProperties}
    >
      {ask && <RemoteAsk ask={ask} onAnswer={answerRemote} />}
      <BrowserLogin />

      <Chrome
        host={runtimes.find((rt) => rt.id === active)?.host}
        port={activePort}
        status={report.status}
        workspace={report.workspace}
        title={tabs.find((tab) => tab.rt.id === active)?.title ?? report.title}
        steer={report.steer}
        account={account}
        theme={scheme}
        onTheme={() => setTheme(scheme === "dark" ? "light" : "dark")}
        onFind={find.toggle}
        view={report.view} rows={report.rows} surfaces={report.surfaces} onView={showView}
        hub={hub} seat={runtimes.find((rt) => rt.id === active)?.team} onError={fail}
      />

      <WallpaperStage sky={!!pack?.sky && effects !== "reduced"} look={look} onLook={onLook} />

      <div className="cols">
        {navRail.drawn && (
          <Nav
            at={settings === false ? null : settings === true ? "" : settings}
            onGo={showPrefs}
            onFiles={openFiles}
            onHome={hidePrefs}
            onRail={() => setRail((v) => !v)}
            onFeedback={() => setFeedback(feedbackEntryTab(feedbackUnread))}
            onNew={() => {
              hidePrefs();
              void startConversation().catch(fail);
            }}
            feedbackUnread={feedbackUnread}
            shown={navRail.shown}
          />
        )}
        <Sidebar
          hub={hub}
          collapsed={!rail}
          navShown={navRail.shown}
          tree={viewed.tree}
          treeRead={treeRead}
          answered={answered}
          runtimes={runtimes}
          runs={runs}
          active={active}
          activeWorkspace={activeWorkspace}
          liveIds={liveIds}
          listedIds={listedIds}
          pinned={pinnedSessions}
          onPin={togglePinnedSession}
          remotes={remotes}
          remoteTrees={viewed.remote}
          reloadRemotes={reloadRemotes}
          reloadRemoteTrees={reloadRemoteTrees}
          readRemoteTree={readRemoteTree}
          reloadTree={reloadTree}
          retryTree={retryTree}
          paging={paging}
          adder={adder}
          opening={sw.path}
          onOpen={openFromSidebar}
          onNew={startConversation}
          onOpenRemote={openRemotePane}
          onFocusPane={focusPane}
          onAdoptPane={adoptPane}
          onClosePanes={closePanes}
          onPause={(runtimeId) => void panePorts.get(runtimeId)?.cancel()}
          onBranch={branchSession}
          onArchive={archiveSession}
          onRename={renameSession}
          onCollapse={() => setRail(false)}
          account={account}
          accountUnread={accountUnread}
          wallet={report.wallet}
          onSettings={showPrefs}
          onFiles={openFiles}
          onView={showView}
          onReferenceSession={panePorts.has(active) ? referenceSession : undefined}
          onFeedback={(tab) => setFeedback(tab ?? "send")}
          feedbackUnread={feedbackUnread}
          onFind={find.open}
          onError={fail}
        />

        <div className="main">
          <Gutter
            edge="l"
            span={RAIL}
            width={railW}
            label={t("调整工作区栏宽度")}
            open={rail}
            onWidth={onRailW}
            onOpen={setRail}
          />
          {/* One conversation on screen at a time. Side by side, two panes
              squeezed each other and a glance could not tell which composer
              belonged to which run; the ones behind keep streaming either way. */}
          {sessionTabs && tabs.length > 1 && (
            <PaneTabs
              tabs={tabs}
              active={active}
              showRoot={manyRoots}
              onFocus={focusPane}
              onClose={dropPanes}
              onRename={(rt, title, mode) => renameSession(rt.sessionPath ?? "", title, mode)}
            />
          )}

          <div className="panes" aria-busy={sw.pending || undefined}>
            {runtimes.map((rt) => {
              const port = panePorts.get(rt.id);
              return port ? (
                <Pane
                  key={`${rt.id}:${takeover[rt.id] ?? 0}`}
                  rt={rt}
                  port={port}
                  hub={hub}
                  title={tabs.find((tab) => tab.rt.id === rt.id)?.title ?? t("新会话")}
                  active={rt.id === active}
                  sideHost={null}
                  side={false}
                  onFocus={focusFor(rt.id)}
                  visible={rt.id === active}
                  onReport={onReport}
                  onTurnDone={viewed.turnDone}
                  // Panes, not just the tree: the first turn gives this pane a
                  // session path, and until /runtimes reports it the pane still
                  // looks blank — the next history row would take it over.
                  onSessionChanged={refreshPanes}
                  pulse={settingsPulse}
                  findPulse={find.pulse}
                  findTogglePulse={find.togglePulse}
                  filesPulse={filesRequests[rt.id] ?? 0}
                  viewRequest={viewRequests[rt.id]}
                  sessionRef={sessionRefs[rt.id]}
                  alert={rt.id === active ? bar : undefined}
                  connect={first.connect}
                  needsProject={needsProject && !rt.team}
                  onOpenProject={openProject}
                  onKeepHere={keepHere}
                  onSettings={openSettings}
                  onFeedback={openFeedback}
                  onOpenSession={openSessionFor(rt.id)}
                  theme={scheme}
                  dockW={shownDockW}
                  dockMax={dockLimit}
                  onDockW={onDockW}
                  manualBrowser={rt.id === active && browser}
                  onManualBrowser={setBrowser}
                />
              ) : null;
            })}
            {runtimes.length === 0 && (
              <div className="panes-empty">
                <span className="mk" aria-hidden="true">
                  ⌘
                </span>
                <p className="t">{t("没有打开的会话")}</p>
                {!treeRead ? null : tree.length === 0 ? (
                  <>
                    <p className="h">{t("没有文件夹也能开始：会话会在应用自己的目录里打开")}</p>
                    <button data-action="session.new" onClick={() => void startConversation().catch(fail)}>{t("新建会话")}</button>
                    <button data-action="workspace.add" disabled={adder.busy} onClick={() => adder.add()}>{t("添加文件夹")}</button>
                  </>
                ) : (
                  <>
                    <p className="h">{t("从左栏选择，或在当前文件夹新建")}</p>
                    <button data-action="session.new" onClick={() => void startConversation().catch(fail)}>{t("新建会话")}</button>
                  </>
                )}
              </div>
            )}
            <TeamLayer hub={hub} active={active} seat={runtimes.find((rt) => rt.id === active)?.team} root={runtimes.find((rt) => rt.id === active)?.root} onAdoptPane={adoptPane} />
          </div>

          {!activePaneShown && bar}
        </div>

      </div>

      {adder.pathOpen && <AddWorkspacePrompt busy={adder.busy} onSubmit={adder.addPath} onClose={adder.closePath} />}
      {renaming && (
        <RenameSession session={renaming} hub={hub} onClose={() => setRenaming(null)}
          onSaved={() => { setRenaming(null); void reloadTree().catch(fail); }} />
      )}

      <ToastStack onFeedback={() => setFeedback("send")} port={machinePort} idle={!running} look={look} onLook={onLook} />

      {feedback && activePort && (
        <Feedback port={machinePort} tab={feedback} onClose={() => setFeedback(null)} onError={setError} onUnread={setFeedbackUnread} />
      )}

      {settings && activePort && (
        <Boundary fallback={<SettingsUnavailable onClose={hidePrefs} />}>
        <Suspense fallback={<div className="prefs" aria-busy="true" />}>
          <Settings
            hub={hub}
            onError={fail}
            port={activePort}
            networkPort={machinePort}
            localWorkspacePort={localWorkspacePort}
            networkHost=""
            status={report.status}
            theme={theme}
            onTheme={setTheme}
            contrast={contrast}
            weight={weight}
            onWeight={setWeight}
            look={look}
            onLook={onLook}
            onContrast={setContrast}
            onClose={hidePrefs}
            onChanged={onSettingsChanged}
            onSessionsRecovered={() => void reloadTree()}
            reloadThemes={reloadThemes}
            at={typeof settings === "string" ? settings : undefined}
            account={account}
            accountUnread={accountUnread}
            reloadAccount={reloadAccount}
            workspaceRoot={localRt?.root ?? ""}
          />
        </Suspense>
        </Boundary>
      )}
      <RailToggle rail={rail} onRail={() => setRail((v) => !v)} />
      <StopHint armed={stopArmed} />
    </div>
    </PeerThreadsProvider>
    </CompositionNewContext.Provider>
  );
}
