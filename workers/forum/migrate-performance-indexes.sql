-- Additive indexes for the public forum read and anti-abuse paths.
-- Apply before deploying the matching Worker:
--   wrangler d1 execute reasonix-forum --remote --file=migrate-performance-indexes.sql

CREATE INDEX IF NOT EXISTS posts_author_created_at
  ON posts (author, created_at);

CREATE INDEX IF NOT EXISTS posts_visible_topic
  ON posts (topic_id, created_at)
  WHERE status = 'visible';

CREATE INDEX IF NOT EXISTS topics_visible_latest
  ON topics (pinned DESC, last_post_at DESC)
  WHERE status <> 'hidden';

CREATE INDEX IF NOT EXISTS topics_visible_top
  ON topics (reply_count DESC, last_post_at DESC)
  WHERE status <> 'hidden';
