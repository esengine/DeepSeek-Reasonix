-- One-time: add the crash-group summary column to the live reasonix-crash DB.
-- Apply once: wrangler d1 execute reasonix-crash --remote --file=migrate-title.sql
-- Not idempotent (the ALTER errors if the column already exists). Fresh installs
-- get the column from schema.sql.
ALTER TABLE groups ADD COLUMN title TEXT NOT NULL DEFAULT '';
