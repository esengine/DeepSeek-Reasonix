import { WINDOW_DAYS, recommendScore, type DailyInstalls } from "../lib/ranking";

const UPDATE_CHUNK = 50;

// Recomputes packages.rec_score from the vote counts and the distinct install
// ids of the window, each counted once on the latest day it was seen. Called for one package after a vote or a counted install, and for
// every package by the cron, because decay moves scores with no write at all.
// A private package is never ranked: it takes no votes and no counted installs.
export async function rescore(db: D1Database, today: string, packageId?: number): Promise<number> {
  const one = packageId !== undefined;
  const pkgs = await db
    .prepare(`SELECT id, up_count, down_count, rec_score FROM packages WHERE status != 'private'${one ? " AND id = ?1" : ""}`)
    .bind(...(one ? [packageId] : []))
    .all<{ id: number; up_count: number; down_count: number; rec_score: number }>();
  const days = await db
    .prepare(
      `SELECT package_id, latest AS date, COUNT(*) AS count FROM (
         SELECT package_id, install_key, MAX(date) AS latest FROM package_install_seen
         WHERE date >= date(?1, '-${WINDOW_DAYS - 1} day')${one ? " AND package_id = ?2" : ""}
         GROUP BY package_id, install_key
       ) GROUP BY package_id, latest`,
    )
    .bind(...(one ? [today, packageId] : [today]))
    .all<{ package_id: number; date: string; count: number }>();
  const byPackage = new Map<number, DailyInstalls[]>();
  for (const d of days.results ?? []) {
    const list = byPackage.get(d.package_id) ?? [];
    list.push({ date: d.date, count: d.count });
    byPackage.set(d.package_id, list);
  }
  const updates: D1PreparedStatement[] = [];
  for (const p of pkgs.results ?? []) {
    const score = recommendScore(p.up_count, p.down_count, byPackage.get(p.id) ?? [], today);
    if (score !== p.rec_score) {
      // Fenced on the counts it was computed from, so a vote landing mid-run
      // is not overwritten by a score that predates it.
      updates.push(
        db
          .prepare("UPDATE packages SET rec_score = ?1 WHERE id = ?2 AND up_count = ?3 AND down_count = ?4")
          .bind(score, p.id, p.up_count, p.down_count),
      );
    }
  }
  for (let i = 0; i < updates.length; i += UPDATE_CHUNK) {
    await db.batch(updates.slice(i, i + UPDATE_CHUNK));
  }
  return updates.length;
}
