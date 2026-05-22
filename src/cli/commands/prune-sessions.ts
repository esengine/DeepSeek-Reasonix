import { t } from "../../i18n/index.js";
import { listSessions, pruneStaleSessions } from "../../memory/session.js";

export interface PruneSessionsOptions {
  days?: number;
  dryRun?: boolean;
}

export function pruneSessionsCommand(opts: PruneSessionsOptions): void {
  const days = opts.days ?? 90;
  if (!Number.isFinite(days) || days < 1) {
    console.error(t("sessions.daysInvalid", { days }));
    process.exit(1);
  }
  if (opts.dryRun) {
    const cutoff = Date.now() - days * 24 * 60 * 60 * 1000;
    const stale = listSessions().filter((s) => s.mtime.getTime() < cutoff);
    if (stale.length === 0) {
      console.log(t("sessions.noIdleSessions", { days }));
      return;
    }
    console.log(t("sessions.wouldPrune", { count: stale.length, days }));
    for (const s of stale) {
      console.log(`  ${s.name}`);
    }
    console.log("");
    console.log(t("sessions.dryRunHint"));
    return;
  }
  const removed = pruneStaleSessions(days);
  if (removed.length === 0) {
    console.log(t("sessions.noIdleSessions", { days }));
    return;
  }
  console.log(t("sessions.prunedCount", { count: removed.length, days }));
  for (const name of removed) {
    console.log(`  ${name}`);
  }
}
