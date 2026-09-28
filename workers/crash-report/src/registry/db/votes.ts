import type { PackageRow } from "../types";

export type VoteValue = -1 | 0 | 1;

export interface VoteTally {
  value: VoteValue;
  up: number;
  down: number;
  previous: VoteValue;
}

export class VoteRepo {
  constructor(private readonly db: D1Database) {}

  async get(packageId: number, userId: number): Promise<VoteValue> {
    const row = await this.db
      .prepare("SELECT value FROM votes WHERE package_id = ?1 AND user_id = ?2")
      .bind(packageId, userId)
      .first<{ value: number }>();
    return row?.value === 1 ? 1 : row?.value === -1 ? -1 : 0;
  }

  // The vote and the recount run as one batch (one transaction), and the
  // recount reads the votes table rather than adjusting by a delta, so the
  // cached counts cannot drift however writes interleave.
  async cast(pkg: PackageRow, userId: number, value: VoteValue, now: string): Promise<VoteTally> {
    const previous = await this.get(pkg.id, userId);
    const write =
      value === 0
        ? this.db.prepare("DELETE FROM votes WHERE package_id = ?1 AND user_id = ?2").bind(pkg.id, userId)
        : this.db
            .prepare(
              `INSERT INTO votes (package_id, user_id, value, created_at, updated_at)
               VALUES (?1, ?2, ?3, ?4, ?4)
               ON CONFLICT (package_id, user_id) DO UPDATE
               SET value = excluded.value, updated_at = excluded.updated_at`,
            )
            .bind(pkg.id, userId, value, now);
    const [, counted] = await this.db.batch([
      write,
      this.db
        .prepare(
          `UPDATE packages SET
             up_count = (SELECT COUNT(*) FROM votes WHERE package_id = ?1 AND value = 1),
             down_count = (SELECT COUNT(*) FROM votes WHERE package_id = ?1 AND value = -1)
           WHERE id = ?1
           RETURNING up_count, down_count`,
        )
        .bind(pkg.id),
    ]);
    const row = counted?.results?.[0] as { up_count?: number; down_count?: number } | undefined;
    return { value, up: row?.up_count ?? 0, down: row?.down_count ?? 0, previous };
  }
}
