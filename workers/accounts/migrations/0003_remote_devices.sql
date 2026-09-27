-- Registered devices and short-lived remote connection grants.
-- Raw device credentials and grant tickets are never stored.

CREATE TABLE IF NOT EXISTS remote_devices (
  id                TEXT    PRIMARY KEY,
  user_id           INTEGER NOT NULL,
  credential_hash   TEXT    NOT NULL UNIQUE,
  name              TEXT    NOT NULL,
  platform          TEXT    NOT NULL,
  public_key        TEXT    NOT NULL,
  capabilities      TEXT    NOT NULL,
  created_at        TEXT    NOT NULL,
  updated_at        TEXT    NOT NULL,
  last_seen_at      TEXT,
  revoked_at        TEXT,
  UNIQUE (user_id, public_key)
);
CREATE INDEX IF NOT EXISTS remote_devices_user ON remote_devices (user_id, revoked_at);

CREATE TABLE IF NOT EXISTS remote_connection_grants (
  ticket_hash       TEXT    PRIMARY KEY,
  user_id           INTEGER NOT NULL,
  target_device_id  TEXT    NOT NULL,
  scopes            TEXT    NOT NULL,
  created_at        TEXT    NOT NULL,
  expires_at        TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS remote_connection_grants_expires ON remote_connection_grants (expires_at);
CREATE INDEX IF NOT EXISTS remote_connection_grants_user ON remote_connection_grants (user_id, target_device_id);
