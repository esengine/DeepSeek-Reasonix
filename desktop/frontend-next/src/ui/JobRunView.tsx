import { createContext, useContext, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/http_error";
import type { AgentPort, JobEntry, JobOutput } from "../port/port";
import { jobElapsed } from "./panels/jobElapsed";
import { StudioIcon } from "./StudioIcon";

/** The pane's background jobs and how to stop one, for a job's workbench tab. */
export interface JobsLive { jobs: JobEntry[]; cancel?: (id: string) => Promise<void>; open?: (job: JobEntry) => void }
export const JobsLiveContext = createContext<JobsLive>({ jobs: [] });

const POLL_MS = 2000;
const GONE = "job.not_running";
const FOLLOW_SLACK = 24;

/** One background job's output in the workbench. It follows the newest output
 *  until the reader scrolls up, and reading it never takes output from the
 *  model. */
export function JobRunView({ port, id, name, shown }: { port: Pick<AgentPort, "jobOutput">; id: string; name: string; shown: boolean }) {
  const { jobs, cancel } = useContext(JobsLiveContext);
  const job = jobs.find((j) => j.id === id);
  const [out, setOut] = useState<JobOutput | null>(null);
  const [failed, setFailed] = useState("");
  const [following, setFollowing] = useState(true);
  const [, tick] = useState(0);
  const box = useRef<HTMLPreElement>(null);
  const running = (out?.status ?? job?.status) === "running";

  useEffect(() => {
    if (!shown) return;
    let alive = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const read = () => {
      port.jobOutput(id)
        .then((next) => {
          if (!alive) return;
          setOut(next);
          setFailed("");
          if (next.status === "running") timer = setTimeout(read, POLL_MS);
        })
        .catch((e) => {
          if (!alive || (e instanceof HttpError && e.reason?.code === GONE)) return;
          setFailed(reason(e));
          timer = setTimeout(read, POLL_MS * 2);
        });
    };
    read();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [port, id, shown]);

  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => tick((v) => v + 1), 1000);
    return () => clearInterval(timer);
  }, [running]);

  useEffect(() => {
    const el = box.current;
    if (el && following) el.scrollTop = el.scrollHeight;
  }, [out?.text, following]);

  const onScroll = () => {
    const el = box.current;
    if (el) setFollowing(el.scrollHeight - el.scrollTop - el.clientHeight <= FOLLOW_SLACK);
  };
  const took = job ? (running ? jobElapsed(Date.now() - job.startedAt) : job.endedAt ? jobElapsed(job.endedAt - job.startedAt) : "") : "";
  const result = !running && job
    ? [job.exitCode != null ? t("workbench.job_exited", { code: job.exitCode }) : t("workbench.job_ended"), took].filter(Boolean).join(" · ")
    : took;
  const command = job?.detail?.trim() || job?.label || "";
  return (
    <div className="jobrun" data-status={out?.status ?? job?.status}>
      <div className="jobrun-hd">
        <StudioIcon name="play" className="jobrun-mark" />
        <b>{name}</b>
        <span className="jobrun-state">{result}</span>
        {running && cancel && (
          <button type="button" className="agentrun-stop" data-action="job.cancel" data-target={id} onClick={() => void cancel(id)}>{t("common.stop")}</button>
        )}
      </div>
      {failed && <p className="jobrun-note" role="alert">{failed}</p>}
      <pre className="jobrun-out" ref={box} data-action="job.scroll" onScroll={onScroll}>
        {command && <span className="jobrun-cmd">$ {command}{"\n"}</span>}
        {out?.text ?? ""}
      </pre>
      {running && (
        <div className="jobrun-foot">
          <span>{following ? t("workbench.job_following") : t("workbench.job_paused")}</span>
          {!following && (
            <button type="button" className="agent-open" data-action="job.follow" aria-label={t("workbench.job_follow")} title={t("workbench.job_follow")}
              onClick={() => setFollowing(true)}>
              <StudioIcon name="down" />
            </button>
          )}
        </div>
      )}
    </div>
  );
}
