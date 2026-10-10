import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { reason } from "../i18n/kernel";
import { t } from "../i18n";
import type { AccountState, AgentPort, ProviderSetup } from "../port/port";
import type { HubPort, RuntimeView, TreeWorkspace } from "../port/hub";
import { Chrome } from "./Chrome";
import { useLaunchHealth } from "./launchhealth";
import { Nav } from "./Nav";
import { useNavRail } from "./navrail";
import { feedbackEntryTab } from "./feedbackentry";
import { useLinkRouting } from "./links";
import { Pane, type PaneReport } from "./Pane";
import { clearDraftForSession } from "./drafts";
import { DOCK, Gutter, RAIL, dockMax, keepWidth, widthOf } from "./Gutter";
import { useWindowKeys, type Shortcut } from "./windowkeys";
import { folded as roomGaveUp, onRoomWidth } from "./viewport";
import { useFoldAway } from "./foldaway";
import { useDrawerCloses } from "./drawer";
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
import { Sky } from "./Sky";import { useAddWorkspace } from "./addws";
import { AddWorkspacePrompt } from "./AddWorkspacePrompt";
import { PaneTabs } from "./PaneTabs";
import { isUnread, useViewed } from "./unread";
import { Onboarding } from "./Onboarding";
import { Welcome } from "./Welcome";
import { useSessionSwitch } from "./sessionswitch";
import { singleFlight } from "./singleflight";
import { useOpenPane } from "./useOpenPane";
import { markSettled } from "../boot/gate";

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
  title: "",
  steer: 0,
  run: "idle",
  live: false,
  cost: "",
  contextPercent: null,
  context: null,
  mcp: [],
  wallet: "",
};
const PINNED_SESSIONS_KEY = "reasonix:pinned-sessions";

function savedPins(): Set<string> {
  try {
    const raw = JSON.parse(localStorage.getItem(PINNED_SESSIONS_KEY) ?? "[]");
    return new Set(Array.isArray(raw) ? raw.filter((x): x is string => typeof x === "string") : []);
  } catch {
    return new Set();
  }
}

// App is the window around the panes, not a session itself: the workspace tree,
// the chrome, the settings sheet and the theme are the window's, while every
// conversation — its transcript, metrics and event stream — lives in the Pane
// that owns it. That split is what lets two sessions run side by side.
export function App({ hub }: { hub: HubPort }) {
  const [runtimes, setRuntimes] = useState<RuntimeView[]>([]);
  const [panesRead, setPanesRead] = useState(false);
  const [active, setActive] = useState(savedActivePane);
  const [tree, setTree] = useState<TreeWorkspace[]>([]);
  // An empty tree before the first read settles is an unread one, not a
  // machine with no folders; only the kernel's answer can say the latter.
  const [treeRead, setTreeRead] = useState(false);
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
  const [pinnedSessions, setPinnedSessions] = useState<Set<string>>(savedPins);
  const [railW, setRailW] = useState(() => widthOf(RAIL));
  const [dockW, setDockW] = useState(() => widthOf(DOCK));
  const [dockLimit, setDockLimit] = useState(() => dockMax(document.body.clientWidth));
  const [report, setReport] = useState<PaneReport>(NO_REPORT);
  const [findPulse, setFindPulse] = useState(0);
  const [error, setError] = useState("");
  // false = closed, true = open at its last section, a string = open there.
  const [settings, setSettings] = useState<string | boolean>(false);
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

  const fail = useCallback((e: unknown) => setError(reason(e)), []);
  // Asked at the moment a confirmation opens, never subscribed to: runs moves
  // on every usage round, and handing the sidebar that would rebuild a tree of
  // a few hundred sessions each frame — which is what its memo is there for.
  const liveIds = useCallback((ids: string[]) => ids.filter((id) => runsRef.current[id]?.live), []);

  const onReport = useCallback((id: string, next: PaneReport) => {
    setRuns((prev) =>
      prev[id]?.run === next.run && prev[id]?.live === next.live ? prev : { ...prev, [id]: { run: next.run, live: next.live } },
    );
    // Every pane's last report, kept in a ref so a background pane's usage
    // round does not re-render the window — and so switching tabs has
    // something to read. Rendering from state here is the same figure at the
    // cost of a window render per background beat.
    reportsRef.current[id] = next;
    if (id === activeRef.current) setReport(next);
  }, []);

  // The window's title, run pill and status belong to the pane in front. They
  // were only ever written when a pane reported, so switching to an idle pane
  // left the title naming the conversation that had last spoken — a pane that
  // has nothing to say never says it again. Freshness follows the switch here
  // rather than waiting for the new pane to happen to report.
  useEffect(() => {
    setReport(reportsRef.current[active] ?? NO_REPORT);
  }, [active]);

  const reloadTree = useCallback(
    () =>
      hub
        .tree()
        .then(setTree)
        .catch(() => {})
        .finally(() => setTreeRead(true)),
    [hub],
  );

  // Held beside this machine's tree, and re-read on the same beat: see
  // useMachineBooks for why that beat is one rather than two.
  const { trees: remoteTrees, reload: reloadRemoteTrees, read: readRemoteTree } = useMachineBooks(hub, remotes);

  // Panes and tree move together: opening a session marks its row live, closing
  // one hands the row back.
  const reloadPanes = useCallback(async () => {
    const list = await hub.runtimes().catch(() => [] as RuntimeView[]);
    // Setup and the welcome are read through a pane's port. With none open
    // there is nothing to ask, and waiting on them would leave the window
    // blank; the next pane to open reads both again.
    if (list.length === 0) {
      setSetup((cur) => (cur === undefined ? null : cur));
      setWelcomed((cur) => cur ?? true);
    }
    setRuntimes(list);
    setPanesRead(true);
    setActive((cur) => (list.some((rt) => rt.id === cur) ? cur : (list[0]?.id ?? "")));
    await reloadTree();
    void reloadRemoteTrees();
  }, [hub, reloadTree, reloadRemoteTrees]);
  const refreshPanes = useMemo(() => singleFlight(reloadPanes), [reloadPanes]);

  useEffect(() => {
    void reloadPanes();
  }, [reloadPanes]);

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

  const openRemotePane = useCallback(
    async (host: string, workspace?: string, sessionPath?: string) => {
      setOpening((n) => n + 1);
      try {
        const view = await hub.openRemote({ host, workspace, sessionPath });
        await reloadPanes();
        sw.cancel();
        setActive(view.id);
      } finally {
        setOpening((n) => n - 1);
        void reloadRemotes();
      }
    },
    [hub, reloadPanes, reloadRemotes, sw.cancel],
  );

  // One port per pane, held across renders — a fresh instance would resubscribe
  // the event stream and drop the frames in between.
  const panePorts = useMemo(() => {
    const map = new Map<string, AgentPort>();
    for (const rt of runtimes) map.set(rt.id, hub.portFor(rt));
    return map;
  }, [hub, runtimes]);
  const activePort = panePorts.get(active) ?? panePorts.values().next().value ?? null;
  // Model requests from a remote pane leave through this machine's broker, so
  // this machine's network is the one a person configures; a remote pane is
  // used only when no local one is open, and is named as such.
  const localRt = runtimes.find((rt) => rt.id === active && !rt.host) ?? runtimes.find((rt) => !rt.host);
  const networkPort = localRt ? panePorts.get(localRt.id) ?? null : activePort;
  const networkHost = localRt ? "" : runtimes.find((rt) => rt.id === active)?.host ?? "";

  useLaunchHealth(activePort, setup, welcomed);

  // Asked on open, on focus (at most every 30 s) and every 10 min while a report
  // is unsettled, never otherwise; a failed refresh stays silent and backs off.
  useFeedbackUnread(networkPort ?? activePort, networkHost, feedbackUnread, setFeedbackUnread, feedback !== null);

  useEffect(() => {
    if (!activePort) return;
    let alive = true;
    activePort.providerSetup().then((v) => alive && setSetup(v)).catch(() => alive && setSetup(null));
    // A machine that cannot answer has met the app before as far as we care:
    // the sequence must never be what stands between someone and their session.
    activePort.welcomeSeen().then((v) => alive && setWelcomed(v)).catch(() => alive && setWelcomed(true));
    return () => {
      alive = false;
    };
  }, [activePort]);

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
  const viewed = useViewed({ runtimes, ports: panePorts, active, tree, remote: remoteTrees, reload: reloadBooks });
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

  const openPane = useOpenPane({ hub, runtimes, panePorts, active, reportsRef, runsRef, setRuntimes, setActive, setTakeover, reloadPanes: refreshPanes, sw });

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

  const adder = useAddWorkspace(hub, reloadTree, fail);
  // 每个窗口都有一个根 —— 没选过项目时那是它碰巧启动的地方。两者读起来一样，
  // 于是「从哪加项目」这句问题永远问不出口；只有内核说的 remembered 分得开。
  const [claimed, setClaimed] = useState(() => localStorage.getItem("rx-claim") === "off");
  const needsProject = treeRead && !claimed && tree.every((ws) => !ws.remembered);

  useFoldAway("rail", setRail);
  const navRail = useNavRail(rail);
  useDrawerCloses(setRail, active, settings);
  useEffect(() => onRoomWidth((width) => setDockLimit(dockMax(width))), []);
  const shownDockW = Math.min(dockW, dockLimit);

  const onRailW = useCallback((w: number) => { setRailW(w); keepWidth(RAIL, w); }, []);
  const onDockW = useCallback((w: number) => { setDockW(w); keepWidth(DOCK, w); }, []);

  useLinkRouting(activePort, useCallback(() => setBrowser(true), []), fail);

  // Settings cover the conversation, so a find opened under them is one nobody
  // sees: asking for it leaves them.
  const openFind = useCallback(() => {
    setSettings(false);
    setFindPulse((n) => n + 1);
  }, []);

  // The window's shortcuts, named by the action each one performs — the same
  // identity the control on screen carries, because they are the same thing
  // asked for two ways. Written out rather than branched so the census can read
  // the set: a chain of ifs is a set nothing can enumerate.
  const zoomKeys = useZoomKeys(look, onLook);
  const shortcuts: Shortcut[] = useMemo(
    () => [
      { chord: "\\", fields: true, action: "rail.toggle", run: () => setRail((v) => !v) },
      { chord: ",", fields: true, action: "chrome.settings", run: showPrefs },
      { chord: "f", action: "transcript.find", run: openFind },
      ...zoomKeys,
    ],
    [showPrefs, openFind, zoomKeys],
  );
  const closeBrowser = useCallback(() => setBrowser(false), []);
  const stopTurn = useCallback(() => activePort?.cancel(), [activePort]);
  useWindowKeys(shortcuts, { browser, settings: !!settings, running, closeBrowser, stop: stopTurn });

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
      for (const ws of tree) {
        for (const session of ws.sessions) {
          // An unwritten one has only a file name; the numbered fallback reads better.
          if (session.runtimeId === rt.id && (session.title || session.turns)) return session.title || session.name;
        }
      }
      return at === 0 ? t("新会话") : t("新会话 {n}", { n: at + 1 });
    },
    [tree],
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
  const errorBar = error ? (
    <div className="errbar" role="alert">
      <span>{error}</span>
      <button onClick={() => setError("")}>{t("知道了")}</button>
    </div>
  ) : null;
  const activeWorkspace = tree.find((ws) => ws.root === activeRuntime?.root) ?? tree[0];
  // The folder a new session opens in is the one the switcher above names —
  // the window's, not whichever project happens to sit first in the tree.
  const newSessionRoot = activeWorkspace?.root;

  usePaneForSheets({
    settings: !!settings,
    feedback: feedback !== null,
    panesRead,
    panes: runtimes.length,
    root: newSessionRoot,
    openPane,
    refuse: (message) => {
      setSettings(false);
      setFeedback(null);
      setError(message);
    },
    fail: (e) => {
      setSettings(false);
      setFeedback(null);
      fail(e);
    },
    clear: (message) => setError((cur) => (cur === message ? "" : cur)),
  });

  // The tab strip has nowhere to await: closing is the end of the gesture there,
  // so a refusal has to land in the error bar rather than in a caller.
  const dropPanes = useCallback((ids: string[]) => void closePanes(ids).catch(fail), [closePanes, fail]);

  // One rename for both surfaces: the tab renames by the pane's session path,
  // the sidebar by the row's — the same file either way.
  const renameSession = useCallback(
    (path: string, title: string) => {
      if (!path) return;
      void hub.renameSession(path, title).then(reloadTree).catch(fail);
    },
    [hub, reloadTree, fail],
  );

  const archiveSession = useCallback(
    async (path: string, archived: boolean, runtimeId?: string) => {
      if (runtimeId) await closePanes([runtimeId]);
      await hub.archiveSession(path, archived);
      if (archived) clearDraftForSession("", path);
      if (archived) {
        setPinnedSessions((current) => {
          if (!current.has(path)) return current;
          const next = new Set(current);
          next.delete(path);
          localStorage.setItem(PINNED_SESSIONS_KEY, JSON.stringify([...next]));
          return next;
        });
      }
      await reloadTree();
    },
    [closePanes, hub, reloadTree],
  );

  const togglePinnedSession = useCallback((path: string) => {
    setPinnedSessions((current) => {
      const next = new Set(current);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      localStorage.setItem(PINNED_SESSIONS_KEY, JSON.stringify([...next]));
      return next;
    });
  }, []);

  if (setup === undefined || welcomed === undefined) return <div className="app" data-run="idle" />;
  if (setup?.required && activePort) {
    return (
      <Onboarding
        port={activePort}
        setup={setup}
        onDone={() => {
          setSetup(null);
          if (!welcomed) {
            setWelcomed(true);
            void activePort.markWelcomed().catch(() => {});
          }
          void reloadPanes();
        }}
      />
    );
  }
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
        title={tabs.find((tab) => tab.rt.id === active)?.title ?? report.title}
        steer={report.steer}
        onSettings={showPrefs}
        onBrowser={() => setBrowser((value) => !value)}
        browser={browser}
        account={account}
        rail={rail}
        theme={scheme}
        onRail={() => setRail((v) => !v)}
        onTheme={() => setTheme(scheme === "dark" ? "light" : "dark")}
        onFind={openFind}
        hub={hub} onError={fail}
      />

      {pack?.sky && effects !== "reduced" && <Sky />}

      <div className="cols">
        {navRail.drawn && (
          <Nav
            at={settings === false ? null : settings === true ? "" : settings}
            onGo={showPrefs}
            onHome={hidePrefs}
            onFeedback={() => setFeedback(feedbackEntryTab(feedbackUnread))}
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
          runtimes={runtimes}
          runs={runs}
          active={active}
          activeWorkspace={activeWorkspace}
          liveIds={liveIds}
          pinned={pinnedSessions}
          onPin={togglePinnedSession}
          remotes={remotes}
          remoteTrees={viewed.remote}
          reloadRemotes={reloadRemotes}
          reloadRemoteTrees={reloadRemoteTrees}
          readRemoteTree={readRemoteTree}
          reloadTree={reloadTree}
          adder={adder}
          opening={sw.path}
          onOpen={openPane}
          onOpenRemote={openRemotePane}
          onFocusPane={focusPane}
          onClosePanes={closePanes}
          onPause={(runtimeId) => void panePorts.get(runtimeId)?.cancel()}
          onArchive={archiveSession}
          onRename={renameSession}
          onCollapse={() => setRail(false)}
          account={account}
          accountUnread={accountUnread}
          wallet={report.wallet}
          onSettings={showPrefs}
          onFeedback={(tab) => setFeedback(tab ?? "send")}
          feedbackUnread={feedbackUnread}
          onFind={openFind}
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
          {tabs.length > 1 && (
            <PaneTabs
              tabs={tabs}
              active={active}
              showRoot={manyRoots}
              onFocus={focusPane}
              onClose={dropPanes}
              onRename={(rt, title) => renameSession(rt.sessionPath ?? "", title)}
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
                  title={tabs.find((tab) => tab.rt.id === rt.id)?.title ?? t("新会话")}
                  active={rt.id === active}
                  sideHost={null}
                  side={false}
                  onFocus={() => focusPane(rt.id)}
                  visible={rt.id === active}
                  onReport={onReport}
                  onTurnDone={viewed.turnDone}
                  // Panes, not just the tree: the first turn gives this pane a
                  // session path, and until /runtimes reports it the pane still
                  // looks blank — the next history row would take it over.
                  onSessionChanged={refreshPanes}
                  pulse={settingsPulse}
                  findPulse={findPulse}
                  alert={rt.id === active ? (errorBar ?? undefined) : undefined}
                  needsProject={needsProject}
                  onOpenProject={() => {
                    setRail(true);
                    adder.add();
                  }}
                  onKeepHere={() => {
                    localStorage.setItem("rx-claim", "off");
                    setClaimed(true);
                  }}
                  onSettings={(section) => section ? showPrefs(section) : showPrefs()}
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
                {/* With no folder there is nowhere a session could open, so the
                    one button offered is the step that makes it possible. */}
                {!treeRead ? null : tree.length === 0 ? (
                  <>
                    <p className="h">{t("会话在文件夹里打开，先添加一个")}</p>
                    <button data-action="workspace.add" disabled={adder.busy} onClick={() => adder.add()}>{t("添加文件夹")}</button>
                  </>
                ) : (
                  <>
                    <p className="h">{t("从左栏选择，或在当前文件夹新建")}</p>
                    <button data-action="session.new" onClick={() => void openPane({ root: newSessionRoot }).catch(fail)}>{t("新建会话")}</button>
                  </>
                )}
              </div>
            )}
          </div>

          {errorBar && !activePaneShown && errorBar}
        </div>

      </div>

      {adder.pathOpen && <AddWorkspacePrompt busy={adder.busy} onSubmit={adder.addPath} onClose={adder.closePath} />}

      {feedback && (networkPort ?? activePort) && (
        <Feedback port={(networkPort ?? activePort)!} tab={feedback} onClose={() => setFeedback(null)} onError={setError} onUnread={setFeedbackUnread} />
      )}

      {settings && activePort && (
        <Boundary fallback={<SettingsUnavailable onClose={hidePrefs} />}>
        <Suspense fallback={<div className="prefs" aria-busy="true" />}>
          <Settings
            hub={hub}
            onError={fail}
            port={activePort}
            networkPort={networkPort ?? activePort}
            networkHost={networkHost}
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
            workspaceRoot={activeWorkspace?.root ?? ""}
          />
        </Suspense>
        </Boundary>
      )}
    </div>
  );
}
