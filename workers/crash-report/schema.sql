-- Apply: wrangler d1 execute reasonix-crash --remote --file=schema.sql
CREATE TABLE IF NOT EXISTS groups (
  fingerprint TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  count INTEGER NOT NULL,
  first_seen TEXT NOT NULL,
  last_seen TEXT NOT NULL,
  last_version TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open',
  note TEXT NOT NULL DEFAULT ''
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
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS reports_fingerprint ON reports (fingerprint);

CREATE TABLE IF NOT EXISTS pings (
  date TEXT NOT NULL,
  install_id TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  arch TEXT NOT NULL,
  os_version TEXT NOT NULL DEFAULT '',
  opens INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (date, install_id)
);

-- Opt-in aggregate agent metrics: anonymous per-day (signal, bucket) counters,
-- no install id and no content. Generic shape so a new signal is just new rows.
CREATE TABLE IF NOT EXISTS metrics (
  date TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  signal TEXT NOT NULL,
  bucket TEXT NOT NULL,
  count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (date, version, os, signal, bucket)
);

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

CREATE TABLE IF NOT EXISTS audit_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL,
  actor_id INTEGER,
  actor_email TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT ''
);
