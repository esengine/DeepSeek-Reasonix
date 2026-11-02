import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import type { HubPort, RuntimeView } from "../../port/hub";
import type { GroupLine, TeamAgent, TeamGroup, TeamView } from "../../port/team";
import { useTeam } from "../../state/team";
import { closeGroupStage, useGroupStage } from "../../state/teamStage";
import { pushToast } from "../../state/toasts";
import { StudioIcon } from "../StudioIcon";
import { AgentAvatar, GroupAvatar } from "./AgentAvatar";
import { GroupDialog } from "./GroupDialog";
import { eventText } from "./groupEvents";
import "../../styles/team.css";
import "../../styles/team-stage.css";

const EVERYONE = "*";
// How many lines one read of the log brings: the latest on open, then the
// ones before, a page at a time, on request.
const PAGE = 200;

// One group's chat over the panes: its log, who is speaking or working, and a
// composer whose mentions are member ids rather than names typed out.
export function GroupStage({ hub, active, onAdoptPane }: { hub: HubPort; active: string; onAdoptPane: (rt: RuntimeView | Promise<RuntimeView>) => void }) {
  const id = useGroupStage();
  const { view, team, shown } = useTeam(hub);
  const opened = useRef(active);
  useEffect(() => {
    if (id) opened.current = active;
  }, [id]);
  useEffect(() => {
    if (id && active !== opened.current) closeGroupStage();
  }, [active, id]);
  const group = view?.groups.find((g) => g.id === id);
  if (!id || !view || !team || !group) return null;
  return <GroupChat key={group.id} group={group} view={view} team={team} onAdoptPane={onAdoptPane} onSaved={shown} />;
}

function GroupChat({ group, view, team, onAdoptPane, onSaved }: {
  group: TeamGroup;
  view: TeamView;
  team: NonNullable<ReturnType<HubPort["team"]>>;
  onAdoptPane: (rt: RuntimeView | Promise<RuntimeView>) => void;
  onSaved: (v: TeamView) => void;
}) {
  const [lines, setLines] = useState<GroupLine[]>([]);
  const [text, setText] = useState("");
  const [mentions, setMentions] = useState<string[]>([]);
  const [picking, setPicking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [settings, setSettings] = useState(false);
  const [older, setOlder] = useState(false);
  const [paging, setPaging] = useState(false);
  const log = useRef<HTMLDivElement>(null);
  const following = useRef(true);
  const anchor = useRef<number | null>(null);
  const agents = new Map(view.agents.map((a) => [a.id, a]));
  const nameOf = (id: string) => (id === "user" ? t("你") : id === "scheduler" ? t("定时任务") : agents.get(id)?.name ?? t("另一个 Agent"));
  const members = group.members.map((m) => agents.get(m)).filter((a): a is TeamAgent => !!a);
  const lastSeq = lines.at(-1)?.seq ?? 0;

  useEffect(() => {
    let live = true;
    const after = lastSeq;
    if (after > 0 && (group.lastSeq ?? 0) <= after) return;
    void team.groupLog(group.id, after ? { after } : { limit: PAGE }).then(
      (more) => {
        if (!live) return;
        if (!after) setOlder(more.length === PAGE && (more[0]?.seq ?? 1) > 1);
        setLines((cur) => [...cur, ...more.filter((l) => l.seq > (cur.at(-1)?.seq ?? 0))]);
      },
      (e) => live && pushToast(reason(e)),
    );
    return () => { live = false; };
  }, [group.id, group.lastSeq, view.revision]);
  // A new line is followed only by someone already at the end; reading older
  // lines is never interrupted by one arriving.
  useLayoutEffect(() => {
    const el = log.current;
    if (el && anchor.current === null && following.current) el.scrollTop = el.scrollHeight;
  }, [lines.at(-1)?.seq]);
  // Lines paged in above keep the ones on screen where they were.
  useLayoutEffect(() => {
    if (anchor.current === null || !log.current) return;
    log.current.scrollTop += log.current.scrollHeight - anchor.current;
    anchor.current = null;
  }, [lines[0]?.seq]);
  const loadOlder = async () => {
    const first = lines[0]?.seq;
    if (!first || paging) return;
    setPaging(true);
    try {
      const page = await team.groupLog(group.id, { before: first, limit: PAGE });
      anchor.current = log.current?.scrollHeight ?? 0;
      setOlder(page.length === PAGE && (page[0]?.seq ?? 1) > 1);
      setLines((cur) => [...page.filter((l) => l.seq < (cur[0]?.seq ?? Infinity)), ...cur]);
    } catch (e) {
      pushToast(reason(e));
    } finally {
      setPaging(false);
    }
  };

  const send = async () => {
    const body = text.trim();
    if (!body || busy) return;
    setBusy(true);
    following.current = true;
    try {
      await team.postGroup(group.id, body, mentions.filter((m) => m !== EVERYONE), mentions.includes(EVERYONE));
      setText("");
      setMentions([]);
    } catch (e) {
      pushToast(reason(e));
    } finally {
      setBusy(false);
    }
  };
  const openMember = async (agent: string) => {
    try {
      await onAdoptPane(team.openMember(group.id, agent));
    } catch (e) {
      pushToast(reason(e));
    }
  };
  const mention = (m: string) => {
    setMentions((cur) => (cur.includes(m) ? cur : [...cur, m]));
    setText((cur) => cur.replace(/@$/, ""));
    setPicking(false);
  };
  const speaking = group.round.speaking;
  const stop = (m: string) => !view.readOnly && (
    <button type="button" data-action="team.member-stop" data-target={m} aria-label={t("停止 {name}", { name: nameOf(m) })}
      onClick={() => void team.stopMember(group.id, m).catch((e) => pushToast(reason(e)))}>
      <StudioIcon name="stop" />
    </button>
  );
  const waiting = members.filter((m) => m.activity.state === "needs_you");
  const lastPaused = [...lines].reverse().find((l) => l.event === "paused" || l.event === "resumed");

  return (
    <section className="team-stage" aria-label={group.name || t("群聊")}>
      <header className="team-stage-head">
        <GroupAvatar members={members} size={32} />
        <div className="team-stage-title">
          <h2>{group.name || members.map((m) => m.name).join("、")}</h2>
          {group.notice && <p>{group.notice}</p>}
        </div>
        {!view.readOnly && (
          <button type="button" data-action="team.group-settings" data-target={group.id} aria-label={t("群聊设置")} onClick={() => setSettings(true)}>
            <StudioIcon name="settings" />
          </button>
        )}
        <button type="button" data-action="team.group-close" aria-label={t("关闭群聊")} onClick={closeGroupStage}>
          <StudioIcon name="close" />
        </button>
      </header>
      <div className="team-stage-log" role="log" aria-live="polite" ref={log} data-action="team.group-scroll" data-target={group.id}
        onScroll={(ev) => { const el = ev.currentTarget; following.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48; }}>
        {older && (
          <button type="button" className="sessmore" data-action="team.group-older" data-target={group.id} disabled={paging} onClick={() => void loadOlder()}>
            {t("加载更早的消息")}
          </button>
        )}
        {lines.length === 0 && <p className="team-note">{t("还没有消息。发一条消息，管理员会先回答；用 @ 可以直接叫某位成员。")}</p>}
        {lines.map((l) =>
          l.kind === "post" ? (
            <article key={l.seq} className="team-line" data-from={l.from === "user" ? "user" : "agent"}>
              {l.from !== "user" && agents.get(l.from) && <AgentAvatar agent={agents.get(l.from)!} size={28} badges={false} />}
              <div className="team-line-body">
                <header>
                  <b>{nameOf(l.from)}</b>
                  {l.mentions && <span className="team-line-to">{"→ " + l.mentions.map(nameOf).join("、")}</span>}
                  {l.late && <span className="team-line-tag">{t("迟到的回复")}</span>}
                  {l.schedule && <span className="team-line-tag">{t("定时任务")}</span>}
                </header>
                <p>{l.text}</p>
                {!!l.length && <p className="team-note">{t("这条发言很长，群里只保留了前一部分")}</p>}
              </div>
            </article>
          ) : (
            <div key={l.seq} className="team-event" data-event={l.event}>
              <span>{eventText(l, nameOf(l.from))}</span>
              {l.event === "failed" && agents.has(l.from) && (
                <button type="button" data-action="team.member-open" data-target={l.from} aria-label={t("查看 {name} 的对话", { name: nameOf(l.from) })}
                  onClick={() => void openMember(l.from)}>
                  {t("查看")}
                </button>
              )}
              {l.event === "paused" && l === lastPaused && !!group.round.parked && !view.readOnly && (
                <button type="button" data-action="team.group-continue" data-target={group.id} onClick={() => void team.continueGroup(group.id).catch((e) => pushToast(reason(e)))}>
                  {t("继续")}
                </button>
              )}
            </div>
          ),
        )}
      </div>
      {(speaking || group.round.working.length > 0 || waiting.length > 0) && (
        <div className="team-dock" role="status">
          {waiting.map((m) => (
            <button key={m.id} type="button" className="team-dock-ask" data-action="team.member-open" data-target={m.id} onClick={() => void openMember(m.id)}>
              {t("{name} 等你处理", { name: m.name })}
            </button>
          ))}
          {speaking && (
            <span className="team-dock-item">
              {t("{name} 正在发言", { name: nameOf(speaking) })}
              {stop(speaking)}
            </span>
          )}
          {group.round.working.map((m) => (
            <span key={m} className="team-dock-item">
              {t("{names} 在后台工作", { names: nameOf(m) })}
              {stop(m)}
            </span>
          ))}
        </div>
      )}
      {!view.readOnly && (
        <form className="team-compose" data-action-submit="team.group-post" data-target={group.id} onSubmit={(ev) => { ev.preventDefault(); void send(); }}>
          {mentions.length > 0 && (
            <div className="team-chips">
              {mentions.map((m) => (
                <button key={m} type="button" data-action="team.mention-remove" data-value={m} onClick={() => setMentions((cur) => cur.filter((x) => x !== m))}>
                  {"@" + (m === EVERYONE ? t("所有人") : nameOf(m))} ×
                </button>
              ))}
            </div>
          )}
          {picking && (
            <ul className="team-picker" role="listbox" aria-label={t("提到成员")}>
              {[...members.map((m) => [m.id, m.name] as const), [EVERYONE, t("所有人")] as const].map(([value, label]) => (
                <li key={value}>
                  <button type="button" role="option" aria-selected={false} data-action="team.mention-pick" data-value={value} onClick={() => mention(value)}>
                    {"@" + label}
                  </button>
                </li>
              ))}
            </ul>
          )}
          <textarea data-action-change="team.group-text" aria-label={t("发到群聊")} placeholder={t("发消息，输入 @ 提到成员")} value={text} disabled={busy}
            data-action-keydown="team.group-post"
            onKeyDown={(ev) => {
              if (ev.key === "Enter" && !ev.shiftKey && !ev.nativeEvent.isComposing) {
                ev.preventDefault();
                void send();
              }
              if (ev.key === "Escape") setPicking(false);
            }}
            onChange={(ev) => {
              setText(ev.target.value);
              setPicking(ev.target.value.endsWith("@"));
            }} />
          <button type="submit" className="team-primary" disabled={busy || !text.trim()}>
            {t("发送")}
          </button>
        </form>
      )}
      {settings && (
        <GroupDialog team={team} agents={view.agents} group={group} onClose={() => setSettings(false)}
          onSaved={(next) => { onSaved(next); setSettings(false); if (!next.groups.some((g) => g.id === group.id)) closeGroupStage(); }} />
      )}
    </section>
  );
}
