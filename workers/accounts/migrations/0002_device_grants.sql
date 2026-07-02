-- Device authorization grants (RFC 8628-style) for CLI/desktop sign-in.
-- Apply: wrangler d1 migrations apply reasonix-accounts --local   (or --remote)

CREATE TABLE IF NOT EXISTS device_grants (
  device_code_hash TEXT    PRIMARY KEY,             -- sha256(pepper:device_code); the raw code lives only on the client
  user_code        TEXT    NOT NULL UNIQUE,         -- canonical (no separators, upper) code the human types to approve
  user_id          INTEGER,                          -- null until approved
  status           TEXT    NOT NULL DEFAULT 'pending', -- pending | approved | denied
  kind             TEXT    NOT NULL DEFAULT 'cli',   -- session kind minted on claim (web | cli)
  user_agent       TEXT    NOT NULL DEFAULT '',
  created_at       TEXT    NOT NULL,
  last_polled_at   TEXT,                              -- drives the slow_down hint
  approved_at      TEXT,
  expires_at       TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS device_grants_user_code ON device_grants (user_code);
CREATE INDEX IF NOT EXISTS device_grants_expires ON device_grants (expires_at);
