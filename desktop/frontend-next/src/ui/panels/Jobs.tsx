import { useEffect, useState } from "react";
import { t } from "../../i18n";
import type { JobEntry } from "../../port/port";
import { StudioIcon } from "../StudioIcon";

// How an ended job finished, by the kernel's status: only "done" is a success,
// and a job someone stopped is neither a success nor a fault.
const ENDED: Record<string, { label: string; tone: string }> = {
  done: { label: "已完成", tone: "var(--ok)" },
  failed: { label: "失败", tone: "var(--err)" },
  killed: { label: "已停止", tone: "var(--faint)" },
  interrupted: { label: "已中断", tone: "var(--faint)" },
};

export function Jobs({ jobs, onCancel }: { jobs: JobEntry[]; onCancel?: (id: string) => Promise<void> }) {
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
        {t("后台任务")}<span className="c">{jobs.length}</span>
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
              <span className="cmd">{j.label || j.id}</span>
              <span className="rt">{running ? `${Math.floor((Date.now() - j.startedAt) / 1000)}s` : ended ? t(ended.label) : j.status}</span>
              {running && onCancel && (
                <button
                  type="button"
                  className="job-stop"
                  data-action="job.cancel"
                  data-target={j.id}
                  disabled={stopping.has(j.id)}
                  title={t("停止")}
                  aria-label={t("停止后台任务：{cmd}", { cmd: j.label || j.id })}
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
