-- Additive Registry indexes and the install rollup used by trending.
-- Apply before deploying the matching folded crash/registry Worker:
--   wrangler d1 execute reasonix-registry --remote --file=registry-migrate-performance-indexes.sql

CREATE INDEX IF NOT EXISTS packages_active_created
  ON packages (created_at DESC)
  WHERE status = 'active';

CREATE INDEX IF NOT EXISTS packages_active_kind_created
  ON packages (kind, created_at DESC)
  WHERE status = 'active';

CREATE INDEX IF NOT EXISTS packages_active_installs
  ON packages (install_count DESC, created_at DESC)
  WHERE status = 'active';

CREATE TABLE IF NOT EXISTS package_install_daily (
  date       TEXT NOT NULL,
  package_id INTEGER NOT NULL,
  count      INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (date, package_id)
);

CREATE TABLE IF NOT EXISTS registry_migration_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- Backfill deterministically. Re-running the migration replaces each day's
-- aggregate with the exact count from the legacy event log rather than adding
-- it a second time.
INSERT INTO package_install_daily (date, package_id, count)
SELECT substr(created_at, 1, 10), package_id, COUNT(*)
FROM events
WHERE type = 'install' AND package_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM registry_migration_meta WHERE key = 'package_install_daily_v1')
GROUP BY substr(created_at, 1, 10), package_id
ON CONFLICT (date, package_id) DO UPDATE SET count = excluded.count;

INSERT OR IGNORE INTO registry_migration_meta (key, value)
VALUES ('package_install_daily_v1', datetime('now'));
