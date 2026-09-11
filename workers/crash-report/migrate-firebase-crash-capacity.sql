-- Phase 2: Spark capacity accounting, lifecycle state, and generation leases.
-- The phase-1 lease table remains in place for an old Worker during rollout.
CREATE TABLE IF NOT EXISTS firebase_crash_group_state (
  fingerprint TEXT PRIMARY KEY,
  sample_state TEXT NOT NULL DEFAULT 'active'
    CHECK (sample_state IN ('active', 'compacted', 'archiving', 'archived')),
  sample_epoch INTEGER NOT NULL DEFAULT 1 CHECK (sample_epoch >= 1),
  epoch_first_event_id TEXT NOT NULL DEFAULT '',
  reserved_bytes INTEGER NOT NULL DEFAULT 655360 CHECK (reserved_bytes >= 0),
  last_seen TEXT NOT NULL,
  compacted_at TEXT NOT NULL DEFAULT '',
  archived_at TEXT NOT NULL DEFAULT '',
  archive_reason TEXT NOT NULL DEFAULT ''
    CHECK (archive_reason IN ('', 'retention', 'admin')),
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_generation INTEGER NOT NULL DEFAULT 0 CHECK (lease_generation >= 0),
  lease_expires_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS firebase_crash_group_state_lifecycle
  ON firebase_crash_group_state (sample_state, last_seen);

CREATE INDEX IF NOT EXISTS firebase_crash_group_state_lease
  ON firebase_crash_group_state (lease_expires_at);

-- Seed existing D1 groups with the same 30/60-day classification used by the
-- migration command. Its preflight enforces the 700 MiB budget before this
-- statement runs.
INSERT OR IGNORE INTO firebase_crash_group_state (
  fingerprint, sample_state, sample_epoch, reserved_bytes, last_seen,
  compacted_at, archived_at, archive_reason
)
SELECT fingerprint,
  CASE
    WHEN status IN ('resolved', 'ignored') AND datetime(last_seen) <= datetime('now', '-60 days')
      THEN 'archived'
    WHEN status IN ('resolved', 'ignored') AND datetime(last_seen) <= datetime('now', '-30 days')
      THEN 'compacted'
    ELSE 'active'
  END,
  1,
  CASE
    WHEN status IN ('resolved', 'ignored') AND datetime(last_seen) <= datetime('now', '-60 days') THEN 0
    WHEN status IN ('resolved', 'ignored') AND datetime(last_seen) <= datetime('now', '-30 days') THEN 131072
    ELSE 655360
  END,
  last_seen,
  CASE WHEN status IN ('resolved', 'ignored') AND datetime(last_seen) <= datetime('now', '-30 days')
    AND datetime(last_seen) > datetime('now', '-60 days') THEN datetime('now') ELSE '' END,
  CASE WHEN status IN ('resolved', 'ignored') AND datetime(last_seen) <= datetime('now', '-60 days')
    THEN datetime('now') ELSE '' END,
  CASE WHEN status IN ('resolved', 'ignored') AND datetime(last_seen) <= datetime('now', '-60 days')
    THEN 'retention' ELSE '' END
FROM groups;
