-- Apply: wrangler d1 execute reasonix-crash --remote --file=migrate-window-index-fix.sql
--
-- Every dashboard aggregate is `WHERE date >= ... GROUP BY <other columns>`, and
-- each table's primary key already starts with `date`. Offering SQLite a second
-- index on the GROUP BY columns makes it prefer an ordered full-index scan —
-- skipping the window prune and reading every historical row back through the
-- table. Measured on seeded copies (65 days retained, 30-day window):
--   metrics       180k rows:  807ms -> 88ms
--   metric_users  5.3M rows:  31.6s -> 3.3s
--   pings         568k rows:  672ms -> 459ms
-- Dropping them also drops their write cost on the ingest path.
--
-- Run this only against a worker that no longer creates the cli_* three:
-- ensureCLITelemetrySchema used to rebuild them on the next CLI request.
DROP INDEX IF EXISTS metrics_signal_bucket;
DROP INDEX IF EXISTS metric_users_signal_bucket;
DROP INDEX IF EXISTS pings_version;
DROP INDEX IF EXISTS cli_metrics_signal_bucket;
DROP INDEX IF EXISTS cli_metric_users_signal_bucket;
DROP INDEX IF EXISTS cli_pings_version;
