/**
 * JobRegistry leak probe — spawns N short-lived `node -e "exit 0"`
 * background jobs, lets each one complete, then samples the Map size +
 * resident set. If JobRegistry doesn't auto-prune completed jobs (it
 * currently does NOT — `grep "jobs.delete" src/tools/jobs.ts` returns
 * nothing) the Map grows linearly forever.
 *
 * Run:
 *   node --expose-gc --import tsx scripts/probe-jobs-leak.mts
 *
 * Tuning:
 *   PROBE_JOBS=200 node --expose-gc --import tsx scripts/probe-jobs-leak.mts
 */

import { JobRegistry } from "../src/tools/jobs.js";

const TOTAL = Number(process.env.PROBE_JOBS ?? 100);
const SAMPLE_EVERY = Number(process.env.PROBE_SAMPLE ?? 10);

function fmtMB(bytes: number): string {
  return (bytes / (1024 * 1024)).toFixed(1).padStart(7);
}

function jobsMapSize(reg: JobRegistry): number {
  // `jobs` is private — read it through the type-erased back door for the probe.
  const internal = (reg as unknown as { jobs: Map<number, unknown> }).jobs;
  return internal?.size ?? -1;
}

async function main() {
  const reg = new JobRegistry();
  const cwd = process.cwd();

  if (global.gc) global.gc();
  const baseline = process.memoryUsage();
  console.log(
    `\nbaseline: rss=${fmtMB(baseline.rss)} MB · heap=${fmtMB(baseline.heapUsed)} MB · jobs.size=${jobsMapSize(reg)}\n`,
  );

  console.log(
    `done | rss MB  | heap MB | jobs.size | growth-rss`,
  );
  console.log(
    `-----+---------+---------+-----------+-----------`,
  );

  let prevRss = baseline.rss;
  for (let i = 1; i <= TOTAL; i++) {
    await reg.start("node -e \"process.exit(0)\"", {
      cwd,
      waitSec: 1,
      // Tiny cap so the per-job buffer can't dominate the leak signal.
      maxBufferBytes: 1024,
    });
    // Brief breather so the spawned child has a chance to exit before the
    // next sample — otherwise `running:true` rows confound the leak claim.
    await new Promise((r) => setTimeout(r, 30));

    if (i % SAMPLE_EVERY === 0) {
      if (global.gc) global.gc();
      const m = process.memoryUsage();
      const delta = m.rss - prevRss;
      console.log(
        `${String(i).padStart(4)} | ${fmtMB(m.rss)} | ${fmtMB(m.heapUsed)} | ${String(jobsMapSize(reg)).padStart(9)} | ${delta >= 0 ? "+" : ""}${(delta / (1024 * 1024)).toFixed(2)} MB`,
      );
      prevRss = m.rss;
    }
  }

  if (global.gc) global.gc();
  const end = process.memoryUsage();
  console.log(
    `\nfinal:    rss=${fmtMB(end.rss)} MB · heap=${fmtMB(end.heapUsed)} MB · jobs.size=${jobsMapSize(reg)}`,
  );
  console.log(
    `delta:    rss=${fmtMB(end.rss - baseline.rss)} MB · heap=${fmtMB(end.heapUsed - baseline.heapUsed)} MB`,
  );
  console.log(
    `\nverdict:  if jobs.size == ${TOTAL}, every completed job is still pinned. Map should drop after natural cleanup.`,
  );
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
