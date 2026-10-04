CREATE TABLE IF NOT EXISTS feedback_replies (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  receipt TEXT NOT NULL,
  author TEXT NOT NULL,
  body TEXT NOT NULL,
  handled INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS feedback_replies_receipt ON feedback_replies (receipt, id);
CREATE INDEX IF NOT EXISTS feedback_replies_unhandled ON feedback_replies (author, handled, id);

CREATE TABLE IF NOT EXISTS feedback_blocks (
  target TEXT PRIMARY KEY,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT
);

CREATE INDEX IF NOT EXISTS feedback_blocks_expires ON feedback_blocks (expires_at);

CREATE TABLE IF NOT EXISTS feedback_trust (
  install_hash TEXT PRIMARY KEY,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS feedback_public_images (
  key TEXT PRIMARY KEY,
  receipt TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS feedback_releases (
  receipt TEXT PRIMARY KEY,
  install_hash TEXT NOT NULL,
  released_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS feedback_releases_install ON feedback_releases (install_hash);

CREATE INDEX IF NOT EXISTS feedback_public_images_receipt ON feedback_public_images (receipt);

CREATE TABLE IF NOT EXISTS feedback_config (
  key TEXT PRIMARY KEY,
  value INTEGER NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS feedback_audit (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL,
  action TEXT NOT NULL,
  detail TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS feedback_audit_at ON feedback_audit (at);
CREATE INDEX IF NOT EXISTS feedback_install_status_updated ON feedback (install_hash, status, updated_at);

-- One-time backfill, recorded by a marker row rather than inferred from the trust
-- table being empty, so a later deploy never re-grants what was revoked. Installs
-- with a rejected item, a takedown, or a block are never backfilled.
INSERT OR IGNORE INTO feedback_trust (install_hash, created_at, expires_at)
SELECT install_hash, MIN(created_at), strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '+30 days') FROM feedback
WHERE status IN ('recorded','in_progress','fixed','wontfix','duplicate')
  AND NOT EXISTS (SELECT 1 FROM feedback_config WHERE key = 'trust_backfilled_v1')
  AND install_hash NOT IN (SELECT install_hash FROM feedback WHERE status = 'rejected')
  AND install_hash NOT IN (SELECT f.install_hash FROM feedback f JOIN feedback_audit a ON a.detail LIKE f.receipt || '%' WHERE a.action IN ('reject','takedown'))
  AND 'install:' || install_hash NOT IN (SELECT target FROM feedback_blocks)
GROUP BY install_hash;

INSERT OR IGNORE INTO feedback_config (key, value, updated_at) VALUES ('trust_backfilled_v1', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
