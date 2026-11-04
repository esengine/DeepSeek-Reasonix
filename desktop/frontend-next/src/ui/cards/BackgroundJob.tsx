import { useContext, useEffect, useState } from "react";
import { t } from "../../i18n";
import { JobsLiveContext } from "../JobRunView";
import { jobElapsed } from "../panels/jobElapsed";

/** Where the job a call sent to the background stands, read from the pane's
 *  live job list rather than from what the call said when it started. */
export function BackgroundReading({ id }: { id: string }) {
  const { jobs } = useContext(JobsLiveContext);
  const job = jobs.find((j) => j.id === id);
  const running = job?.status === "running";
  const [, tick] = useState(0);
  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => tick((v) => v + 1), 1000);
    return () => clearInterval(timer);
  }, [running]);
  if (!job) return null;
  const text = running
    ? t("chat.background_running", { time: jobElapsed(Date.now() - job.startedAt) })
    : [job.endedAt ? jobElapsed(job.endedAt - job.startedAt) : "", job.exitCode != null ? t("panels.exit_code", { code: job.exitCode }) : ""].filter(Boolean).join(" · ");
  return text ? <span className="tool-bg" data-status={job.status}>{text}</span> : null;
}

/** While the job runs, what sending it away means, and the way to its output. */
export function BackgroundNote({ id }: { id: string }) {
  const { jobs, open } = useContext(JobsLiveContext);
  const job = jobs.find((j) => j.id === id);
  if (job?.status !== "running") return null;
  return (
    <p className="tool-bg-note">
      {t("chat.background_note")}
      {open && <button type="button" data-action="job.view" data-target={id} onClick={() => open(job)}>{t("panels.view_output")}</button>}
    </p>
  );
}
