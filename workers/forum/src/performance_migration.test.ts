import { describe, expect, it } from "vitest";
// @ts-expect-error Node types are intentionally not part of the Worker build.
import { DatabaseSync } from "node:sqlite";
import schema from "../schema.sql?raw";
import migration from "../migrate-performance-indexes.sql?raw";

describe("forum performance indexes", () => {
  it("keeps fresh installs and the additive migration aligned", () => {
    for (const sql of [schema, migration]) {
      expect(sql).toMatch(/CREATE INDEX IF NOT EXISTS posts_author_created_at\s+ON posts \(author, created_at\)/);
      expect(sql).toMatch(/CREATE INDEX IF NOT EXISTS posts_visible_topic\s+ON posts \(topic_id, created_at\)\s+WHERE status = 'visible'/);
      expect(sql).toMatch(/CREATE INDEX IF NOT EXISTS topics_visible_latest\s+ON topics \(pinned DESC, last_post_at DESC\)\s+WHERE status <> 'hidden'/);
      expect(sql).toMatch(/CREATE INDEX IF NOT EXISTS topics_visible_top\s+ON topics \(reply_count DESC, last_post_at DESC\)\s+WHERE status <> 'hidden'/);
    }
  });

  it("keeps the migration additive and idempotent", () => {
    expect(migration).not.toMatch(/\b(?:DROP|ALTER)\b/);
    expect(migration.match(/CREATE INDEX IF NOT EXISTS/g)).toHaveLength(4);
  });

  it("removes full scans from the hot topic and post-count queries", () => {
    const db = new DatabaseSync(":memory:");
    try {
      db.exec(schema);
      db.exec(migration);
      const postsPlan = db.prepare(
        "EXPLAIN QUERY PLAN SELECT COUNT(*) FROM posts WHERE author = 'alice' AND created_at > '2026-01-01'",
      ).all().map((row: Record<string, unknown>) => String(row.detail)).join(" ");
      const topicsPlan = db.prepare(
        "EXPLAIN QUERY PLAN SELECT id FROM topics WHERE status <> 'hidden' ORDER BY pinned DESC, last_post_at DESC LIMIT 50",
      ).all().map((row: Record<string, unknown>) => String(row.detail)).join(" ");
      expect(postsPlan).toContain("USING COVERING INDEX posts_author_created_at");
      expect(topicsPlan).toContain("USING INDEX topics_visible_latest");
      expect(topicsPlan).not.toContain("USE TEMP B-TREE FOR ORDER BY");
    } finally {
      db.close();
    }
  });
});
