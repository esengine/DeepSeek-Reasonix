import { t } from "../i18n";
import type { ProcessEntry } from "../port/process";
import { jobElapsed } from "./panels/jobElapsed";

function stateLabel(state: string): string {
  switch (state) {
    case "pending": return t("subagent.process.state.pending");
    case "queued": return t("subagent.process.state.queued");
    case "running": return t("subagent.process.state.running");
    case "waiting": return t("subagent.process.state.waiting");
    case "completed": return t("subagent.process.state.completed");
    case "failed": return t("subagent.process.state.failed");
    case "stopped": return t("subagent.process.state.stopped");
    case "interrupted": return t("subagent.process.state.interrupted");
    case "skipped": return t("subagent.process.state.skipped");
    default: return state;
  }
}

function causeLabel(cause: string): string {
  switch (cause) {
    case "slots": return t("subagent.process.cause.slots");
    case "writers": return t("subagent.process.cause.writers");
    case "claim": return t("subagent.process.cause.claim");
    case "held": return t("subagent.process.cause.held");
    case "adopted": return t("subagent.process.cause.adopted");
    case "budget": return t("subagent.process.cause.budget");
    case "parked": return t("subagent.process.cause.parked");
    default: return cause;
  }
}

function kindLabel(kind: string): string {
  switch (kind) {
    case "subagent": return t("subagent.process.kind.subagent");
    case "job": return t("subagent.process.kind.job");
    default: return kind;
  }
}

/** What this session has running now, one row per process. State and cause are
 *  the kernel's words for it; a value this page has no wording for is shown as
 *  it came rather than guessed at. */
export function ProcessDeck({ rows, error }: { rows: ProcessEntry[]; error: string }) {
  if (error) return <div className="empty" role="alert">{error}</div>;
  if (rows.length === 0) return <div className="empty">{t("subagent.process.empty")}</div>;
  return (
    <div className="job-deck">
      {rows.map((p) => (
        <div className="job-deck-row" key={p.id} data-status={p.state}>
          <div className="job-deck-line">
            <b className="job-deck-name">{p.name}</b>
            <span className="job-deck-time">{[kindLabel(p.kind), stateLabel(p.state), jobElapsed(Date.now() - p.startedAt)].join(" · ")}</span>
          </div>
          {p.cause && <span className="job-deck-tail">{causeLabel(p.cause)}</span>}
        </div>
      ))}
    </div>
  );
}
