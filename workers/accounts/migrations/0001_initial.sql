-- Reasonix accounts: users, sessions, email tokens.
-- Apply: wrangler d1 migrations apply reasonix-accounts --local   (or --remote)

CREATE TABLE IF NOT EXISTS users (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  handle         TEXT    NOT NULL UNIQUE,        -- public identity, /u/<handle>
  email          TEXT    NOT NULL UNIQUE,        -- stored lowercased
  email_verified INTEGER NOT NULL DEFAULT 0,
  password_hash  TEXT,                            -- nullable: reserved for future OAuth-only users
  display_name   TEXT    NOT NULL DEFAULT '',
  avatar_url     TEXT    NOT NULL DEFAULT '',
  bio            TEXT    NOT NULL DEFAULT '',
  role           TEXT    NOT NULL DEFAULT 'member',  -- member | admin
  status         TEXT    NOT NULL DEFAULT 'active',  -- active | suspended | deleted
  created_at     TEXT    NOT NULL,
  updated_at     TEXT    NOT NULL
);

-- Sessions store sha256(raw cookie token); the raw token only ever lives in the
-- cookie, so a DB read can't resurrect a live session. `kind` is reserved so the
-- future desktop/CLI device-flow login reuses this same table.
CREATE TABLE IF NOT EXISTS sessions (
  token_hash   TEXT    PRIMARY KEY,
  user_id      INTEGER NOT NULL,
  kind         TEXT    NOT NULL DEFAULT 'web',    -- web | cli
  user_agent   TEXT    NOT NULL DEFAULT '',
  created_at   TEXT    NOT NULL,
  last_seen_at TEXT    NOT NULL,
  expires_at   TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_user ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_expires ON sessions (expires_at);

-- Single-use, hashed tokens for email verification and password reset.
CREATE TABLE IF NOT EXISTS email_tokens (
  token_hash TEXT    PRIMARY KEY,
  user_id    INTEGER NOT NULL,
  purpose    TEXT    NOT NULL,                    -- verify | reset
  created_at TEXT    NOT NULL,
  expires_at TEXT    NOT NULL,
  used_at    TEXT
);
CREATE INDEX IF NOT EXISTS email_tokens_user ON email_tokens (user_id);
