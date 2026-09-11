-- Reasonix registry: published skills/MCP servers, their versions, and the
-- activity/install event log. Apply:
--   wrangler d1 migrations apply reasonix-registry --local   (or --remote)

-- One row per published capability. The registry stores only metadata and a
-- source pointer; it never fetches or executes the content — install happens
-- client-side through the install_source tool (SSRF-guarded, risk-rated).
CREATE TABLE IF NOT EXISTS packages (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  kind           TEXT    NOT NULL,                    -- skill | plugin | mcp
  scope_handle   TEXT    NOT NULL,                    -- publisher handle (namespace)
  name           TEXT    NOT NULL,                    -- capability slug within the scope
  slug           TEXT    NOT NULL UNIQUE,             -- '<handle>/<name>' canonical id
  summary        TEXT    NOT NULL DEFAULT '',         -- one-liner (SKILL.md description)
  description    TEXT    NOT NULL DEFAULT '',         -- long markdown (README)
  source         TEXT    NOT NULL DEFAULT '',         -- what install_source pulls from
  install_kind   TEXT    NOT NULL DEFAULT 'auto',     -- auto | skill | plugin | mcp
  homepage       TEXT    NOT NULL DEFAULT '',
  repo_url       TEXT    NOT NULL DEFAULT '',
  tags           TEXT    NOT NULL DEFAULT '',         -- comma-separated
  latest_version TEXT    NOT NULL DEFAULT '',
  install_count  INTEGER NOT NULL DEFAULT 0,
  star_count     INTEGER NOT NULL DEFAULT 0,
  verified       INTEGER NOT NULL DEFAULT 0,          -- owner/admin trust badge
  status         TEXT    NOT NULL DEFAULT 'active',   -- active | hidden | removed
  publisher_id   INTEGER NOT NULL,                    -- accounts user id (owner)
  created_at     TEXT    NOT NULL,
  updated_at     TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS packages_kind ON packages (kind, status);
CREATE INDEX IF NOT EXISTS packages_installs ON packages (install_count DESC);
CREATE INDEX IF NOT EXISTS packages_created ON packages (created_at DESC);
CREATE INDEX IF NOT EXISTS packages_publisher ON packages (publisher_id);
CREATE INDEX IF NOT EXISTS packages_active_created
  ON packages (created_at DESC)
  WHERE status = 'active';
CREATE INDEX IF NOT EXISTS packages_active_kind_created
  ON packages (kind, created_at DESC)
  WHERE status = 'active';
CREATE INDEX IF NOT EXISTS packages_active_installs
  ON packages (install_count DESC, created_at DESC)
  WHERE status = 'active';

-- Immutable version history: one row per published version, carrying the source
-- snapshot and content hash so a consumer can audit provenance before install.
CREATE TABLE IF NOT EXISTS package_versions (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  package_id   INTEGER NOT NULL,
  version      TEXT    NOT NULL,
  source       TEXT    NOT NULL DEFAULT '',
  manifest     TEXT    NOT NULL DEFAULT '',           -- JSON: frontmatter / mcp config snapshot
  content_hash TEXT    NOT NULL DEFAULT '',           -- sha256 of the source
  risk_level   TEXT    NOT NULL DEFAULT '',           -- echoed from install_source plan
  created_at   TEXT    NOT NULL,
  UNIQUE (package_id, version)
);
CREATE INDEX IF NOT EXISTS versions_package ON package_versions (package_id, created_at DESC);

-- Per-user stars, so star_count is a real de-duplicated tally.
CREATE TABLE IF NOT EXISTS stars (
  package_id INTEGER NOT NULL,
  user_id    INTEGER NOT NULL,
  created_at TEXT    NOT NULL,
  PRIMARY KEY (package_id, user_id)
);

-- Activity log: powers the homepage live feed and the 7-day "trending" ranking.
CREATE TABLE IF NOT EXISTS events (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  type         TEXT    NOT NULL,                       -- publish | update | install | star
  package_id   INTEGER,
  actor_handle TEXT    NOT NULL DEFAULT '',
  summary      TEXT    NOT NULL DEFAULT '',
  created_at   TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS events_created ON events (created_at DESC);
CREATE INDEX IF NOT EXISTS events_trending ON events (type, created_at);

-- Daily install counts replace one-row-per-install events for trending. Legacy
-- install events remain readable for audit/backfill compatibility; new writes
-- go to this bounded aggregate instead.
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
