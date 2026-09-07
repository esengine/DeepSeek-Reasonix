import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type Dispatch, type SetStateAction } from "react";
import { reason } from "../i18n/kernel";
import { t } from "../i18n";
import type { AccountState, AgentPort, Appearance as Look, ProviderSetup, ThemePack } from "../port/port";
import type { HubPort, RuntimeView, TreeWorkspace } from "../port/hub";
import { Chrome } from "./Chrome";
import { Nav } from "./Nav";
import { AccountRow } from "./AccountRow";
import { swapping } from "./swap";
import { apply as applyThemePack } from "./theme";
import { apply as applyLook } from "./look";
import { adopt as adoptLang } from "../i18n";
import { Pane, type PaneReport } from "./Pane";
import { Gutter, RAIL, SIDE, widthOf } from "./Gutter";
import { folded as roomGaveUp, onFolds } from "./viewport";
import { trailing } from "./trailing";
import { RemoteAsk } from "./RemoteAsk";
import { RemoteHosts } from "./RemoteHosts";
import type { RemoteAsk as RemoteAskT, RemoteHost } from "../port/remote";
import { Workspaces } from "./Workspaces";
import { Sky } from "./Sky";
import { useAddWorkspace } from "./addws";
import { PaneTabs } from "./PaneTabs";
import { Settings } from "./Settings";
import { Onboarding } from "./Onboarding";
import { Welcome } from "./Welcome";

const NO_REPORT: PaneReport = { status: null, title: "", steer: 0, run: "idle", live: false, cost: "" };

// 度量栏默认展开，收起是用户的选择 —— 那个选择跟主题一样留在盘上，不然拖一下
// 窗口或者重开一次就被顶回展开。
const sideWanted = () => localStorage.getItem("rx-side") !== "0";

// 两栏共用一条规则：窄到放不下就收起，缝和把手留在原处。宽档下的那个选择要留
// 着 —— 拖窄一下再拖回来，不该把用户自己收起或展开的决定抹掉。
function useFoldAway(name: string, set: Dispatch<SetStateAction<boolean>>, wideDefault = true) {
  const wide = useRef(wideDefault);
  useEffect(() => {
    let tight = roomGaveUp(name);
    return onFolds((f) => {
      const now = f.split(" ").includes(name);
      if (now === tight) return;
      tight = now;
      set((cur) => {
        if (!now) return wide.current;
        wide.current = cur;
        return false;
      });
    });
  }, [name, set]);
}

// App is the window around the panes, not a session itself: the workspace tree,
// the chrome, the settings sheet and the theme are the window's, while every
// conversation — its transcript, metrics and event stream — lives in the Pane
// that owns it. That split is what lets two sessions run side by side.
export function App({ hub }: { hub: HubPort }) {
  const [runtimes, setRuntimes] = useState<RuntimeView[]>([]);
  const [active, setActive] = useState("");
  const [tree, setTree] = useState<TreeWorkspace[]>([]);
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
  const [folded, setFolded] = useState<Set<string>>(new Set());
  // 窄到放不下工作区栏时它是收起的，而不是消失的：栏一旦从 DOM 里拿掉，把手也
  // 跟着没了，剩下的入口只有一个没人知道的快捷键。
  const [rail, setRail] = useState(() => !roomGaveUp("rail"));
  const [side, setSide] = useState(() => sideWanted() && !roomGaveUp("side"));
  const chooseSide = useCallback((v: boolean | ((p: boolean) => boolean)) => {
    setSide((cur) => {
      const next = typeof v === "function" ? v(cur) : v;
      localStorage.setItem("rx-side", next ? "1" : "0");
      return next;
    });
  }, []);
  // 三层所有权，只在用的地方相乘：wanted（用户的选择，rail 在 useFoldAway 的
  // 记忆里、side 还在盘上）、allowed（当前视口，useFoldAway 管）、focus（临时
  // 观看状态）。focus 绝不调 setRail/chooseSide —— 那会把临时状态写回偏好，退
  // 出后就恢复不了了。它也不存盘：重开一次窗口不该还在专注里。
  const [focus, setFocus] = useState(false);
  const [railW, setRailW] = useState(() => widthOf(RAIL));
  const [sideW, setSideW] = useState(() => widthOf(SIDE));
  const [report, setReport] = useState<PaneReport>(NO_REPORT);
  const [error, setError] = useState("");
  // false = closed, true = open at its last section, a string = open there.
  const [settings, setSettings] = useState<string | boolean>(false);
  const [theme, setTheme] = useState(() => localStorage.getItem("rx-theme") ?? "auto");
  // "" means never chosen, which is what lets the system's own contrast setting
  // decide. Any explicit pick wins over it from then on.
  const [contrast, setContrast] = useState(() => localStorage.getItem("rx-contrast") ?? "");
  // 空串是「跟随语言」：中文界面本来就该比西文粗一档，样式表按 :lang 给默认。
  const [weight, setWeight] = useState(() => localStorage.getItem("rx-weight") ?? "");
  const [setup, setSetup] = useState<ProviderSetup | null | undefined>(undefined);
  // undefined until asked; false means the opening sequence is still owed.
  const [welcomed, setWelcomed] = useState<boolean | undefined>(undefined);
  const [account, setAccount] = useState<AccountState | null>(null);
  const [pack, setPack] = useState<ThemePack | null>(null);
  const [look, setLook] = useState<Look>({});
  // The metrics column is the window's, but its contents belong to the focused
  // pane, which renders into it through a portal.
  const [sideHost, setSideHost] = useState<HTMLElement | null>(null);
  // Bumped when a pane is rebound to another transcript. It rides the Pane key,
  // so the takeover remounts it: every bit of what is on screen belonged to the
  // conversation it just left.
  const [takeover, setTakeover] = useState<Record<string, number>>({});
  // Every pane's run state, so a tab can show that the conversation behind it
  // is still working. Read through a ref by the report handler, which must stay
  // stable or each frame would re-render every pane.
  const [runs, setRuns] = useState<Record<string, { run: string; live: boolean }>>({});
  const activeRef = useRef("");
  activeRef.current = active;
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
    if (id === activeRef.current) setReport(next);
  }, []);

  const reloadTree = useCallback(
    () =>
      hub
        .tree()
        .then(setTree)
        .catch(() => setTree([])),
    [hub],
  );

  // Panes and tree move together: opening a session marks its row live, closing
  // one hands the row back.
  const reloadPanes = useCallback(async () => {
    const list = await hub.runtimes().catch(() => [] as RuntimeView[]);
    setRuntimes(list);
    setActive((cur) => (list.some((rt) => rt.id === cur) ? cur : (list[0]?.id ?? "")));
    await reloadTree();
  }, [hub, reloadTree]);

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
        setActive(view.id);
      } finally {
        setOpening((n) => n - 1);
        void reloadRemotes();
      }
    },
    [hub, reloadPanes, reloadRemotes],
  );

  // One port per pane, held across renders — a fresh instance would resubscribe
  // the event stream and drop the frames in between.
  const panePorts = useMemo(() => {
    const map = new Map<string, AgentPort>();
    for (const rt of runtimes) map.set(rt.id, hub.portFor(rt));
    return map;
  }, [hub, runtimes]);
  const activePort = panePorts.get(active) ?? panePorts.values().next().value ?? null;

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

  const reloadAccount = useCallback(() => {
    activePort?.account().then(setAccount).catch(() => setAccount(null));
  }, [activePort]);
  useEffect(reloadAccount, [reloadAccount]);

  // Appearance is the window's, not the focused pane's. A remote kernel keeps
  // its own copy, and adopting that one repaints this window because the
  // reader changed tabs — so these three read and write the local pane only.
  const lookPort = useMemo(() => {
    const local = runtimes.find((rt) => !rt.host);
    return local ? hub.portFor(local) : null;
  }, [hub, runtimes]);

  const reloadThemes = useCallback(() => {
    lookPort
      ?.themes()
      .then((list) => setPack(list.find((p) => p.active) ?? null))
      .catch(() => setPack(null));
  }, [lookPort]);
  useEffect(reloadThemes, [reloadThemes]);

  useEffect(() => {
    lookPort
      ?.appearance()
      .then((look) => {
        // The kernel's copy is the authority across machines; adopt reloads if
        // this window booted in the wrong language from a stale local cache.
        if (adoptLang(look.language)) return;
        setLook(look);
      })
      .catch(() => {});
  }, [lookPort]);

  // The control moves now and the config catches up: a size or a colour that
  // waits on a round trip reads as a dead click. The kernel's answer is still
  // what is kept, since it clamps what the slider sent.
  const saveLook = useMemo(
    () => (lookPort ? trailing((next: Look) => lookPort.saveAppearance(next), setLook, fail) : null),
    [lookPort, fail],
  );
  const onLook = useCallback(
    (next: Look) => {
      setLook(next);
      saveLook?.(next);
    },
    [saveLook],
  );

  const running = report.run === "running";
  // A pack carries a light and a dark set, so it is repainted with the scheme
  // rather than once at load: switching the OS to dark has to move both. The
  // running flag rides along because a pack's picture recedes while a turn is
  // in flight — the transition is in CSS, this only moves the target.
  useEffect(() => {
    const mq = matchMedia("(prefers-color-scheme: dark)");
    const paint = () => {
      const scheme = theme === "auto" ? (mq.matches ? "dark" : "light") : theme;
      document.documentElement.dataset.theme = scheme;
      applyThemePack(pack, scheme as "light" | "dark", running);
      // After the pack, never before: size and type are the reader's, and a
      // palette someone else authored does not get to overrule them.
      applyLook(look, running);
    };
    paint();
    mq.addEventListener("change", paint);
    localStorage.setItem("rx-theme", theme);
    return () => mq.removeEventListener("change", paint);
  }, [theme, pack, running, look]);

  useEffect(() => {
    if (contrast) document.documentElement.dataset.contrast = contrast;
    else delete document.documentElement.dataset.contrast;
    localStorage.setItem("rx-contrast", contrast);
    if (weight) document.documentElement.dataset.weight = weight;
    else delete document.documentElement.dataset.weight;
    localStorage.setItem("rx-weight", weight);
  }, [contrast, weight]);

  // A pane with no session file has never been written to — the empty one every
  // window opens with. Opening a conversation takes it over instead of parking
  // a blank column next to it.
  const focusPane = useCallback((id: string) => swapping(() => setActive(id), "pane"), []);
  // Settings is the next layer over the whole screen and had entry but no exit: it
  // simply vanished on unmount. A view transition can animate out an element that
  // is absent from the new state, so the tree need not stay mounted.
  const showPrefs = useCallback((sec?: string) => swapping(() => setSettings(sec ?? true), "prefs"), []);
  // The host book is edited in settings and read by the sidebar, and nothing
  // else would tell it a machine was added: an idle host reports no change to
  // poll for, so a folder added there stayed invisible until the next launch.
  const hidePrefs = useCallback(() => {
    swapping(() => setSettings(false), "prefs");
    void reloadRemotes();
  }, [reloadRemotes]);

  const openPane = useCallback(
    async (req: { root?: string; sessionPath?: string }) => {
      const blank = runtimes.find((rt) => !rt.sessionPath);
      // Asking for a new session when an unused one is already open in that
      // folder: it is the pane being asked for. Rebuilding it would cost a full
      // assembly to arrive back where we started.
      if (blank && !req.sessionPath && blank.root === req.root) {
        focusPane(blank.id);
        return;
      }
      // Same folder: the pane just rebinds, so nothing is torn down and a draft
      // in its composer survives. The kernel refuses a path from another
      // project's session dir, which is why the root has to match.
      if (blank && req.sessionPath && blank.root === req.root) {
        await panePorts.get(blank.id)?.resume(req.sessionPath);
        setTakeover((prev) => ({ ...prev, [blank.id]: (prev[blank.id] ?? 0) + 1 }));
        await reloadPanes();
        focusPane(blank.id);
        return;
      }
      const rt = await hub.open(req);
      // Another folder needs its own runtime, so the blank one is retired
      // rather than left behind.
      if (blank && blank.id !== rt.id) await hub.close(blank.id);
      await reloadPanes();
      focusPane(rt.id);
    },
    [hub, reloadPanes, runtimes, panePorts, focusPane],
  );

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

  // Stable, or the sidebar's memo is defeated by its own handlers and a window
  // with a few hundred sessions rebuilds that whole tree on every repaint.
  const onFold = useCallback((root: string, shut: boolean) => {
    setFolded((prev) => {
      const next = new Set(prev);
      if (shut) next.add(root);
      else next.delete(root);
      return next;
    });
  }, []);

  const adder = useAddWorkspace(hub, reloadTree, fail);
  // 每个窗口都有一个根 —— 没选过项目时那是它碰巧启动的地方。两者读起来一样，
  // 于是「从哪加项目」这句问题永远问不出口；只有内核说的 remembered 分得开。
  const [claimed, setClaimed] = useState(() => localStorage.getItem("rx-claim") === "off");
  const needsProject = !claimed && tree.every((ws) => !ws.remembered);

  useFoldAway("rail", setRail);
  useFoldAway("side", setSide, sideWanted());

  const onRailW = useCallback((w: number) => {
    setRailW(w);
    localStorage.setItem(RAIL.key, String(Math.round(w)));
  }, []);
  const onSideW = useCallback((w: number) => {
    setSideW(w);
    localStorage.setItem(SIDE.key, String(Math.round(w)));
  }, []);

  // A webview has nowhere to put a new tab, so target="_blank" opens nothing at
  // all, and letting the link navigate in place would replace the session with
  // the page. Every link leaves through the host instead.
  useEffect(() => {
    if (!activePort) return;
    const onClick = (e: MouseEvent) => {
      if (e.defaultPrevented || e.button !== 0) return;
      const link = (e.target as Element | null)?.closest?.("a[href]");
      const href = link?.getAttribute("href") ?? "";
      if (!/^https?:\/\//i.test(href)) return;
      e.preventDefault();
      void activePort.openExternal(href).catch(fail);
    };
    addEventListener("click", onClick);
    return () => removeEventListener("click", onClick);
  }, [activePort, fail]);

  // The window's shortcuts, named by the action each one performs — the same
  // identity the control on screen carries, because they are the same thing
  // asked for two ways. Written out rather than branched so the census can read
  // the set: a chain of ifs is a set nothing can enumerate.
  const shortcuts: { chord: string; shift?: boolean; action: string; run: () => void }[] = useMemo(
    () => [
      { chord: "\\", action: "rail.toggle", run: () => setRail((v) => !v) },
      { chord: "\\", shift: true, action: "inspector.toggle", run: () => chooseSide((v) => !v) },
      { chord: ",", action: "chrome.settings", run: showPrefs },
    ],
    [showPrefs, chooseSide],
  );

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey) {
        const hit = shortcuts.find((s) => s.chord === e.key && !!s.shift === e.shiftKey);
        if (hit) {
          e.preventDefault();
          hit.run();
        }
      }
      // Escape stops the turn you are looking at, not every live turn in the
      // window — the other panes are someone else's work in progress. A pane
      // over that turn owns the press first: closing it is not stopping it.
      // Anything transient above this — a menu, a picker, the overflow bubble —
      // takes the press in the capture phase and stops it there (see
      // useDismiss), so a press that reaches this listener is one nothing else
      // wanted. Leaving focus is last: stopping a turn is the more urgent of
      // the two, and doing both on one press would be neither.
      // Escape is two actions on one key, the way send and stop share one
      // button: session.stop while a turn is live, chrome.focus otherwise.
      if (e.key === "Escape" && !settings) {
        if (running) activePort?.cancel();
        else setFocus(false);
      }
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [activePort, running, settings, shortcuts]);

  // A setting changed in the pane is a fact about the session behind it, and
  // the pane is what holds that fact. Without a nudge it keeps polling only
  // while a turn runs — so approving mode, preset and model all changed on disk
  // while the screen went on showing what they were when it opened.
  const [settingsPulse, setSettingsPulse] = useState(0);
  // The chrome's own controls change this session's posture — its preset, its
  // plan mode. Nothing about the pane list or the account moved, so reloading
  // those left the button showing the posture it had when the window opened.
  // What has to hear about it is the pane holding that fact.
  const onSessionSettingChanged = useCallback(() => setSettingsPulse((n) => n + 1), []);

  const onSettingsChanged = useCallback(() => {
    reloadAccount();
    setSettingsPulse((n) => n + 1);
    void reloadPanes();
  }, [reloadAccount, reloadPanes]);

  // A pane's label comes from the tree row it opened, so the sidebar and the
  // tab never disagree. An unnamed session gets a number rather than a third
  // "新会话" — with several open, identical labels are the same as no labels.
  const titleFor = useCallback(
    (rt: RuntimeView, at: number) => {
      for (const ws of tree) {
        for (const session of ws.sessions) {
          if (session.runtimeId === rt.id) return session.title || session.name;
        }
      }
      return at === 0 ? t("新会话") : t("新会话 {n}", { n: at + 1 });
    },
    [tree],
  );

  const tabs = useMemo(
    () => runtimes.map((rt, i) => ({ rt, title: titleFor(rt, i), run: runs[rt.id]?.run ?? "idle", live: runs[rt.id]?.live ?? false })),
    [runtimes, titleFor, runs],
  );
  // The folder only earns tab space when the panes actually span more than one.
  const manyRoots = useMemo(() => new Set(runtimes.map((rt) => rt.root)).size > 1, [runtimes]);

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

  if (setup === undefined || welcomed === undefined) return <div className="app" data-run="idle" />;
  // The sequence and the first connection are one scene, not two screens: the
  // card rises inside it after the collapse, with the introduction still above.
  // A machine that has seen the sequence but still owes a key gets the card
  // over a still scene rather than a replay.
  if ((!welcomed || setup?.required) && activePort) {
    const card = setup?.required ? (
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
    ) : undefined;
    return (
      <Welcome
        variant={setup?.required ? "full" : "short"}
        replay={!welcomed}
        onDone={() => {
          setWelcomed(true);
          void activePort.markWelcomed().catch(() => {});
        }}
      >
        {card}
      </Welcome>
    );
  }

  return (
    <div
      className="app"
      data-run={report.run}
      data-rail={rail ? "on" : "off"}
      data-side={side ? "on" : "off"}
      data-focus={focus ? "true" : undefined}
      data-plan={report.status?.plan ? "on" : "off"}
      data-apv={report.status?.toolApprovalMode ?? "ask"}
      data-prefs={settings ? "" : undefined}
      data-tabs={runtimes.length > 1 ? "" : undefined}
      style={{ "--rail-open": `${railW}px`, "--side-open": `${sideW}px` } as CSSProperties}
    >
      {ask && <RemoteAsk ask={ask} onAnswer={answerRemote} />}

      <Chrome
        host={runtimes.find((rt) => rt.id === active)?.host}
        port={activePort}
        status={report.status}
        title={report.title}
        steer={report.steer}
        run={report.run}
        theme={theme}
        onTheme={setTheme}
        onSettings={showPrefs}
        onChanged={onSessionSettingChanged}
        account={account}
        focus={focus}
        onFocus={() => setFocus((v) => !v)}
      />

      {pack?.sky && <Sky />}

      <div className="cols">
        <Nav
          at={settings === false ? null : settings === true ? "" : settings}
          onGo={showPrefs}
          onHome={hidePrefs}
        />
        {/* 收起而不是卸载：卸掉就丢了侧栏的滚动位置、Inspector 的展开、当前选
            中的面板。inert 是「看不见就够不着」那一半 —— 只做视觉隐藏的话，
            屏幕上没有的栏还能被 Tab 走进去。 */}
        <div className="rail" inert={focus}>
          <div className="railscroll">
          <Workspaces
            hub={hub}
            tree={tree}
            runtimes={runtimes}
            active={active}
            folded={folded}
            onFold={onFold}
            reload={reloadTree}
            onOpen={openPane}
            onFocus={focusPane}
            onClose={closePanes}
            liveIds={liveIds}
            onRename={renameSession}
            onError={fail}
            adder={adder}
          />
          {remotes ? (
            <RemoteHosts
              hub={hub}
              hosts={remotes}
              runtimes={runtimes}
              active={active}
              onOpen={openRemotePane}
              onFocus={focusPane}
              reload={reloadRemotes}
              onError={fail}
            />
          ) : null}
          </div>
          <div className="railfoot">
            <AccountRow account={account} onOpen={() => showPrefs("account")} />
          </div>
        </div>

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
          <Gutter
            edge="r"
            span={SIDE}
            width={sideW}
            label={t("调整度量栏宽度")}
            open={side}
            onWidth={onSideW}
            onOpen={chooseSide}
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

          <div className="panes">
            {runtimes.map((rt) => {
              const port = panePorts.get(rt.id);
              return port ? (
                <Pane
                  key={`${rt.id}:${takeover[rt.id] ?? 0}`}
                  rt={rt}
                  port={port}
                  title={tabs.find((tab) => tab.rt.id === rt.id)?.title ?? t("新会话")}
                  active={rt.id === active}
                  sideHost={sideHost}
                  side={side}
                  onFocus={() => focusPane(rt.id)}
                  visible={rt.id === active}
                  onReport={onReport}
                  // Panes, not just the tree: the first turn gives this pane a
                  // session path, and until /runtimes reports it the pane still
                  // looks blank — the next history row would take it over.
                  onSessionChanged={reloadPanes}
                  pulse={settingsPulse}
                  needsProject={needsProject}
                  // 手输那条逃生口只在侧栏有一份 UI，所以先把栏打开再问。
                  onOpenProject={() => {
                    setRail(true);
                    adder.add("rail");
                  }}
                  onKeepHere={() => {
                    localStorage.setItem("rx-claim", "off");
                    setClaimed(true);
                  }}
                  onSettings={() => showPrefs()}
                />
              ) : null;
            })}
            {runtimes.length === 0 && (
              <div className="panes-empty">
                <span className="mk" aria-hidden="true">
                  ⌘
                </span>
                <p className="t">{t("没有打开的会话")}</p>
                <p className="h">{t("从左边挑一个，或者在当前文件夹开一个新的")}</p>
                <button data-action="session.new" onClick={() => void openPane({ root: tree[0]?.root }).catch(fail)}>{t("开一个新会话")}</button>
              </div>
            )}
          </div>

          {error && (
            <div className="errbar" role="alert">
              <span>{error}</span>
              <button onClick={() => setError("")}>{t("知道了")}</button>
            </div>
          )}
        </div>

        {/* 栏横过来时缝也横过来，把手跟着走：位置仍是「主区和度量之间」，形状和
            左边那枚一样，只是转了 90°。DOM 在栏之前，收起后它就落到窗口底边。 */}
        <button
          className="sidepeek"
          data-shut={side ? undefined : ""}
          title={side ? t("收起") : t("展开")}
          aria-label={side ? t("收起度量栏") : t("展开度量栏")}
          onClick={() => chooseSide(!side)}
        >
          <i className="knurl" aria-hidden="true" />
          <span className="dir" aria-hidden="true">
            {side ? "⌄" : "⌃"}
          </span>
        </button>

        <div className="side" ref={setSideHost} inert={focus} />
      </div>

      {settings && activePort && (
        <Settings
          hub={hub}
          onError={fail}
          port={activePort}
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
          reloadThemes={reloadThemes}
          at={typeof settings === "string" ? settings : undefined}
          account={account}
          reloadAccount={reloadAccount}
        />
      )}
    </div>
  );
}
