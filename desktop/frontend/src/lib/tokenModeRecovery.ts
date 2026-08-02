import { asArray } from "./array";
import type { JobView } from "./types";

export function visibleBackgroundJobCount(reportedCount: number, jobs: readonly JobView[]): number {
  return Math.max(0, reportedCount, jobs.length);
}

/**
 * Stops the jobs present in a captured tab, verifies the backend is actually
 * clear, and only then performs the caller's original work-mode switch.
 */
export async function stopBackgroundJobsAndSwitch(
  listJobs: () => Promise<JobView[]>,
  cancelJob: (jobID: string) => Promise<unknown>,
  switchMode: () => Promise<unknown>,
): Promise<number> {
  const jobs = asArray(await listJobs());
  for (const job of jobs) await cancelJob(job.id);
  const remaining = asArray(await listJobs());
  if (remaining.length > 0) return remaining.length;
  await switchMode();
  return 0;
}
