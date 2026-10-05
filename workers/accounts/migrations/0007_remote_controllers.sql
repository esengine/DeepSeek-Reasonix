-- Controller enrollment records for remote access: bookkeeping that no grant, gateway or host path reads yet.
-- Additive and idempotent. Rows are never deleted, so ordinal = max(ordinal)+1 never reuses a retired one,
-- and `revoked` is terminal.

CREATE TABLE IF NOT EXISTS remote_controllers (
  id                     TEXT    PRIMARY KEY,
  user_id                INTEGER NOT NULL,
  host_device_id         TEXT    NOT NULL,
  key_thumbprint         TEXT    NOT NULL,
  public_key             TEXT    NOT NULL,
  ordinal                INTEGER,
  name                   TEXT    NOT NULL,
  name_source            TEXT    NOT NULL DEFAULT 'auto',
  ua_class               TEXT    NOT NULL DEFAULT 'other',
  state                  TEXT    NOT NULL,
  claims_id              TEXT,
  requester_session_hash TEXT    NOT NULL,
  created_at             TEXT    NOT NULL,
  last_seen_at           TEXT    NOT NULL,
  revoked_at             TEXT,
  expires_at             TEXT,
  UNIQUE (user_id, host_device_id, key_thumbprint),
  UNIQUE (user_id, host_device_id, ordinal),
  CHECK (state IN ('pending', 'active', 'revoked')),
  CHECK (name_source IN ('auto', 'owner')),
  CHECK ((state = 'revoked') = (revoked_at IS NOT NULL)),
  CHECK (state != 'active' OR (ordinal IS NOT NULL AND expires_at IS NULL)),
  CHECK (ordinal IS NULL OR ordinal > 0)
);
CREATE INDEX IF NOT EXISTS remote_controllers_host ON remote_controllers (user_id, host_device_id, state);
CREATE INDEX IF NOT EXISTS remote_controllers_pending_expiry ON remote_controllers (expires_at) WHERE state = 'pending';

CREATE TABLE IF NOT EXISTS remote_controller_challenges (
  nonce_hash      TEXT    PRIMARY KEY,
  user_id         INTEGER NOT NULL,
  host_device_id  TEXT    NOT NULL,
  session_hash    TEXT    NOT NULL,
  purpose         TEXT    NOT NULL,
  created_at      TEXT    NOT NULL,
  expires_at      TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS remote_controller_challenges_expires ON remote_controller_challenges (expires_at);

CREATE TABLE IF NOT EXISTS remote_rate_counters (
  counter_key   TEXT    PRIMARY KEY,
  window_start  INTEGER NOT NULL,
  count         INTEGER NOT NULL,
  expires_at    TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS remote_rate_counters_expires ON remote_rate_counters (expires_at);

CREATE TRIGGER IF NOT EXISTS remote_controllers_revoked_terminal
BEFORE UPDATE ON remote_controllers
WHEN OLD.state = 'revoked' AND (NEW.state != 'revoked' OR NEW.revoked_at IS NOT OLD.revoked_at)
BEGIN
  SELECT RAISE(ABORT, 'revoked_is_terminal');
END;
