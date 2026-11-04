import { useEffect, useState } from "react";
import { t } from "../i18n";
import type { JobEntry } from "../port/port";
import { JobName, jobName } from "./panels/JobName";
import { jobElapsed } from "./panels/jobElapsed";

const ENDED: Record<string, string> = { done: "common.done_2", failed: "common.failed", killed: "plan.stopped", interrupted: "common.interrupted" };

/** The run rail's background jobs: the running ones first, then those that
 *  ended, each with how long it took, what it runs and the last line it wrote. */
export function JobDeck({ jobs, onCancel, onView, onClear }: {
  jobs: JobEntry[];
  onCancel?: (id: string) => Promise<void>;
  onView?: (id: string) => void;
  onClear?: (ids: string[]) => void;
}) {
  const [, tick] = useState(0);
  const [armed, setArmed] = useState("");
  const [stopping, setStopping] = useState<ReadonlySet<string>>(new Set());
  const live = jobs.filter((j) => j.status === "running");
  const ended = jobs.filter((j) => j.status !== "running");
  useEffect(() => {
    if (!armed) return;
    const timer = setTimeout(() => setArmed(""), 3000);
    return () => clearTimeout(timer);
  }, [armed]);
  useEffect(() => {
    if (live.length === 0) return;
    const timer = setInterval(() => tick((v) => v + 1), 1000);
    return () => clearInterval(timer);
  }, [live.length]);
  const stop = (ids: string[]) => {
    setArmed("");
    setStopping((was) => new Set([...was, ...ids]));
    for (const id of ids) void onCancel?.(id).finally(() => setStopping((was) => { const next = new Set(was); next.delete(id); return next; }));
  };
  const row = (j: JobEntry) => {
    const running = j.status === "running";
    const took = running ? jobElapsed(Date.now() - j.startedAt) : j.endedAt ? jobElapsed(j.endedAt - j.startedAt) : "";
    const command = j.detail?.trim() || (j.description?.trim() ? j.label : "");
    return (
      <div className="job-deck-row" key={j.id} data-status={j.status}>
        <div className="job-deck-line">
          <JobName job={j}><b className="job-deck-name" tabIndex={0}>{jobName(j)}</b></JobName>
          <span className="job-deck-time">{running ? took : [took, t(ENDED[j.status] ?? "common.done_2")].filter(Boolean).join(" · ")}</span>
        </div>
        {command && <code className="job-deck-cmd">{command}</code>}
        {j.tail && <span className="job-deck-tail">{j.tail}</span>}
        <div className="job-deck-acts">
          {!running && j.exitCode != null && <span className="job-deck-exit" data-ok={j.exitCode === 0 ? "" : undefined}>{t("panels.exit_code", { code: j.exitCode })}</span>}
          {onView && <button type="button" className="agent-quiet" data-action="job.view" data-target={j.id} aria-label={t("panels.view_output_cmd", { cmd: jobName(j) })} onClick={() => onView(j.id)}>{t("panels.view_output")}</button>}
          {running && onCancel && (armed === j.id ? (
            <span className="job-deck-confirm">
              <span>{t("panels.stop")}</span>
              <button type="button" className="job-deck-sure" data-action="job.cancel" data-target={j.id} aria-label={t("panels.confirm_stopping_background_task_cmd", { cmd: jobName(j) })} onClick={() => stop([j.id])}>{t("panels.ok")}</button>
            </span>
          ) : (
            <button type="button" className="agent-quiet" data-action="job.arm" data-target={j.id} disabled={stopping.has(j.id)} aria-label={t("panels.stop_background_job_cmd", { cmd: jobName(j) })} onClick={() => setArmed(j.id)}>{t("common.stop")}</button>
          ))}
        </div>
      </div>
    );
  };
  return (
    <div className="job-deck">
      {live.length > 0 && (
        <section aria-label={t("panels.running_n", { n: live.length })}>
          <header className="job-deck-head">
            <b>{t("panels.running_n", { n: live.length })}</b>
            {onCancel && live.length >= 2 && (armed === "*" ? (
              <span className="job-deck-confirm"><span>{t("panels.stop_all")}</span><button type="button" className="job-deck-sure" data-action="job.cancel-all" onClick={() => stop(live.map((j) => j.id))}>{t("panels.ok")}</button></span>
            ) : (
              <button type="button" className="agent-quiet" data-action="job.arm-all" onClick={() => setArmed("*")}>{t("common.stop_all")}</button>
            ))}
          </header>
          {live.map(row)}
        </section>
      )}
      {ended.length > 0 && (
        <section aria-label={t("panels.ended_n", { n: ended.length })}>
          <header className="job-deck-head">
            <b>{t("panels.ended_n", { n: ended.length })}</b>
            {onClear && <button type="button" className="agent-quiet" data-action="job.clear" onClick={() => onClear(ended.map((j) => j.id))}>{t("panels.clear")}</button>}
          </header>
          {ended.map(row)}
          <p className="job-deck-note">{t("panels.ended_jobs_stay")}</p>
        </section>
      )}
    </div>
  );
}
