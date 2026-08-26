-- Apply: wrangler d1 execute reasonix-crash --remote --file=migrate-drop-metric-users.sql
--
-- Retires per-install metric detail. The 30-day COUNT(DISTINCT install_id) it
-- fed was the database's entire read bill: the primary key leads with `date`,
-- so the rollup's `date >= ... AND signal = ?` could only prune the window and
-- scanned all ~28M rows once per signal, eight signals an hour — ~5.4B rows
-- read a day for one dashboard module. The aggregate `metrics` tables keep the
-- same signals as event counts at ~180k rows total.
--
-- Deploy the worker first: it stops writing these tables and stops creating
-- them at runtime, so a DROP before that just gets rebuilt on the next request.
DROP TABLE IF EXISTS metric_user_rollup;
DROP TABLE IF EXISTS metric_user_rollup_state;
DROP TABLE IF EXISTS metric_users;
DROP TABLE IF EXISTS cli_metric_users;
