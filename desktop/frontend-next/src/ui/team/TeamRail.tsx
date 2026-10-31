import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import { createPortal } from "react-dom";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { HubPort, RuntimeView } from "../../port/hub";
import type { TeamAgent, TeamGroup, TeamView } from "../../port/team";
import { useTeam } from "../../state/team";
import { setTeamFolded, useTeamFolded } from "../../state/teamFold";
import { RailSectionToggle } from "../RailSectionToggle";
import { closeGroupStage, openGroupStage, useGroupStage } from "../../state/teamStage";
import { isUnread, noteSeen, useTeamUnread } from "../../state/teamUnread";
import { pushToast } from "../../state/toasts";
import { useDismiss } from "../dismiss";
import { pinToViewport } from "../place";
import { StudioIcon } from "../StudioIcon";
import { workspaceMenuKeys } from "../WorkspaceOrder";
import { AgentAvatar, GroupAvatar } from "./AgentAvatar";
import { AgentDialog } from "./AgentDialog";
import { GroupDialog } from "./GroupDialog";
import { eventText } from "./groupEvents";
import { TeamMenu, type MenuTarget } from "./TeamMenu";
import { useReorderMotion } from "./useReorderMotion";
import { UnreadDot } from "../UnreadMark";
import "../../styles/team.css";

type Row = { kind: "agent"; agent: TeamAgent } | { kind: "group"; group: TeamGroup };

const pinnedOf = (r: Row) => (r.kind === "agent" ? !!r.agent.pinned : !!r.group.pinned);
const activeAt = (r: Row) => (r.kind === "agent" ? r.agent.lastActivityAt : r.group.lastActivityAt) ?? 0;

// The rows in the order the sidebar draws them: the first main agent leads,
// then pinned rows, then the rest by when they last moved. The kernel owns
// each field; this only orders them. Hidden rows are counted, and drawn only
// when the person asks for them.
export function teamRows(view: TeamView, withHidden = false): { lead?: TeamAgent; rows: Row[]; hidden: number } {
  const lead = view.agents.find((a) => a.main);
  const all: Row[] = [
    ...view.agents.filter((a) => a !== lead).map((agent) => ({ kind: "agent" as const, agent })),
    ...view.groups.filter((g) => !g.archived).map((group) => ({ kind: "group" as const, group })),
  ];
  const hiddenOf = (r: Row) => (r.kind === "agent" ? !!r.agent.hidden : !!r.group.hidden);
  const rows = all.filter((r) => withHidden || !hiddenOf(r));
  rows.sort((a, b) => Number(pinnedOf(b)) - Number(pinnedOf(a)) || activeAt(b) - activeAt(a));
  return { lead, rows, hidden: all.filter(hiddenOf).length };
}

function agentLine(a: TeamAgent, names: Map<string, string>): string {
  switch (a.activity.state) {
    case "needs_you":
      return t("等你处理");
    case "working":
      return t("工作中");
    case "waiting_on":
      return t("等待 {names}", { names: (a.activity.on ?? []).map((id) => names.get(id) ?? id).join("、") });
  }
  return a.preview || a.role || "";
}

function groupTitle(g: TeamGroup, names: Map<string, string>): string {
  return g.name || g.members.map((id) => names.get(id) ?? id).join("、");
}

function groupLine(g: TeamGroup, names: Map<string, string>): string {
  if (g.round.parked) return t("已暂停，等你继续");
  if (g.round.speaking) return t("{name} 正在发言", { name: names.get(g.round.speaking) ?? g.round.speaking });
  if (g.lastEvent) return eventText(g.lastEvent, names.get(g.lastEvent.from) ?? t("另一个 Agent"));
  return g.preview ?? "";
}

// The team's block in the sidebar: the agents and the group chats they meet
// in. Opening an agent opens its own conversation as a pane; opening a group
// fills the main area with that group's chat.
export function TeamRail({ hub, active, onAdoptPane }: { hub: HubPort; active: string; onAdoptPane: (rt: RuntimeView | Promise<RuntimeView>) => void }) {
  const { view, unavailable, team, shown } = useTeam(hub);
  const stage = useGroupStage();
  const [dialog, setDialog] = useState<{ kind: "agent" | "group"; id?: string } | null>(null);
  const [starting, setStarting] = useState(false);
  const [showHidden, setShowHidden] = useState(false);
  const [menu, setMenu] = useState<{ kind: "agent" | "group"; id: string; x: number; y: number } | null>(null);
  const menuRow = useRef<HTMLElement | null>(null);
  const list = useRef<HTMLUListElement>(null);
  const unread = useTeamUnread();
  const folded = useTeamFolded();
  const [more, setMore] = useState(false);
  const moreButton = useRef<HTMLButtonElement>(null);
  const moreMenu = useRef<HTMLDivElement>(null);
  useDismiss(more, moreButton, () => { setMore(false); moreButton.current?.focus(); }, moreMenu);
  useEffect(() => {
    if (more) moreMenu.current?.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus();
  }, [more]);
  useLayoutEffect(() => {
    const anchor = moreButton.current?.getBoundingClientRect();
    if (more && anchor && moreMenu.current) pinToViewport(moreMenu.current, anchor.right + 8, anchor.top - 7, 12);
  }, [more]);
  useReorderMotion(list, view ? teamRows(view, showHidden).rows.map((r) => (r.kind === "agent" ? r.agent.id : r.group.id)).join(" ") : "");
  useEffect(() => {
    if (!view) return;
    noteSeen([
      ...view.agents.map((a) => ({ id: a.id, at: a.lastActivityAt, looking: !!a.runtime && a.runtime === active && !stage })),
      ...view.groups.map((g) => ({ id: g.id, at: g.lastActivityAt, looking: stage === g.id })),
    ]);
  }, [view, active, stage]);
  if (unavailable || !view || !team) return null;
  const names = new Map(view.agents.map((a) => [a.id, a.name]));
  const { lead, rows, hidden } = teamRows(view, showHidden);
  const waiting = view.agents.filter((a) => a.activity.state === "needs_you").length;
  const readOnly = !!view.readOnly;
  const menuAt = (kind: "agent" | "group", id: string, ev: MouseEvent<HTMLElement>) => {
    if (readOnly) return;
    ev.preventDefault();
    menuRow.current = ev.currentTarget;
    setMenu({ kind, id, x: ev.clientX, y: ev.clientY });
  };
  const menuKey = (kind: "agent" | "group", id: string, ev: KeyboardEvent<HTMLElement>) => {
    if (readOnly || !(ev.key === "ContextMenu" || (ev.shiftKey && ev.key === "F10"))) return;
    ev.preventDefault();
    const box = ev.currentTarget.getBoundingClientRect();
    menuRow.current = ev.currentTarget;
    setMenu({ kind, id, x: box.right, y: box.top });
  };

  const openAgent = async (id: string) => {
    noteSeen([{ id, at: view.agents.find((a) => a.id === id)?.lastActivityAt, looking: true }]);
    try {
      closeGroupStage();
      await onAdoptPane(team.openAgent(id));
    } catch (e) {
      pushToast(reason(e));
    }
  };
  const bootstrap = async () => {
    setStarting(true);
    try {
      const next = await team.bootstrap("Chief", t("首席助理"));
      shown(next);
      const chief = next.agents.find((a) => a.main);
      if (chief) await openAgent(chief.id);
    } catch (e) {
      pushToast(reason(e));
    } finally {
      setStarting(false);
    }
  };
  const agentRow = (a: TeamAgent, main = false) => (
    <li key={a.id} data-key={a.id} className={main ? "team-row team-row--main" : "team-row"} data-state={a.activity.state} aria-current={!!a.runtime && a.runtime === active && !stage}
      data-unread={isUnread(unread, a.id, a.lastActivityAt) ? "" : undefined} data-hidden={a.hidden ? "" : undefined}>
      <button type="button" className="team-row-open" data-action-click="team.agent-open" data-action-contextmenu="team.menu" data-action-keydown="team.menu" data-target={a.id}
        onClick={() => void openAgent(a.id)} onContextMenu={(ev) => menuAt("agent", a.id, ev)} onKeyDown={(ev) => menuKey("agent", a.id, ev)}>
        <span className="team-row-mark"><AgentAvatar agent={a} size={main ? 32 : 28} /></span>
        <span className="team-row-text">
          <span className="team-row-name">{a.name}{isUnread(unread, a.id, a.lastActivityAt) && <>{" "}<UnreadDot /></>}</span>{" "}
          <span className="team-row-line">{agentLine(a, names)}</span>
        </span>
      </button>
      {a.pinned && <StudioIcon name="pin" className="team-row-pin" aria-label={t("已置顶")} />}
      {!readOnly && (
        <button type="button" className="team-row-more" data-action="team.agent-settings" data-target={a.id} aria-label={t("{name} 的设置", { name: a.name })}
          onClick={() => setDialog({ kind: "agent", id: a.id })}>
          <StudioIcon name="more" />
        </button>
      )}
    </li>
  );
  const groupRow = (g: TeamGroup) => (
    <li key={g.id} data-key={g.id} className="team-row" aria-current={stage === g.id}
      data-unread={isUnread(unread, g.id, g.lastActivityAt) ? "" : undefined} data-hidden={g.hidden ? "" : undefined}>
      <button type="button" className="team-row-open" data-action-click="team.group-open" data-action-contextmenu="team.menu" data-action-keydown="team.menu" data-target={g.id}
        onClick={() => { noteSeen([{ id: g.id, at: g.lastActivityAt, looking: true }]); openGroupStage(g.id); }}
        onContextMenu={(ev) => menuAt("group", g.id, ev)} onKeyDown={(ev) => menuKey("group", g.id, ev)}>
        <span className="team-row-mark"><GroupAvatar members={g.members.map((id) => view.agents.find((a) => a.id === id)).filter((a): a is TeamAgent => !!a)} /></span>
        <span className="team-row-text">
          <span className="team-row-name">{groupTitle(g, names)}{isUnread(unread, g.id, g.lastActivityAt) && <>{" "}<UnreadDot /></>}</span>{" "}
          <span className="team-row-line">{groupLine(g, names)}</span>
        </span>
      </button>
      {g.pinned && <StudioIcon name="pin" className="team-row-pin" aria-label={t("已置顶")} />}
      {!readOnly && (
        <button type="button" className="team-row-more" data-action="team.group-settings" data-target={g.id} aria-label={t("{name} 的设置", { name: groupTitle(g, names) })}
          onClick={() => setDialog({ kind: "group", id: g.id })}>
          <StudioIcon name="more" />
        </button>
      )}
    </li>
  );
  const menuTarget: MenuTarget | undefined = !menu
    ? undefined
    : menu.kind === "agent"
      ? (() => { const agent = view.agents.find((a) => a.id === menu.id); return agent && { kind: "agent" as const, agent }; })()
      : (() => { const group = view.groups.find((g) => g.id === menu.id); return group && { kind: "group" as const, group, title: groupTitle(group, names) }; })();
  const editing = dialog?.kind === "agent" ? view.agents.find((a) => a.id === dialog.id) : undefined;
  const editingGroup = dialog?.kind === "group" ? view.groups.find((g) => g.id === dialog.id) : undefined;
  const done = (next: TeamView) => {
    shown(next);
    setDialog(null);
  };

  return (
    <section className="team-rail" aria-label={t("团队")}>
      <div className="team-rail-head">
        <span className="team-rail-title">
          <RailSectionToggle title={t("团队")} label={folded ? t("展开团队") : t("收起团队")}
            folded={folded} onToggle={() => setTeamFolded(!folded)} section="team" />
          {folded && waiting > 0 && <span className="team-rail-waiting">{t("{n} 个在等你", { n: waiting })}</span>}
        </span>
        {!readOnly && view.agents.length > 0 && (
          <>
            <button type="button" data-action="team.agent-new" aria-label={t("新建 Agent")} title={t("新建 Agent")} onClick={() => setDialog({ kind: "agent" })}>
              <StudioIcon name="plus" />
            </button>
            <button ref={moreButton} type="button" data-action="team.rail-menu" aria-label={t("团队的更多操作")} title={t("更多操作")}
              aria-haspopup="menu" aria-expanded={more} onClick={() => setMore((v) => !v)}>
              <StudioIcon name="more" />
            </button>
          </>
        )}
      </div>
      {more && createPortal(
        <div ref={moreMenu} className="session-pop" role="menu" aria-label={t("团队的更多操作")} data-action-keydown="team.rail-menu" onKeyDown={workspaceMenuKeys}>
          <div className="session-pop-head"><b>{t("团队")}</b></div>
          <div className="session-pop-group">
            <button type="button" role="menuitem" data-action="team.group-new" disabled={view.agents.length < 2}
              onClick={() => { setMore(false); setDialog({ kind: "group" }); }}>
              <StudioIcon name="chat" /><span>{t("新建群聊")}</span>
            </button>
          </div>
        </div>,
        document.body,
      )}
      {!folded && readOnly && <p className="team-note">{t("团队正在另一个 Reasonix 窗口中编辑，这里只能查看")}</p>}
      {folded ? null : view.agents.length === 0 ? (
        !readOnly && (
          <div className="team-welcome">
            <h3>{t("组建你的团队")}</h3>
            <p>{t("Chief 是你的主 Agent：它可以按你的要求创建其他 Agent 和群聊，并把工作分派给它们。每个 Agent 都有自己的长期对话。")}</p>
            <button type="button" className="team-primary" data-action="team.bootstrap" disabled={starting} onClick={() => void bootstrap()}>
              {starting ? t("正在创建…") : t("创建 Chief")}
            </button>
          </div>
        )
      ) : (
        <ul className="team-rows" ref={list}>
          {lead && agentRow(lead, true)}
          {rows.map((r) => (r.kind === "agent" ? agentRow(r.agent) : groupRow(r.group)))}
        </ul>
      )}
      {!folded && hidden > 0 && (
        <button type="button" className="team-hidden-toggle" data-action="team.show-hidden" aria-pressed={showHidden} onClick={() => setShowHidden((v) => !v)}>
          {showHidden ? t("收起已隐藏的 {n} 个", { n: hidden }) : t("已隐藏 {n} 个", { n: hidden })}
        </button>
      )}
      {menu && menuTarget && (
        <TeamMenu team={team} view={view} target={menuTarget} at={{ x: menu.x, y: menu.y }} anchor={menuRow}
          unread={isUnread(unread, menu.id, menuTarget.kind === "agent" ? menuTarget.agent.lastActivityAt : menuTarget.group.lastActivityAt)}
          lead={menuTarget.kind === "agent" && menuTarget.agent === lead}
          onClose={() => setMenu(null)} onSettings={() => setDialog({ kind: menu.kind, id: menu.id })} onShown={shown} onOpened={(rt) => { closeGroupStage(); return onAdoptPane(rt); }} />
      )}
      {dialog?.kind === "agent" && (dialog.id === undefined || editing) && (
        <AgentDialog team={team} agent={editing} onClose={() => setDialog(null)} onSaved={done} />
      )}
      {dialog?.kind === "group" && (dialog.id === undefined || editingGroup) && (
        <GroupDialog team={team} agents={view.agents} group={editingGroup} onClose={() => setDialog(null)} onSaved={done} />
      )}
    </section>
  );
}
