-- Votes, part 2 of 2: tables, index, and the one-time star backfill. Safe to
-- re-run; apply right after part 1 and before deploying the matching Worker:
--   wrangler d1 execute reasonix-registry --remote --file=registry-migrate-votes.sql
CREATE TABLE IF NOT EXISTS votes (
  package_id INTEGER NOT NULL,
  user_id    INTEGER NOT NULL,
  value      INTEGER NOT NULL CHECK (value IN (-1, 1)),
  created_at TEXT    NOT NULL,
  updated_at TEXT    NOT NULL,
  PRIMARY KEY (package_id, user_id)
);
CREATE TABLE IF NOT EXISTS package_install_seen (
  install_key TEXT    NOT NULL,
  package_id  INTEGER NOT NULL,
  date        TEXT    NOT NULL,
  fresh       INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (install_key, package_id, date)
);
CREATE INDEX IF NOT EXISTS install_seen_date ON package_install_seen (date);
CREATE INDEX IF NOT EXISTS packages_active_recommended
  ON packages (rec_score DESC, install_count DESC, created_at DESC)
  WHERE status = 'active';
CREATE TABLE IF NOT EXISTS registry_migration_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- Every legacy star becomes a +1 vote, except a publisher's star on their own
-- package, which the vote rules refuse. stars itself is left in place. The meta
-- guard keeps a re-run from restoring a vote someone has since withdrawn, and
-- OR IGNORE keeps it from overwriting a vote cast through the new endpoint.
INSERT OR IGNORE INTO votes (package_id, user_id, value, created_at, updated_at)
SELECT s.package_id, s.user_id, 1, s.created_at, s.created_at
FROM stars s
JOIN packages p ON p.id = s.package_id
WHERE s.user_id != p.publisher_id
  AND NOT EXISTS (SELECT 1 FROM registry_migration_meta WHERE key = 'votes_from_stars_v1');
INSERT OR IGNORE INTO registry_migration_meta (key, value)
VALUES ('votes_from_stars_v1', datetime('now'));

-- Recount from the votes table; idempotent by construction.
UPDATE packages SET
  up_count = (SELECT COUNT(*) FROM votes v WHERE v.package_id = packages.id AND v.value = 1),
  down_count = (SELECT COUNT(*) FROM votes v WHERE v.package_id = packages.id AND v.value = -1);
