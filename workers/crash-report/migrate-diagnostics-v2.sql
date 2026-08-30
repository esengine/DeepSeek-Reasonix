-- Diagnostics v2: additive cross-platform attribution and 30-day installation counts.
-- Apply after a D1 backup and before deploying the matching Worker.

ALTER TABLE reports ADD COLUMN webview2 TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN web_runtime TEXT NOT NULL DEFAULT '';

ALTER TABLE pings ADD COLUMN os_build INTEGER NOT NULL DEFAULT 0;
ALTER TABLE pings ADD COLUMN os_revision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE pings ADD COLUMN channel TEXT NOT NULL DEFAULT '';
ALTER TABLE pings ADD COLUMN distro_id TEXT NOT NULL DEFAULT '';
ALTER TABLE pings ADD COLUMN distro_version TEXT NOT NULL DEFAULT '';
ALTER TABLE pings ADD COLUMN kernel_version TEXT NOT NULL DEFAULT '';
ALTER TABLE pings ADD COLUMN session_type TEXT NOT NULL DEFAULT '';
ALTER TABLE pings ADD COLUMN runtime_engine TEXT NOT NULL DEFAULT '';
ALTER TABLE pings ADD COLUMN runtime_version TEXT NOT NULL DEFAULT '';
ALTER TABLE pings ADD COLUMN gpu_mode TEXT NOT NULL DEFAULT '';
ALTER TABLE cli_pings ADD COLUMN os_build INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cli_pings ADD COLUMN os_revision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cli_pings ADD COLUMN channel TEXT NOT NULL DEFAULT '';
ALTER TABLE cli_pings ADD COLUMN distro_id TEXT NOT NULL DEFAULT '';
ALTER TABLE cli_pings ADD COLUMN distro_version TEXT NOT NULL DEFAULT '';
ALTER TABLE cli_pings ADD COLUMN kernel_version TEXT NOT NULL DEFAULT '';
ALTER TABLE cli_pings ADD COLUMN session_type TEXT NOT NULL DEFAULT '';
ALTER TABLE cli_pings ADD COLUMN runtime_engine TEXT NOT NULL DEFAULT '';
ALTER TABLE cli_pings ADD COLUMN runtime_version TEXT NOT NULL DEFAULT '';
ALTER TABLE cli_pings ADD COLUMN gpu_mode TEXT NOT NULL DEFAULT '';

-- metric_users and cli_metric_users were retired in #9379 and must not be recreated.
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

-- Empty install_id preserves unlinked events for filtered event totals without
-- inventing a device identity; device counts always exclude the empty sentinel.
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
