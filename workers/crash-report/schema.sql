-- Apply: wrangler d1 execute reasonix-crash --remote --file=schema.sql
CREATE TABLE IF NOT EXISTS groups (
  fingerprint TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  count INTEGER NOT NULL,
  first_seen TEXT NOT NULL,
  last_seen TEXT NOT NULL,
  last_version TEXT NOT NULL
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
