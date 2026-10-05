// Fixed-window counters kept in D1. One atomic upsert per hit, so concurrent
// callers cannot both read the same count and both pass.
export class RateCounterRepo {
  constructor(private readonly db: D1Database) {}

  async hit(key: string, windowMs: number, now: number): Promise<{ count: number; resetsAt: number }> {
    const windowStart = Math.floor(now / windowMs) * windowMs;
    const resetsAt = windowStart + windowMs;
    const row = await this.db.prepare(
      `INSERT INTO remote_rate_counters (counter_key, window_start, count, expires_at)
       VALUES (?1, ?2, 1, ?3)
       ON CONFLICT (counter_key) DO UPDATE SET
         count = CASE WHEN remote_rate_counters.window_start = excluded.window_start
                      THEN remote_rate_counters.count + 1 ELSE 1 END,
         window_start = excluded.window_start,
         expires_at = excluded.expires_at
       RETURNING count`,
    ).bind(key, windowStart, new Date(resetsAt).toISOString()).first<{ count: number }>();
    if (!row) throw new Error("rate counter returned no row");
    return { count: row.count, resetsAt };
  }

  async peek(key: string, windowMs: number, now: number): Promise<number> {
    const windowStart = Math.floor(now / windowMs) * windowMs;
    const row = await this.db.prepare(
      "SELECT count FROM remote_rate_counters WHERE counter_key = ?1 AND window_start = ?2",
    ).bind(key, windowStart).first<{ count: number }>();
    return row?.count ?? 0;
  }
}
