CREATE TABLE IF NOT EXISTS feedback_adoptions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  install_hash TEXT NOT NULL,
  item_key TEXT NOT NULL,
  receipt TEXT NOT NULL UNIQUE,
  version TEXT NOT NULL,
  adopted_at TEXT NOT NULL,
  tombstoned_at TEXT,
  UNIQUE (install_hash, item_key)
);

CREATE INDEX IF NOT EXISTS feedback_adoptions_install ON feedback_adoptions (install_hash);
CREATE INDEX IF NOT EXISTS feedback_adoptions_item ON feedback_adoptions (item_key);

CREATE TABLE IF NOT EXISTS feedback_level_state (
  install_hash TEXT PRIMARY KEY,
  high_water_count INTEGER NOT NULL DEFAULT 0,
  revoked_at TEXT,
  updated_at TEXT NOT NULL
);

-- One-time marker for installs revoked before this table existed, recorded by a
-- marker row so a rerun never re-marks one a maintainer has since re-trusted.
-- Only installs without live trust are marked.
INSERT OR IGNORE INTO feedback_level_state (install_hash, high_water_count, revoked_at, updated_at)
SELECT h, 0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now') FROM (
  SELECT install_hash AS h FROM feedback WHERE status = 'rejected'
  UNION
  SELECT substr(u.detail, instr(u.detail, 'install:') + 8, 64) FROM feedback_audit u
  WHERE u.action = 'untrust' AND instr(u.detail, 'install:') > 0
    AND NOT EXISTS (SELECT 1 FROM feedback_audit t WHERE t.action = 'trust' AND t.id > u.id AND instr(t.detail, 'install:') > 0
      AND substr(t.detail, instr(t.detail, 'install:') + 8, 64) = substr(u.detail, instr(u.detail, 'install:') + 8, 64))
  UNION
  SELECT f.install_hash FROM feedback f JOIN feedback_audit a ON a.action = 'takedown' AND a.detail LIKE f.receipt || '%'
)
WHERE length(h) = 64
  AND NOT EXISTS (SELECT 1 FROM feedback_config WHERE key = 'revocation_backfilled_v1')
  AND h NOT IN (SELECT install_hash FROM feedback_trust);

INSERT OR IGNORE INTO feedback_config (key, value, updated_at) VALUES ('revocation_backfilled_v1', 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
