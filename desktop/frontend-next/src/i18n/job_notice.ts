import { t } from ".";
import { elapsed } from "./format";

export const JOB_STARTED = "job_started";
export const JOB_FINISHED = "job_finished";
export const JOB_KILLED = "job_killed";
export const JOB_FAILED = "job_failed";
export const JOB_STALLED = "job_stalled";

// The typed payload a background-job notice carries as its detail, read as the
// variables its sentence is worded from. Anything that does not parse reads as no
// payload, so the notice keeps the kernel's own text.
export function jobNoticeVars(detail: string | undefined): { name: string } | undefined {
  if (!detail) return undefined;
  try {
    const p = JSON.parse(detail) as { id?: unknown; label?: unknown };
    if (typeof p.id !== "string" || p.id === "") return undefined;
    return { name: typeof p.label === "string" && p.label !== "" ? p.label : p.id };
  } catch {
    return undefined;
  }
}

// A stalled job's sentence also says how long it has been inactive, and whether
// it had reported any steps by then: a job that worked and stopped is waiting on
// something, while one that never reported may be writing elsewhere. Without the
// span the sentence would be half-filled, so it reads as no payload.
export function jobStalledVars(detail: string | undefined): { name: string; quiet: string; work: string } | undefined {
  const named = jobNoticeVars(detail);
  if (!named || !detail) return undefined;
  try {
    const p = JSON.parse(detail) as { quietSeconds?: unknown; steps?: unknown };
    if (typeof p.quietSeconds !== "number" || !Number.isInteger(p.quietSeconds) || p.quietSeconds <= 0) return undefined;
    const steps = typeof p.steps === "number" && Number.isInteger(p.steps) && p.steps > 0 ? p.steps : 0;
    return {
      ...named,
      quiet: elapsed(p.quietSeconds * 1000),
      work: steps > 0 ? t("{n} 步在静默前完成", { n: steps }) : t("静默前没有任何进展"),
    };
  } catch {
    return undefined;
  }
}

// How a failed process ended, worded from the typed fields the producer wrote:
// a signal first, since an exit code is then only the shell's echo of it. The
// job function's own error text stands in when the producer did not type it.
export function jobErrorText(detail: string | undefined): string | undefined {
  if (!detail) return undefined;
  try {
    const p = JSON.parse(detail) as { error?: unknown; exitCode?: unknown; signal?: unknown };
    if (typeof p.signal === "number" && p.signal > 0) return t("被信号 {signal} 终止", { signal: p.signal });
    if (typeof p.exitCode === "number") return t("退出码 {code}", { code: p.exitCode });
    return typeof p.error === "string" && p.error !== "" ? p.error : undefined;
  } catch {
    return undefined;
  }
}
