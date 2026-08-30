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

-- Firebase Spark delivery is coordinated through D1 so an unavailable or
-- quota-limited Realtime Database never loses a sanitized report. The payload
-- is deleted immediately after Firebase accepts the group/sample projection.
CREATE TABLE IF NOT EXISTS firebase_crash_outbox (
  event_id TEXT PRIMARY KEY,
  fingerprint TEXT NOT NULL,
  payload TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'processing', 'projected')),
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS firebase_crash_outbox_retry
  ON firebase_crash_outbox (state, next_attempt_at, created_at);

CREATE TABLE IF NOT EXISTS firebase_crash_receipts (
  event_id TEXT PRIMARY KEY,
  projected_at TEXT NOT NULL,
  group_count INTEGER NOT NULL,
  latest_slot INTEGER NOT NULL,
  first_sample INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS firebase_crash_receipts_projected
  ON firebase_crash_receipts (projected_at);

-- Serializes each group's D1 projection, Firebase ring update, metadata sync,
-- and administrative deletion across Worker isolates. Expired owners are
-- recoverable, while owner-checked release prevents an old request from
-- deleting a newer lease.
CREATE TABLE IF NOT EXISTS firebase_crash_group_leases (
  fingerprint TEXT PRIMARY KEY,
  owner TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

-- Durable Firebase lifecycle, capacity reservation, and fencing state. The
-- older lease table above remains for rolling-deployment compatibility only.
CREATE TABLE IF NOT EXISTS firebase_crash_group_state (
  fingerprint TEXT PRIMARY KEY,
  sample_state TEXT NOT NULL DEFAULT 'active'
    CHECK (sample_state IN ('active', 'compacted', 'archiving', 'archived')),
  sample_epoch INTEGER NOT NULL DEFAULT 1 CHECK (sample_epoch >= 1),
  epoch_first_event_id TEXT NOT NULL DEFAULT '',
  reserved_bytes INTEGER NOT NULL DEFAULT 655360 CHECK (reserved_bytes >= 0),
  last_seen TEXT NOT NULL,
  compacted_at TEXT NOT NULL DEFAULT '',
  archived_at TEXT NOT NULL DEFAULT '',
  archive_reason TEXT NOT NULL DEFAULT ''
    CHECK (archive_reason IN ('', 'retention', 'admin')),
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_generation INTEGER NOT NULL DEFAULT 0 CHECK (lease_generation >= 0),
  lease_expires_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS firebase_crash_group_state_lifecycle
  ON firebase_crash_group_state (sample_state, last_seen);

CREATE INDEX IF NOT EXISTS firebase_crash_group_state_lease
  ON firebase_crash_group_state (lease_expires_at);

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
