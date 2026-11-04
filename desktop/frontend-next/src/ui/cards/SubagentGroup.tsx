import { createContext, Fragment, useContext, useState, type ReactNode } from "react";
import { t } from "../../i18n";
import { elapsed } from "../../i18n/format";
import type { Item } from "../../state/session";
import type { AgentPort } from "../../port/port";

type Stopper = Partial<Pick<AgentPort, "stopToolCall">>;
import { delegateStatus, isDelegation, type DelegateStatus } from "../delegation";
import { activityVerb } from "../activityEarlier";
import { shortArgs } from "../args";
import { useWatched } from "../Cost";
import { RMark } from "../RMark";
import { StudioIcon } from "../StudioIcon";

type Task = Extract<Item, { t: "tool" }>;

/** Opens a delegate's own run in the workbench; resolves false when it cannot. */
export const AgentRunOpener = createContext<((callId: string) => Promise<boolean>) | null>(null);

const isAgent = (item: Item): item is Task => item.t === "tool" && !item.tool.parentId && isDelegation(item.tool);

/** A run of work, split where two or more delegates were sent out side by side:
 *  those read as one group of their own, everything else as the execution group. */
export function workLanes(items: Item[]): { agents: Task[] | null; items: Item[] }[] {
  const lanes: { agents: Task[] | null; items: Item[] }[] = [];
  let at = 0;
  while (at < items.length) {
    let end = at;
    while (end < items.length && isAgent(items[end])) end++;
    if (end - at >= 2) {
      lanes.push({ agents: items.slice(at, end) as Task[], items: [] });
      at = end;
      continue;
    }
    const last = lanes[lanes.length - 1];
    const take = items.slice(at, Math.max(end, at + 1));
    if (last && !last.agents) last.items.push(...take);
    else lanes.push({ agents: null, items: take });
    at += take.length;
  }
  return lanes;
}

export function WorkLanes({ items, port, draw }: { items: Item[]; port?: Stopper; draw: (items: Item[]) => ReactNode }) {
  return <>{workLanes(items).map((lane, i) => (lane.agents
    ? <SubagentGroup key={lane.agents[0].id} tasks={lane.agents} port={port} />
    : <Fragment key={lane.items[0]?.id ?? i}>{draw(lane.items)}</Fragment>))}</>;
}

const HUES = 3;
// The same name always gets the same colour, wherever the delegate is drawn.
export function agentHue(name: string): number {
  let h = 0;
  for (const ch of name) h = (h * 31 + ch.codePointAt(0)!) >>> 0;
  return h % HUES;
}

export const nameOf = (x: Task) => x.tool.profile?.name || "task";

export const AgentAvatar = ({ name }: { name: string }) => (
  <span className="agent-avatar" data-hue={agentHue(name)} aria-hidden="true">
    <svg viewBox="0 0 16 16"><path d="M3.5 12.5l6-6M9.5 3l.8 2.2 2.2.8-2.2.8-.8 2.2-.8-2.2L6.5 6l2.2-.8z" /></svg>
  </span>
);

export function SubagentGroup({ tasks, port }: { tasks: Task[]; port?: Stopper }) {
  const status = tasks.map((x) => delegateStatus(x.running, x.tool));
  const running = status.filter((s) => s === "running").length;
  const failed = status.filter((s) => s === "failed" || s === "refused").length;
  const done = status.filter((s) => s === "done").length;
  const settled = running === 0;
  const [touched, setTouched] = useState<boolean | null>(null);
  const open = touched ?? !(settled && failed === 0);
  const total = Math.max(0, ...tasks.map((x) => x.tool.durationMs ?? 0));
  const stop = port?.stopToolCall ? (x: Task) => void port.stopToolCall!(x.tool.id!).catch(() => {}) : undefined;
  const closed = settled && !open;
  return (
    <section className="agent-group" data-settled={settled ? "" : undefined} aria-label={t("subagent.group.parallel", { n: tasks.length })}>
      <header className="agent-group-head">
        <StudioIcon name="branch" className="agent-group-icon" />
        <b>{closed ? t("subagent.group.count", { n: tasks.length }) : t("subagent.group.parallel", { n: tasks.length })}</b>
        {closed ? (
          <span className="agent-group-sum">{failed === 0 ? t("subagent.group.all_delivered", { n: done }) : t("subagent.group.delivered", { n: done })}{total > 0 && ` · ${elapsed(total)}`}</span>
        ) : (
          <>
            {running > 0 && <span className="agent-pill" data-tone="run">{t("subagent.group.running", { n: running })}</span>}
            {failed > 0 && <span className="agent-pill" data-tone="err">{t("subagent.group.failed", { n: failed })}</span>}
            {done > 0 && <span className="agent-group-done">{t("subagent.group.delivered", { n: done })}</span>}
          </>
        )}
        <span className="agent-group-fill" />
        {running > 0 && stop && (
          <button type="button" className="agent-quiet" data-action="agent.stop-all" onClick={() => tasks.filter((x) => x.running && x.tool.id).forEach(stop)}>{t("common.stop_all")}</button>
        )}
        {settled && (
          <button type="button" className="agent-quiet" data-action="agent.group-fold" aria-expanded={open} onClick={() => setTouched(!open)}>{open ? t("common.collapse") : t("common.expand")}</button>
        )}
      </header>
      {open && (
        <div className="agent-group-body">
          {tasks.map((x, i) => <AgentRow key={x.id} task={x} status={status[i]} onStop={stop} />)}
        </div>
      )}
    </section>
  );
}

function AgentRow({ task, status, onStop }: { task: Task; status: DelegateStatus; onStop?: (x: Task) => void }) {
  const open = useContext(AgentRunOpener);
  const watched = useWatched(status === "running");
  const name = nameOf(task);
  const steps = task.children.length;
  const now = status === "running" ? task.children[task.children.length - 1] : undefined;
  const took = task.tool.durationMs ? elapsed(task.tool.durationMs) : "";
  const reading = status === "running" ? [t("subagent.step", { n: Math.max(steps, 1) }), watched ? elapsed(watched) : ""]
    : status === "done" ? [t("subagent.state.delivered"), steps ? t("subagent.steps", { n: steps }) : "", took]
      : [status === "refused" ? t("subagent.state.not_run") : t("subagent.state.failed"), took];
  return (
    <div className="agent-row" data-status={status} data-call={task.tool.id || undefined}>
      <div className="agent-row-line">
        <AgentAvatar name={name} />
        <span className="agent-who">
          <b>{name}</b>
          {task.tool.profile?.label && <span title={task.tool.profile.label}>{task.tool.profile.label}</span>}
        </span>
        <span className="agent-reading">
          {status === "running" && <RMark className="plan-spin" />}
          {reading.filter(Boolean).join(" · ")}
          {status === "done" && <StudioIcon name="check" />}
          {(status === "failed" || status === "refused") && <StudioIcon name="close" />}
        </span>
        {status === "running" && onStop && task.tool.id && (
          <button type="button" className="agent-quiet" data-action="tool.stop" onClick={() => onStop(task)}>{t("common.stop")}</button>
        )}
        {open && task.tool.id && (
          <button type="button" className="agent-open" data-action="agent.open" data-target={task.id} aria-label={t("subagent.open")} title={t("subagent.open")}
            onClick={() => void open(task.tool.id!)}>
            <svg viewBox="0 0 16 16" aria-hidden="true"><rect x="2" y="3" width="12" height="10" rx="1.5" /><path d="M10 3v10" /></svg>
          </button>
        )}
      </div>
      {now && (
        <p className="agent-row-note">
          {t("subagent.doing", { what: t(activityVerb(now.name) ?? "subagent.run") })} <code>{shortArgs(now.args ?? "")}</code>
        </p>
      )}
      {(status === "failed" || status === "refused") && task.tool.err && <p className="agent-row-note" data-tone="err">{task.tool.err}</p>}
    </div>
  );
}
