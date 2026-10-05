-- Apply once:
-- wrangler d1 execute reasonix-crash --remote --file=migrate-metric-users.sql

CREATE TABLE IF NOT EXISTS metric_users (
  date TEXT NOT NULL,
  signal TEXT NOT NULL,
  bucket TEXT NOT NULL,
  install_id TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  PRIMARY KEY (date, signal, bucket, install_id)
);
