-- Additive Firebase Spark crash delivery state. Apply before selecting
-- CRASH_STORAGE_MODE=dual or firebase.
CREATE TABLE IF NOT EXISTS firebase_crash_outbox (
  event_id TEXT PRIMARY KEY,
  fingerprint TEXT NOT NULL,
  payload TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'processing', 'projected')),
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS firebase_crash_outbox_retry
  ON firebase_crash_outbox (state, next_attempt_at, created_at);

CREATE TABLE IF NOT EXISTS firebase_crash_receipts (
  event_id TEXT PRIMARY KEY,
  projected_at TEXT NOT NULL,
  group_count INTEGER NOT NULL,
  latest_slot INTEGER NOT NULL,
  first_sample INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS firebase_crash_receipts_projected
  ON firebase_crash_receipts (projected_at);

CREATE TABLE IF NOT EXISTS firebase_crash_group_leases (
  fingerprint TEXT PRIMARY KEY,
  owner TEXT NOT NULL,
  expires_at TEXT NOT NULL
);
