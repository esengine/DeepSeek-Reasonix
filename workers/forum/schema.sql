-- Reasonix Community forum. Identity comes from id.reasonix.io; this DB holds
-- forum content plus the anti-abuse state (trust levels, flags, silences).
-- Apply: wrangler d1 execute reasonix-forum --remote --file=schema.sql

-- A member is a signed-in identity that has interacted with the forum. Trust
-- level is the primary anti-spam lever: new members (trust 0) are throttled and
-- can't post links/images; trust rises with real participation.
CREATE TABLE IF NOT EXISTS members (
  email          TEXT PRIMARY KEY,              -- lowercased id.reasonix.io identity
  handle         TEXT NOT NULL,
  trust          INTEGER NOT NULL DEFAULT 0,    -- 0 new · 1 basic · 2 member · 3 regular · 4 leader
  role           TEXT NOT NULL DEFAULT 'member',-- member · moderator · admin
  post_count     INTEGER NOT NULL DEFAULT 0,
  likes_received INTEGER NOT NULL DEFAULT 0,
  flagged_count  INTEGER NOT NULL DEFAULT 0,    -- times this member's content was actioned
  silenced_until TEXT,                          -- spam containment; NULL = not silenced
  created_at     TEXT NOT NULL,
  last_seen_at   TEXT
);

CREATE TABLE IF NOT EXISTS categories (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  slug              TEXT NOT NULL UNIQUE,
  name              TEXT NOT NULL,
  description       TEXT NOT NULL DEFAULT '',
  position          INTEGER NOT NULL DEFAULT 0,
  min_trust_to_post INTEGER NOT NULL DEFAULT 0  -- e.g. Announcements = staff-only via a high value
);

CREATE TABLE IF NOT EXISTS topics (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  category_id      INTEGER NOT NULL,
  author           TEXT NOT NULL,               -- members.email
  title            TEXT NOT NULL,
  slug             TEXT NOT NULL,
  status           TEXT NOT NULL DEFAULT 'open',-- open · solved · closed · hidden
  pinned           INTEGER NOT NULL DEFAULT 0,
  reply_count      INTEGER NOT NULL DEFAULT 0,
  view_count       INTEGER NOT NULL DEFAULT 0,
  accepted_post_id INTEGER,
  created_at       TEXT NOT NULL,
  last_post_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS topics_category ON topics (category_id, pinned DESC, last_post_at DESC);
CREATE INDEX IF NOT EXISTS topics_recent ON topics (last_post_at DESC);

CREATE TABLE IF NOT EXISTS posts (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  topic_id   INTEGER NOT NULL,
  author     TEXT NOT NULL,
  body       TEXT NOT NULL,                     -- markdown source
  status     TEXT NOT NULL DEFAULT 'visible',   -- visible · pending (held for review) · hidden · deleted
  like_count INTEGER NOT NULL DEFAULT 0,
  flag_count INTEGER NOT NULL DEFAULT 0,        -- auto-hides at a threshold
  created_at TEXT NOT NULL,
  edited_at  TEXT
);
CREATE INDEX IF NOT EXISTS posts_topic ON posts (topic_id, created_at);

CREATE TABLE IF NOT EXISTS reactions (
  post_id    INTEGER NOT NULL,
  member     TEXT NOT NULL,
  emoji      TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (post_id, member, emoji)
);

-- Abuse reports. One flag per reporter per post; flag_count on posts is derived
-- from these and drives auto-hide + the moderation queue.
CREATE TABLE IF NOT EXISTS flags (
  post_id    INTEGER NOT NULL,
  reporter   TEXT NOT NULL,
  reason     TEXT NOT NULL,                     -- spam · offensive · off_topic · other
  note       TEXT NOT NULL DEFAULT '',
  status     TEXT NOT NULL DEFAULT 'open',      -- open · actioned · dismissed
  created_at TEXT NOT NULL,
  PRIMARY KEY (post_id, reporter)
);
CREATE INDEX IF NOT EXISTS flags_open ON flags (status, created_at);

-- Moderator actions, for accountability.
CREATE TABLE IF NOT EXISTS mod_log (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  at          TEXT NOT NULL,
  actor       TEXT NOT NULL,
  action      TEXT NOT NULL,                    -- hide_post · silence_member · set_trust · resolve_flag · ...
  target      TEXT NOT NULL DEFAULT '',
  detail      TEXT NOT NULL DEFAULT ''
);
