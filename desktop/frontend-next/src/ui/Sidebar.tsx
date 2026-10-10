import { FeedbackBadge, feedbackEntryTab } from "./feedbackentry";
import { useEffect, useMemo, useRef, useState } from "react";
import { t } from "../i18n";
import type { AccountState } from "../port/port";
import type { HubPort, RuntimeView, TreeWorkspace } from "../port/hub";
import type { RemoteHost } from "../port/remote";
import type { Adder } from "./addws";
import { AccountRow } from "./AccountRow";
import { RailSearch } from "./railsearch";
import { RemoteHosts } from "./RemoteHosts";
import { StudioIcon } from "./StudioIcon";
import { useHiddenHosts } from "../state/remotehide";
import { chord } from "./keys";
import { Palette, type Command } from "./Palette";
import { Workspaces } from "./Workspaces";
import { MASK, useHidesAmounts } from "./wallet";

interface Props {
  hub: HubPort;
  // Collapsed, the column keeps its scroll position and its folds; inert is the
  // other half of that — a column nobody can see must not be reachable by Tab.
  collapsed: boolean;
  navShown: boolean;
  tree: TreeWorkspace[];
  treeRead: boolean;
  runtimes: RuntimeView[];
  runs: Record<string, { run: string; live: boolean }>;
  active: string;
  // Where a new session lands, and the row the tree marks as focused. The window
  // decides it from the focused pane, so it arrives rather than being derived.
  activeWorkspace: TreeWorkspace | undefined;
  liveIds: (ids: string[]) => string[];
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
  adder: Adder;
  opening?: string;
  onOpen: (req: { root?: string; sessionPath?: string }) => Promise<void>;
  onOpenRemote: (host: string, workspace?: string, sessionPath?: string) => Promise<void>;
  onFocusPane: (id: string) => void;
  onClosePanes: (ids: string[]) => Promise<void>;
  onPause: (runtimeId: string) => void;
  onArchive: (path: string, archived: boolean, runtimeId?: string) => Promise<void>;
  onRename: (path: string, title: string) => void;
  onCollapse: () => void;
  account: AccountState | null;
  accountUnread: string;
  wallet: string;
  onSettings: (section?: string) => void;
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
  runtimes,
  runs,
  active,
  activeWorkspace,
  liveIds,
  pinned,
  onPin,
  remotes,
  remoteTrees,
  reloadRemotes,
  reloadRemoteTrees,
  readRemoteTree,
  reloadTree,
  adder,
  opening,
  onOpen,
  onOpenRemote,
  onFocusPane,
  onClosePanes,
  onPause,
  onArchive,
  onRename,
  onCollapse,
  account,
  accountUnread,
  wallet,
  onSettings,
  onFeedback,
  feedbackUnread,
  onFind,
  onError,
}: Props) {
  const [railScope, setRailScope] = useState<"all" | "live" | "pinned" | "archived">("all");
  const [palette, setPalette] = useState(false);
  const [folded, setFolded] = useState<Set<string>>(new Set());
  const hideAmounts = useHidesAmounts();
  const hiddenHosts = useHiddenHosts();
  // The rail shows the machines this window was not asked to keep out; a hidden
  // one is still in the book and still listed in settings.
  const shownRemotes = useMemo(
    () => (remotes ?? []).filter((host) => !hiddenHosts.includes(host.name)),
    [remotes, hiddenHosts],
  );
  const newSessionRoot = activeWorkspace?.root;

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
        run: () => void onOpen({ root: newSessionRoot }).catch(onError) },
      { id: "find", label: t("在这段对话里查找"), icon: "search", keywords: "find search conversation 查找 搜索 对话",
        keys: chord("F"), run: onFind },
      { id: "storage", label: t("文件"), icon: "file", keywords: "files storage 文件 存储",
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
    ],
    [newSessionRoot, onOpen, onSettings, onFeedback, feedbackUnread, onFind, onError],
  );
  const sessionCount = tree.reduce((n, ws) => n + ws.sessions.filter((session) => !session.archived).length, 0);
  const archivedCount = tree.reduce((n, ws) => n + ws.sessions.filter((session) => session.archived).length, 0);
  const liveCount = liveIds(runtimes.map((rt) => rt.id)).length;

  const onFold = (root: string, shut: boolean) =>
    setFolded((prev) => {
      const next = new Set(prev);
      if (shut) next.add(root);
      else next.delete(root);
      return next;
    });

  // Seeded once, never per reload: after that the folds are the reader's.
  const seeded = useRef(false);
  useEffect(() => {
    if (seeded.current || !tree.length) return;
    seeded.current = true;
    setFolded(new Set(tree.map((ws) => ws.root).filter((root) => root !== newSessionRoot)));
  }, [tree, newSessionRoot]);

  return (
    <>
    {/* Outside the column's inert wrapper: a collapsed rail must still be
        reachable by the shortcut that opens this. */}
    <Palette
      open={palette}
      onClose={() => setPalette(false)}
      commands={commands}
      tree={tree}
      onOpenSession={(path) => void onOpen({ sessionPath: path }).catch(onError)}
    />
    {/* 收起而不是卸载：卸掉就丢了侧栏的滚动位置、Inspector 的展开、当前选
        中的面板。inert 是「看不见就够不着」那一半 —— 只做视觉隐藏的话，
        屏幕上没有的栏还能被 Tab 走进去。 */}
    <button className="railveil" data-action="chrome.rail" tabIndex={-1} aria-label={t("收起工作区栏")} onClick={() => onCollapse()} />
    <div className="rail" inert={collapsed}>
      <div className="railscroll">
      <div className="studio-rail-head">
        <div className="studio-brand" aria-label="Reasonix Studio">
          <span className="studio-brand-mark" aria-hidden="true"><StudioIcon name="brand" /></span>
          <span className="studio-brand-word" aria-hidden="true">
            <b>reasoni<span className="studio-brand-accent">x</span></b>
            <small>studio</small>
          </span>
          <button className="studio-collapse" data-action="chrome.rail" onClick={() => onCollapse()} aria-label={t("收起工作区栏")} title={t("收起侧栏")}><StudioIcon name="panel" /></button>
        </div>
        <button
          className="studio-new-task"
          data-action="session.new"
          onClick={() => void onOpen({ root: newSessionRoot }).catch(onError)}
        >
          <span aria-hidden="true"><StudioIcon name="plus" /></span><b className="studio-label">{t("新建会话")}</b><kbd>Alt N</kbd>
        </button>
        <button className="studio-search" data-action="workspace.search" onClick={() => setPalette(true)}>
          <span aria-hidden="true"><StudioIcon name="search" /></span><b className="studio-label">{t("搜索与快捷操作")}</b><kbd>Ctrl K</kbd>
        </button>
        <div className="studio-quicknav">
          <button data-action="settings.section" data-value="storage" onClick={() => onSettings("storage")}><span aria-hidden="true"><StudioIcon name="file" /></span><b>{t("文件")}</b></button>
          <button data-action="settings.section" data-value="ext" onClick={() => onSettings("ext")}><span aria-hidden="true"><StudioIcon name="plug" /></span><b>{t("工具与集成")}</b><small>MCP · Skills</small></button>
          <button data-action="settings.section" data-value="remote" onClick={() => onSettings("remote")}><span aria-hidden="true"><StudioIcon name="server" /></span><b>{t("运行环境")}</b><small>Local / SSH</small></button>
        </div>
        <div className="studio-section-label studio-session-label"><span>{t("会话")}</span></div>
        <div className="studio-session-filters" role="group" aria-label={t("会话范围")}>
          <div className="studio-session-segments">
            <button data-action="session.filter" data-value="all" aria-pressed={railScope === "all"} onClick={() => setRailScope("all")}><span>{t("全部")}</span><b>{sessionCount}</b></button>
            <button data-action="session.filter" data-value="live" aria-pressed={railScope === "live"} onClick={() => setRailScope("live")}><span>{t("进行中")}</span><b>{liveCount}</b></button>
            <button data-action="session.filter" data-value="pinned" aria-pressed={railScope === "pinned"} onClick={() => setRailScope("pinned")}><span>{t("置顶")}</span><b>{pinned.size}</b></button>
            <button data-action="session.filter" data-value="archived" aria-pressed={railScope === "archived"} onClick={() => setRailScope("archived")}><span>{t("归档")}</span><b>{archivedCount}</b></button>
          </div>
          <button className="studio-filter-search" data-action="workspace.search" onClick={() => document.querySelector<HTMLInputElement>(".wsfind input")?.focus()} aria-label={t("按名称筛选会话")}><StudioIcon name="search" /></button>
        </div>
      </div>
      <RailSearch>
      <Workspaces
        hub={hub}
        tree={tree}
        treeRead={treeRead}
        runtimes={runtimes}
        active={active}
        folded={folded}
        onFold={onFold}
        reload={reloadTree}
        opening={opening}
        onOpen={onOpen}
        onFocus={onFocusPane}
        onClose={onClosePanes}
        liveIds={liveIds}
        runs={runs}
        scope={railScope}
        pinned={pinned}
        onPin={onPin}
        onPause={(runtimeId) => onPause(runtimeId)}
        onArchive={onArchive}
        onRename={onRename}
        onError={onError}
        adder={adder}
      >
        {shownRemotes.length ? (
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
            onError={onError}
          />
        ) : null}
      </Workspaces>
      </RailSearch>
      </div>
      {!navShown && <div className="railfoot">
        <button className="studio-wallet" data-action="settings.section" data-value="usage" onClick={() => onSettings("usage")}><span aria-hidden="true"><StudioIcon name="wallet" /></span><b>{t("钱包与用量")}</b>{wallet && <small>{hideAmounts ? MASK : wallet}</small>}</button>
        <button className="studio-wallet studio-feedback" data-action="feedback.open" onClick={() => onFeedback(feedbackEntryTab(feedbackUnread))}>
          <span aria-hidden="true"><StudioIcon name="feedback" /></span>
          <b>{t("发送反馈")}</b>
          <FeedbackBadge unread={feedbackUnread} />
        </button>
        <div className="studio-user-foot">
          <AccountRow account={account} unread={accountUnread} onOpen={() => onSettings("account")} />
          <span className="studio-workspace-kind">{t(account?.signedIn ? "个人工作空间" : "本地工作空间")}</span>
          <button className="studio-settings" data-action="chrome.settings" onClick={() => onSettings()} aria-label={t("设置")}><StudioIcon name="settings" /></button>
        </div>
      </div>}
    </div>
    </>
  );
}
