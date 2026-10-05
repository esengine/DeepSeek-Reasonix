-- One-time: add structured crash/exception fields and sample-retention metadata.
-- Apply once before deploying the matching worker:
-- wrangler d1 execute reasonix-crash --remote --file=migrate-structured-reports.sql

ALTER TABLE groups ADD COLUMN first_version TEXT NOT NULL DEFAULT '';
ALTER TABLE groups ADD COLUMN source TEXT NOT NULL DEFAULT 'legacy';
ALTER TABLE groups ADD COLUMN label TEXT NOT NULL DEFAULT '';
ALTER TABLE groups ADD COLUMN error_type TEXT NOT NULL DEFAULT '';
ALTER TABLE groups ADD COLUMN top_frame TEXT NOT NULL DEFAULT '';
ALTER TABLE groups ADD COLUMN severity TEXT NOT NULL DEFAULT 'medium';
ALTER TABLE groups ADD COLUMN last_os TEXT NOT NULL DEFAULT '';
ALTER TABLE groups ADD COLUMN last_arch TEXT NOT NULL DEFAULT '';
ALTER TABLE groups ADD COLUMN last_build_commit TEXT NOT NULL DEFAULT '';
ALTER TABLE groups ADD COLUMN last_channel TEXT NOT NULL DEFAULT '';
ALTER TABLE groups ADD COLUMN resolved_in TEXT NOT NULL DEFAULT '';
ALTER TABLE groups ADD COLUMN resolved_at TEXT NOT NULL DEFAULT '';
ALTER TABLE groups ADD COLUMN regressed_at TEXT NOT NULL DEFAULT '';
ALTER TABLE groups ADD COLUMN last_sample_at TEXT NOT NULL DEFAULT '';

ALTER TABLE reports ADD COLUMN source TEXT NOT NULL DEFAULT 'legacy';
ALTER TABLE reports ADD COLUMN label TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN error_type TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN error_message TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN top_frame TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN build_commit TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN channel TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN language TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN view TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN breadcrumbs TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN component_stack TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN stack TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN occurred_at TEXT NOT NULL DEFAULT '';

UPDATE groups SET first_version = last_version WHERE first_version = '';
UPDATE groups SET last_sample_at = last_seen WHERE last_sample_at = '';
