import { FeedbackBadge, feedbackEntryTab } from "./feedbackentry";
import { useEffect, useMemo, useRef, useState } from "react";
import { t } from "../i18n";
import logoWordmark from "../assets/logo-wordmark.svg";
import type { AccountState } from "../port/port";
import type { HubPort, RuntimeView, TreeSession, TreeWorkspace } from "../port/hub";
import { workspaceContains } from "../port/hub";
import type { RemoteHost } from "../port/remote";
import type { Adder } from "./addws";
import { AccountRow } from "./AccountRow";
import { ContentMatches } from "./ContentMatches";
import { useDestinations } from "./navdest";
import { ICON, type Section } from "./prefsnav";
import { RailSearch } from "./railsearch";
import { RemoteHosts, remoteProjectKey, remoteWorkspaces } from "./RemoteHosts";
import { remoteProjects } from "./remoteProjects";
import { StudioIcon } from "./StudioIcon";
import { UpdateDot } from "./UpdateDot";
import { useHiddenHosts } from "../state/remotehide";
import { useSidebarFolds } from "../state/sidebarFolds";
import { chord } from "./keys";
import { Palette, type Command } from "./Palette";
import { Workspaces } from "./Workspaces";
import { OpenProject } from "./OpenProject";
import { SessionManager } from "./SessionManager";
import type { TreePaging, TreeView } from "./treewindow";
import { MASK, useHidesAmounts } from "./wallet";
import { useFileDrop } from "./filedrop";
import type { PaneView } from "../state/paneview";
import { RecentSessions } from "./RecentSessions";
import { sessionRuntime } from "./sessionScope";

interface Props {
  hub: HubPort;
  // Collapsed, the column keeps its scroll position and its folds; inert is the
  // other half of that — a column nobody can see must not be reachable by Tab.
  collapsed: boolean;
  navShown: boolean;
  tree: TreeWorkspace[];
  treeRead: boolean;
  answered?: TreeView | null;
  runtimes: RuntimeView[];
  runs: Record<string, { run: string; live: boolean }>;
  active: string;
  // Where a new session lands, and the row the tree marks as focused. The window
  // decides it from the focused pane, so it arrives rather than being derived.
  activeWorkspace: TreeWorkspace | undefined;
  liveIds: (ids: string[]) => string[];
  listedIds?: (ids: string[]) => string[];
  // Pins outlive this column — archiving a session drops one — so the set is the
  // window's and only the toggle comes down here.
  pinned: Set<string>;
  onPin: (path: string) => void;
  remotes: RemoteHost[] | null;
  remoteTrees: Record<string, TreeWorkspace[] | null>;
  reloadRemotes: () => Promise<void>;
  reloadRemoteTrees: () => Promise<void>;
  readRemoteTree: (host: string) => Promise<void>;
  reloadTree: () => Promise<void>;
  retryTree?: () => Promise<void>;
  paging: TreePaging;
  adder: Adder;
  opening?: string;
  onOpen: (req: { root?: string; sessionPath?: string }) => Promise<void>;
  onNew?: () => Promise<void>;
  onOpenRemote: (host: string, workspace?: string, sessionPath?: string) => Promise<void>;
  onFocusPane: (id: string) => void;
  onClosePanes: (ids: string[]) => Promise<void>;
  onPause: (runtimeId: string) => void;
  onBranch?: (runtimeId: string, name: string) => Promise<void>;
  onArchive: (path: string, archived: boolean, runtimeId?: string) => Promise<void>;
  onRename: (path: string, title: string, mode?: "inline" | "dialog") => void;
  onCollapse: () => void;
  account: AccountState | null;
  accountUnread: string;
  wallet: string;
  onSettings: (section?: string) => void;
  onFiles: () => void;
  onView: (to: PaneView) => void;
  onReferenceSession?: (id: string, title: string) => void;
  onFeedback: (tab?: "send" | "mine") => void;
  // Reports with a new reply or a question waiting; the entry and the palette say so.
  feedbackUnread: number;
  onFind: () => void;
  onError: (e: unknown) => void;
}

/** The window's left column: what is mounted, what has been said in it, and who
 *  is signed in. It owns only what nothing outside it reads — which scope the
 *  session filter is on, whether the switcher is open, and which workspaces are
 *  folded. Everything else belongs to the window and arrives as a prop. */
export function Sidebar({
  hub,
  collapsed,
  navShown,
  tree,
  treeRead,
  answered,
  runtimes,
  runs,
  active,
  activeWorkspace,
  liveIds,
  listedIds = liveIds,
  pinned,
  onPin,
  remotes,
  remoteTrees,
  reloadRemotes,
  reloadRemoteTrees,
  readRemoteTree,
  reloadTree,
  retryTree,
  paging,
  adder,
  opening,
  onOpen,
  onNew,
  onOpenRemote,
  onFocusPane,
  onClosePanes,
  onPause,
  onBranch,
  onArchive,
  onRename,
  onCollapse,
  account,
  accountUnread,
  wallet,
  onSettings,
  onFiles,
  onView,
  onReferenceSession,
  onFeedback,
  feedbackUnread,
  onFind,
  onError,
}: Props) {
  const [railScope, setRailScope] = useState<"all" | "live" | "pinned" | "archived">("all");
  const destinations = useDestinations();
  const [palette, setPalette] = useState(false);
  const [projectPicker, setProjectPicker] = useState(false);
  const [recent, setRecent] = useState(false);
  const [located, setLocated] = useState<{ target: string } | null>(null);
  const locatedOnce = useRef<typeof located>(null);
  const [folded, setFolded] = useSidebarFolds();
  const hideAmounts = useHidesAmounts();
  const hiddenHosts = useHiddenHosts();
  // Hidden hosts stay manageable in settings but do not contribute project rows.
  const shownRemotes = useMemo(
    () => (remotes ?? []).filter((host) => !hiddenHosts.includes(host.name)),
    [remotes, hiddenHosts],
  );
  const newSessionRoot = activeWorkspace?.root;
  const startConversation = () => {
    if (onNew) return onNew();
    const runtime = runtimes.find((rt) => rt.id === active);
    return runtime?.host ? onOpenRemote(runtime.host, runtime.root) : onOpen({ root: newSessionRoot });
  };
  const paletteTree = useMemo(() => [...tree, ...shownRemotes.flatMap((host) =>
    remoteWorkspaces(host, remoteTrees[host.name]).map((ws) => ({ ...ws, host: host.name })))], [tree, shownRemotes, remoteTrees]);
  const [over, setOver] = useState(false);
  const [managerOpen, setManagerOpen] = useState(false);
  const drop = useFileDrop((d) => {
    if (collapsed || adder.busy || d.text) return;
    if (d.paths.length !== 1) {
      onError(new Error(d.paths.length ? t("请一次拖入一个文件夹") : t("无法取得本机路径，请使用添加文件夹")));
      return;
    }
    adder.addPath(d.paths[0]);
  }, (on, dt) => setOver(on && !!dt?.types.includes("Files")));

  // The shortcut the button prints. It was drawn and never bound, so the one
  // thing a person learns from the label did nothing.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.key.toLowerCase() !== "k" || e.altKey) return;
      e.preventDefault();
      setPalette((on) => !on);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // What the palette can do is what this column already offers, named once so
  // the two cannot drift into two different lists of the same thing.
  const commands = useMemo<Command[]>(
    () => [
      { id: "new", label: t("新建会话"), icon: "plus", keywords: "new session chat 新建",
        run: () => void startConversation().catch(onError) },
      { id: "find", label: t("在这段对话里查找"), icon: "search", keywords: "find search conversation 查找 搜索 对话",
        keys: chord("F"), run: onFind },
      { id: "view-flow", label: t("显示对话"), icon: "menu", keywords: "conversation chat flow 对话 会话", keys: chord("1"), run: () => onView("flow") },
      { id: "view-analysis", label: t("显示运行分析"), icon: "list", keywords: "run analysis trace 运行 分析", keys: chord("2"), run: () => onView("analysis") },
      { id: "view-browser", label: t("显示工作台"), icon: "panel", keywords: "workbench browser files 工作台 浏览器", keys: chord("3"), run: () => onView("browser") },
      { id: "files", label: t("文件"), icon: "file", keywords: "files workspace explorer 文件 工作区", run: onFiles },
      { id: "storage", label: t("存储"), icon: "file", keywords: "storage disk data 存储 磁盘 数据",
        run: () => onSettings("storage") },
      { id: "ext", label: t("工具与集成"), icon: "plug", keywords: "tools mcp skills 工具 集成",
        run: () => onSettings("ext") },
      { id: "remote", label: t("运行环境"), icon: "server", keywords: "ssh local remote 运行环境",
        run: () => onSettings("remote") },
      { id: "providers", label: t("模型服务"), icon: "layers", keywords: "model provider 模型 来源 服务",
        run: () => onSettings("providers") },
      { id: "usage", label: t("钱包与用量"), icon: "wallet", keywords: "usage cost token 用量 费用",
        run: () => onSettings("usage") },
      { id: "feedback", label: t("发送反馈"), icon: "feedback", keywords: "feedback report bug idea suggestion 反馈 报告 问题 建议 意见",
        run: () => onFeedback("send") },
      { id: "feedback-mine", label: t("我的反馈"), icon: "feedback", keywords: "feedback receipt status my reply 反馈 回执 进展 我的 回复",
        status: feedbackUnread > 0 ? t("{n} 项待查看", { n: feedbackUnread }) : undefined, run: () => onFeedback("mine") },
      { id: "community", label: t("社区与贡献者"), icon: "globe", keywords: "community discord qq group contributors 社区 交流群 加群 贡献者",
        run: () => onSettings("versions:community") },
      { id: "settings", label: t("设置"), icon: "settings", keywords: "settings preferences 设置 偏好",
        run: () => onSettings() },
      { id: "manage-sessions", label: t("管理会话"), icon: "layers", keywords: "session manage batch delete 会话 管理 批量 删除",
        run: () => setManagerOpen(true) },
    ],
    [newSessionRoot, onNew, onOpen, onOpenRemote, runtimes, active, onFiles, onView, onSettings, onFeedback, feedbackUnread, onFind, onError],
  );

  const onFold = (root: string, shut: boolean) => {
    const next = new Set(folded);
    if (shut) next.add(root);
    else next.delete(root);
    setFolded(next);
  };
  const foldable = [
    ...tree.filter((ws) => ws.sessions.length || ws.remaining).map((ws) => ws.root),
    ...shownRemotes.flatMap((host) => remoteWorkspaces(host, remoteTrees[host.name], runtimes)
      .filter((ws) => ws.sessions.length || ws.total || !remoteTrees[host.name] || runtimes.some((rt) => rt.host === host.name && workspaceContains(ws, rt.root)))
      .map((ws) => remoteProjectKey(host.name, ws.root))),
  ];
  const allFolded = foldable.length > 0 && foldable.every((key) => folded.has(key));
  const toggleAll = () => setFolded(allFolded
    ? new Set([...folded].filter((key) => !foldable.includes(key))) : new Set([...folded, ...foldable]));

  const pickRecent = async (host: string, workspace: TreeWorkspace, session: TreeSession) => {
    const held = sessionRuntime(session, runtimes, host);
    if (held) onFocusPane(held.id);
    else if (host) {
      await onOpenRemote(host, session.workspaceRoot || workspace.root, session.path);
      await readRemoteTree(host);
    } else await onOpen({ root: session.workspaceRoot || workspace.root, sessionPath: session.path });
    setRailScope("all");
    onFold(host ? remoteProjectKey(host, workspace.root) : workspace.root, false);
    setLocated({ target: host ? remoteProjectKey(host, workspace.root) + ":" + session.path : session.path });
  };
  useEffect(() => {
    if (!located || recent || locatedOnce.current === located) return;
    const row = [...document.querySelectorAll<HTMLElement>('.rail [role="treeitem"]')]
      .find((element) => element.dataset.target === located.target);
    if (!row) return;
    row.scrollIntoView?.({ block: "nearest" });
    row.focus({ preventScroll: true });
    locatedOnce.current = located;
  }, [located, recent, tree, remoteTrees]);

  return (
    <>
    {recent && <RecentSessions hub={hub} hosts={shownRemotes} onPick={pickRecent} onClose={() => setRecent(false)} />}
    {projectPicker && <OpenProject hub={hub} adder={adder} hosts={shownRemotes}
      onClose={() => setProjectPicker(false)} onError={onError} onOpen={async (host, root) => {
        await onOpenRemote(host, root);
        await reloadRemoteTrees();
      }} />}
    {managerOpen && <SessionManager hub={hub} tree={tree} liveIds={liveIds} reload={reloadTree}
      onOpen={onOpen} onError={onError} onCloseManager={() => setManagerOpen(false)} />}
    {/* Outside the column's inert wrapper: a collapsed rail must still be
        reachable by the shortcut that opens this. */}
    <Palette
      open={palette}
      onClose={() => setPalette(false)}
      commands={commands}
      tree={paletteTree}
      onOpenSession={(path, ws) => void (ws.host ? onOpenRemote(ws.host, ws.root, path) : onOpen({ sessionPath: path })).catch(onError)}
    />
    {/* 收起而不是卸载：卸掉就丢了侧栏的滚动位置、Inspector 的展开、当前选
        中的面板。inert 是「看不见就够不着」那一半 —— 只做视觉隐藏的话，
        屏幕上没有的栏还能被 Tab 走进去。 */}
    <button className="railveil" data-action="chrome.rail" tabIndex={-1} aria-label={t("收起工作区栏")} onClick={() => onCollapse()} />
    <div className="rail" inert={collapsed} ref={drop}>
      {over && !collapsed && !adder.busy && <div className="rail-drop" role="status"><StudioIcon name="folder" /><span>{t("松开以添加项目文件夹")}</span></div>}
      <div className="railscroll">
      <div className="studio-rail-head">
        <div className="studio-brand" aria-label="Reasonix Studio">
          <span className="studio-brand-lockup" aria-hidden="true">
            <img className="studio-brand-logo" src={logoWordmark} alt="" draggable={false} />
            <small className="studio-brand-sub">studio</small>
          </span>
        </div>
        <button
          className="studio-new-task"
          data-action="session.new"
          title="Alt N"
          onClick={() => void startConversation().catch(onError)}
        >
          <span aria-hidden="true"><StudioIcon name="plus" /></span><b className="studio-label">{t("新建会话")}</b>
        </button>
        <button className="studio-search" data-action="workspace.search" title="Ctrl K" onClick={() => setPalette(true)}>
          <span aria-hidden="true"><StudioIcon name="search" /></span><b className="studio-label">{t("搜索与快捷操作")}</b>
        </button>
        <button className="studio-search" data-action="session-manager.manage" title={t("管理会话")} onClick={() => setManagerOpen(true)}>
          <span aria-hidden="true"><StudioIcon name="layers" /></span><b className="studio-label">{t("管理会话")}</b>
        </button>
        {!navShown && (
          <div className="studio-quicknav">
            {destinations.filter(([mark]) => mark !== "usage").map(([mark, label, section]) => mark === "files" ? (
              <button key={mark} data-action="files.open" onClick={onFiles}>
                <span aria-hidden="true"><StudioIcon name="file" /></span><b>{t(label)}</b>
              </button>
            ) : (
              <button key={mark} data-action="settings.section" data-value={section} onClick={() => onSettings(section)}>
                <span aria-hidden="true"><svg viewBox="0 0 16 16" width={15} height={15} fill="none" stroke="currentColor" strokeWidth={1} strokeLinecap="round" strokeLinejoin="round">{ICON[mark as Section]}</svg></span><b>{t(label)}</b>
              </button>
            ))}
          </div>
        )}
        <div className="studio-session-filters" role="group" aria-label={t("会话范围")}>
          <div className="studio-session-segments">
            <button data-action="session.filter" data-value="all" aria-pressed={railScope === "all"} onClick={() => setRailScope("all")}><span>{t("全部")}</span></button>
            <button data-action="session.filter" data-value="live" aria-pressed={railScope === "live"} onClick={() => setRailScope("live")}><span>{t("进行中")}</span></button>
            <button data-action="session.filter" data-value="pinned" aria-pressed={railScope === "pinned"} onClick={() => setRailScope("pinned")}><span>{t("置顶")}</span></button>
            <button data-action="session.filter" data-value="archived" aria-pressed={railScope === "archived"} onClick={() => setRailScope("archived")}><span>{t("归档")}</span></button>
          </div>
        </div>
      </div>
      <RailSearch resetKey={located}>
      <ContentMatches hub={hub} tree={tree} onOpen={onOpen} onError={onError} onReference={onReferenceSession} />
      <Workspaces
        hub={hub}
        tree={tree}
        treeRead={treeRead}
        answered={answered}
        runtimes={runtimes}
        active={active}
        folded={folded}
        onToggleAll={toggleAll}
        allFolded={allFolded}
        onRecent={() => setRecent(true)}
        canFold={foldable.length > 0}
        onFold={onFold}
        reload={reloadTree}
        retry={retryTree ? () => void retryTree() : undefined}
        paging={paging}
        previewLimit={5}
        hasOtherRows={(needle, hideLegacy) => shownRemotes.some((host) => remoteProjects(host, remoteTrees[host.name],
          runtimes, railScope, needle, listedIds, hideLegacy).length > 0)}
        opening={opening}
        onOpen={onOpen}
        onFocus={onFocusPane}
        onClose={onClosePanes}
        liveIds={liveIds}
        listedIds={listedIds}
        runs={runs}
        scope={railScope}
        pinned={pinned}
        onPin={onPin}
        onPause={(runtimeId) => onPause(runtimeId)}
        onBranch={onBranch}
        onArchive={onArchive}
        onRename={onRename}
        onError={onError}
        adder={{ ...adder, add: () => shownRemotes.length ? setProjectPicker(true) : adder.add() }}
      >
        {shownRemotes.some((host) => remoteWorkspaces(host, remoteTrees[host.name]).length > 0 || runtimes.some((rt) => rt.host === host.name)) ? (
          <RemoteHosts
            hub={hub}
            hosts={shownRemotes}
            runtimes={runtimes}
            active={active}
            onOpen={onOpenRemote}
            onFocus={onFocusPane}
            reload={reloadRemotes}
            trees={remoteTrees}
            reloadTrees={reloadRemoteTrees}
            readTree={readRemoteTree}
            onClose={onClosePanes}
            liveIds={liveIds}
            listedIds={listedIds}
            scope={railScope}
            previewLimit={5}
            folded={folded}
            onFold={onFold}
            onError={onError}
          />
        ) : null}
      </Workspaces>
      </RailSearch>
      </div>
      {!navShown && <div className="railfoot">
        {destinations.some(([mark]) => mark === "usage") && <button className="studio-wallet" data-action="settings.section" data-value="usage" onClick={() => onSettings("usage")}><span aria-hidden="true"><StudioIcon name="wallet" /></span><b>{t("钱包与用量")}</b>{wallet && <small>{hideAmounts ? MASK : wallet}</small>}</button>}
        <button className="studio-wallet studio-feedback" data-action="feedback.open" onClick={() => onFeedback(feedbackEntryTab(feedbackUnread))}>
          <span aria-hidden="true"><StudioIcon name="feedback" /></span>
          <b>{t("发送反馈")}</b>
          <FeedbackBadge unread={feedbackUnread} />
        </button>
        <div className="studio-user-foot">
          <AccountRow account={account} unread={accountUnread} onOpen={() => onSettings("account")} />
          <span className="studio-workspace-kind">{t(account?.signedIn ? "个人工作空间" : "本地工作空间")}</span>
          <button className="studio-settings" data-action="chrome.settings" onClick={() => onSettings()} aria-label={t("设置")}><StudioIcon name="settings" /><UpdateDot /></button>
        </div>
      </div>}
    </div>
    </>
  );
}
