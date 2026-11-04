import { t } from "../i18n";
import { elapsed } from "../i18n/format";
import { delegateStatus, delegateStatusLabel, type DelegateStatus } from "./delegation";
import { agentsIn } from "./panels/derive";
import type { Task } from "./panels/Agents";
import { AgentAvatar, nameOf } from "./cards/SubagentGroup";
import { useWatched } from "./Cost";
import { RMark } from "./RMark";
import { StudioIcon } from "./StudioIcon";

/** The session's delegates as the run rail's reading opens them: who, sent to
 *  do what, and where each stands; a row opens that run. */
export function AgentDeck({ tasks, onOpen, onStop }: { tasks: Task[]; onOpen: (taskId: string) => void; onStop?: (callId: string) => void }) {
  const live = tasks.filter((x) => x.running);
  return (
    <div className="agent-deck">
      <header className="agent-deck-head">
        <b>{t("subagent.deck.title")}</b>
        <span>{t("subagent.deck.count", { live: agentsIn(live), total: agentsIn(tasks) })}</span>
      </header>
      {tasks.map((x) => <DeckRow key={x.id} task={x} status={delegateStatus(x.running, x.tool)} onOpen={onOpen} />)}
      <footer className="agent-deck-foot">
        <span>{t("subagent.deck.hint")}</span>
        {live.length > 0 && onStop && (
          <button type="button" className="agent-quiet" data-action="agent.stop-all" onClick={() => live.forEach((x) => x.tool.id && onStop(x.tool.id))}>{t("common.stop_all")}</button>
        )}
      </footer>
    </div>
  );
}

function DeckRow({ task, status, onOpen }: { task: Task; status: DelegateStatus; onOpen: (taskId: string) => void }) {
  const watched = useWatched(status === "running");
  const name = nameOf(task);
  const took = task.tool.durationMs ? elapsed(task.tool.durationMs) : "";
  const reading = status === "running" ? [t("subagent.step", { n: Math.max(task.children.length, 1) }), watched ? elapsed(watched) : ""]
    : [delegateStatusLabel(status), took];
  return (
    <button type="button" className="ag agent-deck-row" data-status={status} data-action="agent.open" data-target={task.id}
      aria-label={t("subagent.deck.open_full", { name })} title={status === "failed" ? task.tool.err : undefined} onClick={() => onOpen(task.id)}>
      <AgentAvatar name={name} />
      <span className="agent-deck-who">
        <b>{name}</b>
        {task.tool.profile?.label && <span>{task.tool.profile.label}</span>}
      </span>
      <span className="agent-deck-state">
        {status === "running" && <RMark className="plan-spin" />}
        {reading.filter(Boolean).join(" · ")}
      </span>
      <StudioIcon name="chevron" className="agent-deck-go" />
    </button>
  );
}
