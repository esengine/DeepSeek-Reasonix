import { listSessions, pruneStaleSessions } from "../../memory/session.js";

export interface PruneSessionsOptions {
  days?: number;
  dryRun?: boolean;
}

export function pruneSessionsCommand(opts: PruneSessionsOptions): void {
  const days = opts.days ?? 90;
  if (!Number.isFinite(days) || days < 1) {
    console.error(`--days must be a positive integer (got ${days}).`);
    process.exit(1);
  }
  if (opts.dryRun) {
    const cutoff = Date.now() - days * 24 * 60 * 60 * 1000;
    const stale = listSessions().filter((s) => s.mtime.getTime() < cutoff);
    if (stale.length === 0) {
      console.log(`no sessions idle ≥${days} days. Nothing would be pruned.`);
      return;
    }
    console.log(`would prune ${stale.length} session(s) idle ≥${days} days:`);
    for (const s of stale) {
      console.log(`  ${s.name}`);
    }
    console.log("");
    console.log("re-run without --dry-run to actually delete.");
    return;
  }
  const removed = pruneStaleSessions(days);
  if (removed.length === 0) {
    console.log(`no sessions idle ≥${days} days. Nothing pruned.`);
    return;
  }
  console.log(`pruned ${removed.length} session(s) idle ≥${days} days:`);
  for (const name of removed) {
    console.log(`  ${name}`);
  }
}
