import { useEffect, useState } from "react";
import { t } from "../../i18n";
import type { JobEntry } from "../../port/port";
import { StudioIcon } from "../StudioIcon";
import { JobName, jobName } from "./JobName";
import { jobElapsed } from "./jobElapsed";

// How an ended job finished, by the kernel's status: only "done" is a success,
// and a job someone stopped is neither a success nor a fault.
const ENDED: Record<string, { label: string; tone: string }> = {
  done: { label: "common.done_2", tone: "var(--ok)" },
  failed: { label: "common.failed", tone: "var(--err)" },
  killed: { label: "plan.stopped", tone: "var(--faint)" },
  interrupted: { label: "common.interrupted", tone: "var(--faint)" },
};

export function Jobs({ jobs, onCancel, onView }: { jobs: JobEntry[]; onCancel?: (id: string) => Promise<void>; onView?: (id: string) => void }) {
  const [, tick] = useState(0);
  const [stopping, setStopping] = useState<ReadonlySet<string>>(new Set());
  const [armed, setArmed] = useState("");

  useEffect(() => {
    if (!armed) return;
    const timer = setTimeout(() => setArmed(""), 3000);
    return () => clearTimeout(timer);
  }, [armed]);

  useEffect(() => {
    if (!jobs.some((j) => j.status === "running")) return;
    const t = setInterval(() => tick((v) => v + 1), 1000);
    return () => clearInterval(t);
  }, [jobs]);

  // "No background jobs" is a sentence about nothing, and it held a block on
  // every rail that was fine. The order it sits in is fixed per posture, so a
  // block coming and going moves only what is below it — cheaper than a
  // standing row a reader has learned to skip.
  if (jobs.length === 0) return null;
  const live = jobs.filter((j) => j.status === "running");
  const stopAll = () => {
    setArmed("");
    for (const j of live) void onCancel?.(j.id);
  };

  return (
    <div className="block" data-b="jobs">
      <div className="lbl">
        {t("common.background_jobs")}
        {onCancel && live.length >= 2 && (armed === "*" ? (
          <span className="job-confirm">
            <span>{t("panels.stop_all")}</span>
            <button type="button" className="job-stop" data-action="job.cancel-all" onClick={stopAll}>{t("panels.ok")}</button>
          </span>
        ) : (
          <button type="button" className="job-stop-all" data-action="job.arm-all" onClick={() => setArmed("*")}>{t("common.stop_all")}</button>
        ))}
        <span className="c">{jobs.length}</span>
      </div>
      <div className="jobs">
        {jobs.map((j) => {
          const running = j.status === "running";
          const ended = ENDED[j.status];
          return (
            <div className="job" key={j.id} data-done={running ? undefined : ""} data-status={j.status}>
              <i
                className="pip"
                style={running ? { background: "var(--net)", animation: "tick 1.6s ease-in-out infinite" } : { background: ended?.tone ?? "var(--faint)" }}
              />
              <JobName job={j}>
                {running && onView ? (
                  <button type="button" className="cmd job-view" data-action="job.view" data-target={j.id} aria-label={t("panels.view_output_cmd", { cmd: jobName(j) })} onClick={() => onView(j.id)}>
                    {jobName(j)}
                  </button>
                ) : (
                  <span className="cmd" tabIndex={0}>{jobName(j)}</span>
                )}
              </JobName>
              <span className="rt">{running ? jobElapsed(Date.now() - j.startedAt) : ended ? t(ended.label) : j.status}</span>
              {running && onCancel && (armed === j.id ? (
                <span className="job-confirm">
                  <span>{t("panels.stop")}</span>
                  <button
                    type="button"
                    className="job-stop"
                    data-action="job.cancel"
                    data-target={j.id}
                    disabled={stopping.has(j.id)}
                    aria-label={t("panels.confirm_stopping_background_task_cmd", { cmd: jobName(j) })}
                    onClick={() => {
                      setArmed("");
                      setStopping((prev) => new Set(prev).add(j.id));
                      void onCancel(j.id).finally(() => setStopping((prev) => {
                        const next = new Set(prev);
                        next.delete(j.id);
                        return next;
                      }));
                    }}
                  >
                    {t("panels.ok")}
                  </button>
                </span>
              ) : (
                <button
                  type="button"
                  className="job-stop"
                  data-action="job.arm"
                  data-target={j.id}
                  disabled={stopping.has(j.id)}
                  title={t("common.stop")}
                  aria-label={t("panels.stop_background_job_cmd", { cmd: jobName(j) })}
                  onClick={() => setArmed(j.id)}
                >
                  <StudioIcon name="stop" />
                </button>
              ))}
            </div>
          );
        })}
      </div>
    </div>
  );
}
