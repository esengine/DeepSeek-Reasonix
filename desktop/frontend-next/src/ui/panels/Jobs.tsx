import { useEffect, useState } from "react";
import { t } from "../../i18n";
import type { JobEntry } from "../../port/port";
import { StudioIcon } from "../StudioIcon";
import { JobName, jobName } from "./JobName";

// How an ended job finished, by the kernel's status: only "done" is a success,
// and a job someone stopped is neither a success nor a fault.
const ENDED: Record<string, { label: string; tone: string }> = {
  done: { label: "common.done", tone: "var(--ok)" },
  failed: { label: "common.failed", tone: "var(--err)" },
  killed: { label: "common.stopped", tone: "var(--faint)" },
  interrupted: { label: "common.interrupted", tone: "var(--faint)" },
};

export function Jobs({ jobs, onCancel, onView }: { jobs: JobEntry[]; onCancel?: (id: string) => Promise<void>; onView?: (id: string) => void }) {
  const [, tick] = useState(0);
  const [stopping, setStopping] = useState<ReadonlySet<string>>(new Set());

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

  return (
    <div className="block" data-b="jobs">
      <div className="lbl">
        {t("common.backgroundJobs")}<span className="c">{jobs.length}</span>
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
                  <button type="button" className="cmd job-view" data-action="job.view" data-target={j.id} aria-label={t("jobs.viewOutput", { cmd: jobName(j) })} onClick={() => onView(j.id)}>
                    {jobName(j)}
                  </button>
                ) : (
                  <span className="cmd" tabIndex={0}>{jobName(j)}</span>
                )}
              </JobName>
              <span className="rt">{running ? `${Math.floor((Date.now() - j.startedAt) / 1000)}s` : ended ? t(ended.label) : j.status}</span>
              {running && onCancel && (
                <button
                  type="button"
                  className="job-stop"
                  data-action="job.cancel"
                  data-target={j.id}
                  disabled={stopping.has(j.id)}
                  title={t("common.stop")}
                  aria-label={t("jobs.stopBackgroundJob", { cmd: jobName(j) })}
                  onClick={() => {
                    setStopping((prev) => new Set(prev).add(j.id));
                    void onCancel(j.id).finally(() => setStopping((prev) => {
                      const next = new Set(prev);
                      next.delete(j.id);
                      return next;
                    }));
                  }}
                >
                  <StudioIcon name="stop" />
                </button>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
