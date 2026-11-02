import { useEffect, useState } from "react";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { HubPort, RuntimeView } from "../../port/hub";
import type { TeamAgent, TeamExchange, TeamView } from "../../port/team";
import { useTeam } from "../../state/team";
import { closeSeatDrawer, useSeatDrawer } from "../../state/teamDrawer";
import { useGroupStage } from "../../state/teamStage";
import { pushToast } from "../../state/toasts";
import { StudioIcon } from "../StudioIcon";
import { since } from "../wallet";
import { AgentAvatar } from "./AgentAvatar";
import { AgentDialog } from "./AgentDialog";
import { PERMISSIONS } from "./permissions";
import "../../styles/team-drawer.css";

const STATES: Record<TeamExchange["state"], string> = {
  queued: "排队中",
  delivered: "进行中",
  waiting_children: "等待下游",
  replied: "已回复",
  failed: "失败",
  stopped: "已停止",
  interrupted: "已中断",
};

const SHOWN_EXCHANGES = 8;

function usageLine(a: TeamAgent): string {
  const u = a.usage;
  if (!u || u.requests === 0) return t("还没有用量");
  return t("{n} 次请求 · 输入 {prompt} · 输出 {completion} tokens · 缓存命中 {hit}%", {
    n: u.requests,
    prompt: u.promptTokens.toLocaleString(),
    completion: u.completionTokens.toLocaleString(),
    hit: u.promptTokens ? Math.round((u.cacheHitTokens / u.promptTokens) * 100) : 0,
  });
}

// The focused agent's details beside its conversation: who it is, what it may
// do, where it works, what it has spent and whom it has been talking to. What
// the person can change takes effect the way its endpoint does; the folder is
// read when the agent's conversation next opens.
export function SeatDrawer({ hub, seat, root, onAdoptPane }: {
  hub: HubPort;
  seat?: RuntimeView["team"];
  root?: string;
  onAdoptPane: (rt: RuntimeView | Promise<RuntimeView>) => void;
}) {
  const open = useSeatDrawer();
  const stage = useGroupStage();
  const { view, team, shown } = useTeam(hub);
  const [book, setBook] = useState<TeamExchange[]>([]);
  const [editing, setEditing] = useState(false);
  const [moved, setMoved] = useState(false);
  const agentId = seat?.agent;
  useEffect(() => {
    if (!open || !team || !agentId) return;
    let live = true;
    void team.exchanges(agentId).then((x) => live && setBook(x), () => live && setBook([]));
    return () => { live = false; };
  }, [open, team, agentId, view?.revision]);
  useEffect(() => setMoved(false), [agentId]);
  const agent = view?.agents.find((a) => a.id === agentId);
  if (!open || stage || !agent || !team || !view) return null;

  const names = new Map(view.agents.map((a) => [a.id, a.name]));
  const nameOf = (id: string) => (id === "user" ? t("你") : names.get(id) ?? t("另一个 Agent"));
  const write = async (run: () => Promise<TeamView>) => {
    try {
      shown(await run());
    } catch (e) {
      pushToast(reason(e));
    }
  };
  const pickFolder = async () => {
    const folder = await hub.pickFolder().catch(() => null);
    if (!folder) return;
    await write(() => team.updateAgent(agent.id, { folder }));
    setMoved(true);
  };
  const openOther = async (id: string) => {
    if (id === "user") return;
    try {
      await onAdoptPane(team.openAgent(id));
    } catch (e) {
      pushToast(reason(e));
    }
  };
  const recent = [...book].sort((a, b) => b.at.localeCompare(a.at)).slice(0, SHOWN_EXCHANGES);

  return (
    <aside className="team-drawer" aria-label={t("{name} 的详情", { name: agent.name })}>
      <header className="team-drawer-head">
        <AgentAvatar agent={agent} size={56} />
        <div className="team-drawer-title">
          <h2>{agent.name}</h2>
          {agent.role && <p>{agent.role}</p>}
        </div>
        {!view.readOnly && (
          <button type="button" data-action="team.agent-settings" data-target={agent.id} aria-label={t("{name} 的设置", { name: agent.name })} onClick={() => setEditing(true)}>
            <StudioIcon name="settings" />
          </button>
        )}
        <button type="button" data-action="team.drawer-close" aria-label={t("关闭详情")} onClick={closeSeatDrawer}>
          <StudioIcon name="close" />
        </button>
      </header>
      <dl className="team-drawer-facts">
        <dt>{t("用量")}</dt>
        <dd>{usageLine(agent)}</dd>
        <dt><label htmlFor="team-drawer-permission">{t("权限")}</label></dt>
        <dd>
          <select id="team-drawer-permission" data-action-change="team.drawer-permission" data-target={agent.id} value={agent.permission ?? ""} disabled={!!view.readOnly}
            onChange={(ev) => void write(() => team.updateAgent(agent.id, { permission: ev.target.value }))}>
            {PERMISSIONS.map(([value, label]) => <option key={value} value={value}>{t(label)}</option>)}
          </select>
        </dd>
        <dt>{t("工作文件夹")}</dt>
        <dd>
          <code title={root}>{root || agent.folder || t("Agent 自己的文件夹")}</code>
          {!view.readOnly && (
            <button type="button" className="team-drawer-link" data-action="team.drawer-folder" data-target={agent.id} onClick={() => void pickFolder()}>
              {t("更改…")}
            </button>
          )}
          {moved && <p className="team-note">{t("下次打开这个 Agent 时生效")}</p>}
        </dd>
        {agent.instructions && (
          <>
            <dt>{t("指令")}</dt>
            <dd><p className="team-drawer-instructions">{agent.instructions}</p></dd>
          </>
        )}
      </dl>
      <section className="team-drawer-section">
        <h3>{t("往来")}</h3>
        {recent.length === 0 ? (
          <p className="team-note">{t("还没有和其他 Agent 的往来")}</p>
        ) : (
          <ul className="team-exchanges" aria-label={t("往来")}>
            {recent.map((x) => {
              const outgoing = x.from === agent.id;
              const other = outgoing ? x.to : x.from;
              return (
                <li key={x.id}>
                  <button type="button" data-action="team.exchange-open" data-target={other} disabled={other === "user"} onClick={() => void openOther(other)}>
                    <span className="team-exchange-head">
                      <b>{outgoing ? t("发给 {name}", { name: nameOf(other) }) : t("来自 {name}", { name: nameOf(other) })}</b>
                      <span data-state={x.state}>{t(STATES[x.state])}</span>
                      <time dateTime={x.at}>{since(x.at, Date.now())}</time>
                    </span>
                    {x.preview && <span className="team-exchange-preview">{x.preview}</span>}
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </section>
      {editing && <AgentDialog team={team} agent={agent} onClose={() => setEditing(false)} onSaved={(next) => { shown(next); setEditing(false); }} />}
    </aside>
  );
}
