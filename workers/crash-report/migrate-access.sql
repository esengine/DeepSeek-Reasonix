-- Unify dashboard auth onto id.reasonix.io: dashboard access is now a per-email
-- role, not a local password/session. Identity is resolved from the shared
-- account service; this table only records who may view the dashboard.
-- Apply: wrangler d1 execute reasonix-crash --remote --file=migrate-access.sql
CREATE TABLE IF NOT EXISTS access (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL,
  approved_at TEXT,
  approved_by TEXT
);

-- Carry over every existing dashboard account's role by email, so current
-- admins/viewers keep their access when they next sign in via id.reasonix.io.
INSERT OR IGNORE INTO access (email, role, created_at, approved_at)
  SELECT lower(email), role, created_at, approved_at FROM users;
