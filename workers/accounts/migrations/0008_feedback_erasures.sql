-- Pending "account deleted" notifications for the feedback worker. A row holds only the
-- numeric account id and is removed once the worker confirms. Additive and idempotent.
CREATE TABLE IF NOT EXISTS feedback_erasures (
  user_id         INTEGER PRIMARY KEY,
  created_at      TEXT    NOT NULL,
  attempts        INTEGER NOT NULL DEFAULT 0,
  last_attempt_at TEXT
);
