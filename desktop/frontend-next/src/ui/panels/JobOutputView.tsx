import { useEffect, useRef, useState } from "react";
import { useModalFocus } from "../useModalFocus";
import { createPortal } from "react-dom";
import { t } from "../../i18n";
import { reason } from "../../i18n/kernel";
import { bytes } from "../../i18n/format";
import { HttpError } from "../../port/http_error";
import type { JobEntry, JobOutput } from "../../port/port";
import { CopyButton } from "../CopyButton";
import { useEscape } from "../dismiss";
import { StudioIcon } from "../StudioIcon";

const POLL_MS = 2000;
const GONE = "job.not_running";
const ENDED: Record<string, string> = { done: "common.done_2", failed: "common.failed", killed: "plan.stopped", interrupted: "common.interrupted" };

/** What a background job has printed, for the person looking at it. It reads
 *  again every couple of seconds while the job runs and stops when it ends or
 *  the view closes; looking never takes output from the model. */
export function JobOutputView({ job, read, onClose, onCancel }: { job: JobEntry; read: (id: string) => Promise<JobOutput>; onClose: () => void; onCancel?: (id: string) => Promise<void> }) {
  const [out, setOut] = useState<JobOutput | null>(null);
  const [gone, setGone] = useState(false);
  const [failed, setFailed] = useState("");
  const box = useRef<HTMLPreElement>(null);
  const sheet = useRef<HTMLDivElement>(null);
  useModalFocus(sheet);
  useEscape(true, onClose);

  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = () => {
      read(job.id)
        .then((next) => {
          if (!alive) return;
          setOut(next);
          setFailed("");
          if (next.status === "running") timer = setTimeout(tick, POLL_MS);
        })
        .catch((e) => {
          if (!alive) return;
          if (e instanceof HttpError && e.reason?.code === GONE) setGone(true);
          else {
            setFailed(reason(e));
            timer = setTimeout(tick, POLL_MS * 2);
          }
        });
    };
    tick();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [job.id, read]);

  useEffect(() => {
    const el = box.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [out?.text]);

  const ended = out && out.status !== "running" ? ENDED[out.status] ?? out.status : "";
  const body = (
    <div className="agent-tx-scrim" data-action="job.close" role="presentation" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={sheet} className="agent-tx" role="dialog" aria-modal="true" aria-label={t("panels.background_task_output_name", { name: job.label || job.id })}>
        <div className="agent-tx-hd">
          <span className="who">{job.label || job.id}</span>
          <span className="rt">{ended ? t(ended) : t("common.running")}</span>
          {out && out.text && <CopyButton text={out.text} iconOnly label={t("panels.copy_output")} />}
          {onCancel && !gone && !ended && (
            <button type="button" className="agent-tx-x" data-action="job.cancel" data-target={job.id} aria-label={t("panels.stop_this_task")} title={t("panels.stop_this_task")} onClick={() => void onCancel(job.id)}>
              <StudioIcon name="stop" />
            </button>
          )}
          <button type="button" className="agent-tx-x" data-action="job.close" aria-label={t("common.close")} onClick={onClose}>
            <StudioIcon name="close" />
          </button>
        </div>
        {gone && <p className="mnote">{t("panels.this_task_has_ended_there")}</p>}
        {failed && <p className="mnote">{failed}</p>}
        {out && out.truncated && <p className="mnote">{t("panels.showing_only_the_last_part", { total: bytes(out.bytes) })}</p>}
        {out && !gone && <pre className="nest-ret job-out" ref={box} tabIndex={0}>{out.text || t("panels.no_output_yet")}</pre>}
      </div>
    </div>
  );
  return createPortal(body, document.body);
}
