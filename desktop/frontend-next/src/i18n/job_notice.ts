export const JOB_FINISHED = "job_finished";
export const JOB_KILLED = "job_killed";
export const JOB_FAILED = "job_failed";

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

export function jobErrorText(detail: string | undefined): string | undefined {
  if (!detail) return undefined;
  try {
    const p = JSON.parse(detail) as { error?: unknown };
    return typeof p.error === "string" && p.error !== "" ? p.error : undefined;
  } catch {
    return undefined;
  }
}
