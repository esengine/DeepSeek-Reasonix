export interface InstallResult {
  counted: boolean;
  count: number;
  packageId: number;
  scopeHandle: string;
}

export class InstallRepo {
  constructor(private readonly db: D1Database) {}

  // One counted install per (install key, package, UTC day). The seen row is
  // inserted fresh, the counters move only while it is fresh, and the batch
  // clears the flag before it commits, so a repeat or a concurrent duplicate
  // finds the row already spent. Returns null when no active package matches.
  async record(slug: string, installKey: string, now: string): Promise<InstallResult | null> {
    const pkg = await this.db
      .prepare("SELECT id, scope_handle FROM packages WHERE slug = ?1 AND status = 'active'")
      .bind(slug)
      .first<{ id: number; scope_handle: string }>();
    if (!pkg) return null;
    const date = now.slice(0, 10);
    const fresh = `SELECT 1 FROM package_install_seen
                   WHERE install_key = ?2 AND package_id = ?1 AND date = ?3 AND fresh = 1`;
    const [, bumped, , , total] = await this.db.batch([
      this.db
        .prepare(
          `INSERT OR IGNORE INTO package_install_seen (install_key, package_id, date, fresh)
           VALUES (?2, ?1, ?3, 1)`,
        )
        .bind(pkg.id, installKey, date),
      this.db
        .prepare(
          `UPDATE packages SET install_count = install_count + 1
           WHERE id = ?1 AND status = 'active' AND EXISTS (${fresh})
           RETURNING install_count`,
        )
        .bind(pkg.id, installKey, date),
      this.db
        .prepare(
          `INSERT INTO package_install_daily (date, package_id, count)
           SELECT ?3, ?1, 1 WHERE EXISTS (${fresh})
           ON CONFLICT (date, package_id) DO UPDATE
           SET count = package_install_daily.count + excluded.count`,
        )
        .bind(pkg.id, installKey, date),
      this.db
        .prepare(
          `UPDATE package_install_seen SET fresh = 0
           WHERE install_key = ?2 AND package_id = ?1 AND date = ?3`,
        )
        .bind(pkg.id, installKey, date),
      this.db.prepare("SELECT install_count FROM packages WHERE id = ?1").bind(pkg.id),
    ]);
    const counted = (bumped?.results?.length ?? 0) > 0;
    const count = (total?.results?.[0] as { install_count?: number } | undefined)?.install_count ?? 0;
    return { counted, count, packageId: pkg.id, scopeHandle: pkg.scope_handle };
  }

  // Dedupe keys only matter for the day they name; older rows are dead weight.
  async purgeBefore(date: string): Promise<void> {
    await this.db.prepare("DELETE FROM package_install_seen WHERE date < ?1").bind(date).run();
  }
}
