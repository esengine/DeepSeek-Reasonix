import { useEffect, useMemo, useState } from "react";
import { I } from "../icons";
import { t, useLang } from "../i18n";
import type { JobInfo } from "../protocol";

export function JobsPop({
  open,
  onClose,
  jobs,
  onStop,
  onStopAll,
}: {
  open: boolean;
  onClose: () => void;
  jobs: JobInfo[];
  onStop: (jobId: number) => void;
  onStopAll: () => void;
}) {
  useLang();
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (!open) return;
    const id = window.setInterval(() => setTick((n) => n + 1), 600);
    return () => window.clearInterval(id);
  }, [open]);
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  const running = useMemo(() => jobs.filter((j) => j.running), [jobs]);
  const exited = useMemo(() => jobs.filter((j) => !j.running), [jobs]);

  if (!open) return null;

  return (
    <div className="jobs-mask" onClick={onClose}>
      <div className="jobs-pop" onClick={(e) => e.stopPropagation()}>
        <div className="jobs-head">
          <span className="ico">
            <I.cpu size={13} />
          </span>
          <div>
            <div className="tt">{t("jobs.title")}</div>
            <div className="ss">
              <span className="b ok">{running.length}</span> {t("jobs.running")} ·{" "}
              <span className="b mut">{exited.length}</span> {t("jobs.exited")}
            </div>
          </div>
          <span className="grow" />
          {running.length > 0 ? (
            <button
              type="button"
              className="btn danger sm"
              onClick={onStopAll}
              title={t("jobs.stopAllTip")}
            >
              <I.stop size={10} /> {t("jobs.stopAll")}
            </button>
          ) : null}
        </div>

        <div className="jobs-body">
          {jobs.length === 0 ? (
            <div className="jobs-empty">{t("jobs.empty")}</div>
          ) : (
            <>
              {running.length > 0 ? <div className="jobs-grp">{t("jobs.running")}</div> : null}
              {running.map((j) => (
                <JobRow key={j.id} job={j} tick={tick} onStop={onStop} />
              ))}
              {exited.length > 0 ? <div className="jobs-grp">{t("jobs.exited")}</div> : null}
              {exited.map((j) => (
                <JobRow key={j.id} job={j} tick={tick} onStop={onStop} />
              ))}
            </>
          )}
        </div>

        <div className="jobs-foot">
          <div className="row">
            <I.zap size={11} />
            <span>
              {t("jobs.running")} <span className="v">{running.length}</span>
            </span>
            <span>·</span>
            <span>
              {t("jobs.exited")} <span className="v">{exited.length}</span>
            </span>
            <span className="grow" />
            <span>
              <kbd>⌘J</kbd> {t("jobs.kbToggle")} · <kbd>esc</kbd> {t("jobs.kbClose")}
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}

function formatElapsed(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  const r = Math.floor(s % 60);
  return `${m}m${r}s`;
}

function JobRow({
  job,
  tick,
  onStop,
}: {
  job: JobInfo;
  tick: number;
  onStop: (jobId: number) => void;
}) {
  const [exp, setExp] = useState(false);
  const elapsedMs = (job.running ? Date.now() : job.startedAt + 0) - job.startedAt;
  const liveMs = job.running ? Date.now() - job.startedAt : elapsedMs;
  // tick is referenced so the row re-renders for the running clock
  void tick;
  return (
    <div className="job-row" data-status={job.running ? "running" : "exited"}>
      <div className="jr-main" onClick={() => setExp((v) => !v)}>
        <span className="jr-state">
          {job.running ? (
            <span className="spin" />
          ) : (
            <I.history size={11} style={{ color: "var(--muted-2)" }} />
          )}
        </span>
        <span className="jr-kind">
          <I.terminal size={11} />
          <span>shell</span>
        </span>
        <div className="jr-body">
          <div className="nm" title={job.command}>
            {job.command}
          </div>
          <div className="sub">
            <span className="ses">{job.sessionLabel}</span>
            {job.spawnError ? <span className="rk">· {job.spawnError}</span> : null}
            {!job.running && job.exitCode !== null && job.exitCode !== 0 ? (
              <span className="rk">· exit {job.exitCode}</span>
            ) : null}
          </div>
        </div>
        <div className="jr-time">{formatElapsed(liveMs)}</div>
        <div className="jr-act">
          {job.running ? (
            <button
              type="button"
              className="btn danger sm"
              title={t("jobs.stopOne")}
              onClick={(e) => {
                e.stopPropagation();
                onStop(job.id);
              }}
            >
              <I.stop size={10} /> {t("jobs.stop")}
            </button>
          ) : (
            <span className="jr-exit" title={`exit ${job.exitCode ?? "—"}`}>
              {job.exitCode === 0 ? "ok" : `exit ${job.exitCode ?? "?"}`}
            </span>
          )}
        </div>
      </div>
      {exp ? (
        <div className="jr-detail">
          <div className="kv-grid">
            <div>
              <span className="k">job_id</span>
              <span className="v">{job.id}</span>
            </div>
            <div>
              <span className="k">pid</span>
              <span className="v">{job.pid ?? "—"}</span>
            </div>
            <div>
              <span className="k">elapsed</span>
              <span className="v">{formatElapsed(liveMs)}</span>
            </div>
          </div>
          {job.outputTail ? <pre className="jr-log">{job.outputTail}</pre> : null}
        </div>
      ) : null}
    </div>
  );
}
