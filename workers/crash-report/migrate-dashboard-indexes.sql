-- Indexes for the dashboard's filter/aggregation queries. Apply:
--   wrangler d1 execute reasonix-crash --remote --file=migrate-dashboard-indexes.sql
-- groups has no index but crashGroups filters/sorts by these columns every load.
-- The date-partitioned tables are deliberately left alone — see
-- migrate-window-index-fix.sql for why a GROUP BY index loses to their PK.
CREATE INDEX IF NOT EXISTS groups_status ON groups (status);
CREATE INDEX IF NOT EXISTS groups_source ON groups (source);
CREATE INDEX IF NOT EXISTS groups_last_version ON groups (last_version);
CREATE INDEX IF NOT EXISTS groups_last_os ON groups (last_os);
CREATE INDEX IF NOT EXISTS groups_first_version ON groups (first_version);
