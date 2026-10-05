-- Apply: wrangler d1 execute reasonix-crash --remote --file=schema.sql
CREATE TABLE IF NOT EXISTS groups (
  fingerprint TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  count INTEGER NOT NULL,
  first_seen TEXT NOT NULL,
  last_seen TEXT NOT NULL,
  first_version TEXT NOT NULL DEFAULT '',
  last_version TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open',
  note TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'legacy',
  label TEXT NOT NULL DEFAULT '',
  error_type TEXT NOT NULL DEFAULT '',
  top_frame TEXT NOT NULL DEFAULT '',
  severity TEXT NOT NULL DEFAULT 'medium',
  last_os TEXT NOT NULL DEFAULT '',
  last_arch TEXT NOT NULL DEFAULT '',
  last_build_commit TEXT NOT NULL DEFAULT '',
  last_channel TEXT NOT NULL DEFAULT '',
  resolved_in TEXT NOT NULL DEFAULT '',
  resolved_at TEXT NOT NULL DEFAULT '',
  regressed_at TEXT NOT NULL DEFAULT '',
  last_sample_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS reports (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  fingerprint TEXT NOT NULL,
  kind TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  arch TEXT NOT NULL,
  message TEXT NOT NULL,
  device TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT 'legacy',
  label TEXT NOT NULL DEFAULT '',
  error_type TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  top_frame TEXT NOT NULL DEFAULT '',
  build_commit TEXT NOT NULL DEFAULT '',
  channel TEXT NOT NULL DEFAULT '',
  language TEXT NOT NULL DEFAULT '',
  view TEXT NOT NULL DEFAULT '',
  breadcrumbs TEXT NOT NULL DEFAULT '',
  component_stack TEXT NOT NULL DEFAULT '',
  stack TEXT NOT NULL DEFAULT '',
  occurred_at TEXT NOT NULL DEFAULT '',
  webview2 TEXT NOT NULL DEFAULT '',
  web_runtime TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS reports_fingerprint ON reports (fingerprint);

CREATE TABLE IF NOT EXISTS pings (
  date TEXT NOT NULL,
  install_id TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  arch TEXT NOT NULL,
  os_version TEXT NOT NULL DEFAULT '',
  os_build INTEGER NOT NULL DEFAULT 0,
  os_revision INTEGER NOT NULL DEFAULT 0,
  channel TEXT NOT NULL DEFAULT '',
  distro_id TEXT NOT NULL DEFAULT '',
  distro_version TEXT NOT NULL DEFAULT '',
  kernel_version TEXT NOT NULL DEFAULT '',
  session_type TEXT NOT NULL DEFAULT '',
  runtime_engine TEXT NOT NULL DEFAULT '',
  runtime_version TEXT NOT NULL DEFAULT '',
  gpu_mode TEXT NOT NULL DEFAULT '',
  opens INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (date, install_id)
);

-- CLI telemetry stays in additive tables so either the schema migration or the
-- Worker deployment can happen first without changing the Desktop contract.
CREATE TABLE IF NOT EXISTS cli_pings (
  date TEXT NOT NULL,
  install_id TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  arch TEXT NOT NULL,
  os_version TEXT NOT NULL DEFAULT '',
  os_build INTEGER NOT NULL DEFAULT 0,
  os_revision INTEGER NOT NULL DEFAULT 0,
  channel TEXT NOT NULL DEFAULT '',
  distro_id TEXT NOT NULL DEFAULT '',
  distro_version TEXT NOT NULL DEFAULT '',
  kernel_version TEXT NOT NULL DEFAULT '',
  session_type TEXT NOT NULL DEFAULT '',
  runtime_engine TEXT NOT NULL DEFAULT '',
  runtime_version TEXT NOT NULL DEFAULT '',
  gpu_mode TEXT NOT NULL DEFAULT '',
  opens INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (date, install_id)
);

-- Single-row checkpoint used by the scheduled ingest sentinel to detect when
-- launch totals stop advancing between runs. The worker also creates this
-- table at runtime so existing databases need no manual migration.
CREATE TABLE IF NOT EXISTS ingest_sentinel_state (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  day TEXT NOT NULL,
  ping_count INTEGER NOT NULL,
  open_count INTEGER NOT NULL,
  checked_at TEXT NOT NULL
);

-- Opt-in aggregate Desktop metrics: anonymous per-day (signal, bucket)
-- counters, no content. Generic shape so a new signal is just new rows.
CREATE TABLE IF NOT EXISTS metrics (
  date TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  signal TEXT NOT NULL,
  bucket TEXT NOT NULL,
  count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (date, version, os, signal, bucket)
);

CREATE TABLE IF NOT EXISTS cli_metrics (
  date TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  signal TEXT NOT NULL,
  bucket TEXT NOT NULL,
  count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (date, version, os, signal, bucket)
);

CREATE TABLE IF NOT EXISTS report_daily (
  date TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  events INTEGER NOT NULL DEFAULT 0,
  identified_events INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (date, fingerprint)
);

CREATE TABLE IF NOT EXISTS report_installations (
  date TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  install_id TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  arch TEXT NOT NULL,
  os_build INTEGER NOT NULL DEFAULT 0,
  os_revision INTEGER NOT NULL DEFAULT 0,
  distro_id TEXT NOT NULL DEFAULT '',
  distro_version TEXT NOT NULL DEFAULT '',
  kernel_version TEXT NOT NULL DEFAULT '',
  session_type TEXT NOT NULL DEFAULT '',
  channel TEXT NOT NULL DEFAULT '',
  runtime_engine TEXT NOT NULL DEFAULT '',
  runtime_version TEXT NOT NULL DEFAULT '',
  failure_kind TEXT NOT NULL DEFAULT '',
  failure_reason TEXT NOT NULL DEFAULT '',
  exit_code INTEGER,
  recovery TEXT NOT NULL DEFAULT '',
  gpu_mode TEXT NOT NULL DEFAULT 'unknown',
  events INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (date, fingerprint, install_id)
);

CREATE INDEX IF NOT EXISTS report_installations_fingerprint_date
  ON report_installations (fingerprint, date);

-- Event facts preserve every observed diagnostic environment. Empty install_id
-- is the sentinel for an event that did not carry an anonymous installation ID;
-- it contributes to event totals and dimension filters but never device counts.
CREATE TABLE IF NOT EXISTS report_event_dimensions (
  date TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  install_id TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  arch TEXT NOT NULL,
  os_build INTEGER NOT NULL DEFAULT 0,
  os_revision INTEGER NOT NULL DEFAULT 0,
  distro_id TEXT NOT NULL DEFAULT '',
  distro_version TEXT NOT NULL DEFAULT '',
  kernel_version TEXT NOT NULL DEFAULT '',
  session_type TEXT NOT NULL DEFAULT '',
  channel TEXT NOT NULL DEFAULT '',
  runtime_engine TEXT NOT NULL DEFAULT '',
  runtime_version TEXT NOT NULL DEFAULT '',
  failure_kind TEXT NOT NULL DEFAULT '',
  failure_reason TEXT NOT NULL DEFAULT '',
  exit_code TEXT NOT NULL DEFAULT 'unknown',
  recovery TEXT NOT NULL DEFAULT '',
  gpu_mode TEXT NOT NULL DEFAULT 'unknown',
  events INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (
    date, fingerprint, install_id, version, os, arch, os_build, os_revision,
    distro_id, distro_version, kernel_version, session_type, channel,
    runtime_engine, runtime_version, failure_kind, failure_reason, exit_code, recovery, gpu_mode
  )
);

CREATE INDEX IF NOT EXISTS report_event_dimensions_fingerprint_date
  ON report_event_dimensions (fingerprint, date);

CREATE INDEX IF NOT EXISTS pings_diagnostics_window
  ON pings (date, os, os_build, distro_id, session_type, channel);

CREATE TABLE IF NOT EXISTS diagnostics_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

INSERT OR IGNORE INTO diagnostics_meta (key, value)
VALUES ('installation_linked_since', date('now'));

-- Legacy local auth — superseded by id.reasonix.io identity + the `access`
-- table below. Kept during the transition; migrate-access.sql copies roles over.
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL,
  approved_at TEXT,
  approved_by INTEGER
);

CREATE TABLE IF NOT EXISTS sessions (
  token TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS sessions_user ON sessions (user_id);

-- Dashboard authorization keyed by the shared account email. Identity (login,
-- password, verification) lives in id.reasonix.io; this only maps email → role.
CREATE TABLE IF NOT EXISTS access (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL,
  approved_at TEXT,
  approved_by TEXT
);

CREATE TABLE IF NOT EXISTS audit_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL,
  actor_id INTEGER,
  actor_email TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT ''
);
