-- Add isolated CLI telemetry tables without changing the existing Desktop
-- tables. This migration is additive and idempotent: it is safe before or
-- after deploying the surface-aware Worker, and safe to rerun.
-- Apply with:
--   wrangler d1 execute reasonix-crash --remote --file=migrate-client-surface.sql

CREATE TABLE IF NOT EXISTS cli_pings (
  date TEXT NOT NULL,
  install_id TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  arch TEXT NOT NULL,
  os_version TEXT NOT NULL DEFAULT '',
  opens INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (date, install_id)
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

CREATE TABLE IF NOT EXISTS cli_metric_users (
  date TEXT NOT NULL,
  signal TEXT NOT NULL,
  bucket TEXT NOT NULL,
  install_id TEXT NOT NULL,
  version TEXT NOT NULL,
  os TEXT NOT NULL,
  PRIMARY KEY (date, signal, bucket, install_id)
);

CREATE INDEX IF NOT EXISTS cli_pings_version ON cli_pings (version);
CREATE INDEX IF NOT EXISTS cli_metrics_signal_bucket ON cli_metrics (signal, bucket);
CREATE INDEX IF NOT EXISTS cli_metric_users_signal_bucket ON cli_metric_users (signal, bucket);
