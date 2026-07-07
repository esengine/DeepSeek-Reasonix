-- Indexes for the dashboard's filter/aggregation queries. Apply:
--   wrangler d1 execute reasonix-crash --remote --file=migrate-dashboard-indexes.sql
-- groups has no index but crashGroups filters/sorts by these columns every load;
-- the pings/metrics facets group by version and (signal,bucket) over a window.
CREATE INDEX IF NOT EXISTS groups_status ON groups (status);
CREATE INDEX IF NOT EXISTS groups_source ON groups (source);
CREATE INDEX IF NOT EXISTS groups_last_version ON groups (last_version);
CREATE INDEX IF NOT EXISTS groups_last_os ON groups (last_os);
CREATE INDEX IF NOT EXISTS groups_first_version ON groups (first_version);
CREATE INDEX IF NOT EXISTS pings_version ON pings (version);
CREATE INDEX IF NOT EXISTS metrics_signal_bucket ON metrics (signal, bucket);
CREATE INDEX IF NOT EXISTS metric_users_signal_bucket ON metric_users (signal, bucket);
