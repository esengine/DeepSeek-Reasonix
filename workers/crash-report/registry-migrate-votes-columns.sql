-- Votes, part 1 of 2: the packages columns. SQLite has no ADD COLUMN IF NOT
-- EXISTS, so this file runs exactly once, before part 2 and before deploying:
--   wrangler d1 execute reasonix-registry --remote --file=registry-migrate-votes-columns.sql
ALTER TABLE packages ADD COLUMN up_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE packages ADD COLUMN down_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE packages ADD COLUMN rec_score REAL NOT NULL DEFAULT 0;
