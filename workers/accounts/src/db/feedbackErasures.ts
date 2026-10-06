export interface PendingErasure {
  userId: number;
  createdAt: string;
}

export class FeedbackErasureRepo {
  constructor(private readonly db: D1Database) {}

  async enqueue(userId: number, now: Date): Promise<void> {
    await this.db
      .prepare("INSERT INTO feedback_erasures (user_id, created_at) VALUES (?1, ?2) ON CONFLICT (user_id) DO NOTHING")
      .bind(userId, now.toISOString())
      .run();
  }

  async pending(limit: number): Promise<PendingErasure[]> {
    const { results } = await this.db
      .prepare(
        `SELECT user_id, created_at FROM feedback_erasures
         ORDER BY last_attempt_at IS NOT NULL, last_attempt_at, created_at, user_id LIMIT ?1`,
      )
      .bind(limit)
      .all<{ user_id: number; created_at: string }>();
    return results.map((row) => ({ userId: row.user_id, createdAt: row.created_at }));
  }

  async backlog(): Promise<{ count: number; oldest: string | null; maxAttempts: number }> {
    const row = await this.db
      .prepare("SELECT COUNT(*) AS count, MIN(created_at) AS oldest, COALESCE(MAX(attempts), 0) AS max_attempts FROM feedback_erasures")
      .first<{ count: number; oldest: string | null; max_attempts: number }>();
    return { count: row?.count ?? 0, oldest: row?.oldest ?? null, maxAttempts: row?.max_attempts ?? 0 };
  }

  async recordAttempt(userId: number, now: Date): Promise<void> {
    await this.db
      .prepare("UPDATE feedback_erasures SET attempts = attempts + 1, last_attempt_at = ?2 WHERE user_id = ?1")
      .bind(userId, now.toISOString())
      .run();
  }

  async done(userId: number): Promise<void> {
    await this.db.prepare("DELETE FROM feedback_erasures WHERE user_id = ?1").bind(userId).run();
  }
}
