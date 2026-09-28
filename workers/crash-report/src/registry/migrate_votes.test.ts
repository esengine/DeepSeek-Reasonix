import { describe, expect, it } from "vitest";
import { sqliteD1 } from "./sqlite_d1.testkit";
import legacySchema from "./testdata/registry-schema-before-votes.sql?raw";
import currentSchema from "../../registry-schema.sql?raw";
import addColumns from "../../registry-migrate-votes-columns.sql?raw";
import migrateVotes from "../../registry-migrate-votes.sql?raw";

function legacyDatabase() {
  const kit = sqliteD1(legacySchema);
  const pkg = kit.sqlite.prepare(
    `INSERT INTO packages (id, kind, scope_handle, name, slug, status, publisher_id, star_count, created_at, updated_at)
     VALUES (?1, 'skill', ?2, ?3, ?2 || '/' || ?3, 'active', ?4, ?5, 't', 't')`,
  );
  pkg.run(1, "alice", "review", 7, 3);
  pkg.run(2, "bob", "lint", 8, 1);
  const star = kit.sqlite.prepare("INSERT INTO stars (package_id, user_id, created_at) VALUES (?1, ?2, ?3)");
  star.run(1, 8, "2026-09-01T00:00:00.000Z");
  star.run(1, 9, "2026-09-02T00:00:00.000Z");
  star.run(1, 7, "2026-09-03T00:00:00.000Z"); // the publisher's own star
  star.run(2, 7, "2026-09-04T00:00:00.000Z");
  return kit;
}

const columnsOf = (sqlite: any, table: string) =>
  (sqlite.prepare(`PRAGMA table_info(${table})`).all() as { name: string; type: string; notnull: number; dflt_value: string | null }[])
    .map((c) => `${c.name}:${c.type}:${c.notnull}:${c.dflt_value}`)
    .sort();

describe("votes migration", () => {
  it("turns stars into +1 votes, skips self-stars, and recounts", () => {
    const { sqlite, close } = legacyDatabase();
    try {
      sqlite.exec(addColumns);
      sqlite.exec(migrateVotes);
      const votes = sqlite.prepare("SELECT package_id, user_id, value, created_at FROM votes ORDER BY package_id, user_id").all();
      expect(votes).toEqual([
        { package_id: 1, user_id: 8, value: 1, created_at: "2026-09-01T00:00:00.000Z" },
        { package_id: 1, user_id: 9, value: 1, created_at: "2026-09-02T00:00:00.000Z" },
        { package_id: 2, user_id: 7, value: 1, created_at: "2026-09-04T00:00:00.000Z" },
      ]);
      const counts = sqlite.prepare("SELECT id, up_count, down_count, star_count FROM packages ORDER BY id").all();
      expect(counts).toEqual([
        { id: 1, up_count: 2, down_count: 0, star_count: 3 },
        { id: 2, up_count: 1, down_count: 0, star_count: 1 },
      ]);
      expect((sqlite.prepare("SELECT COUNT(*) AS n FROM stars").get() as { n: number }).n).toBe(4);
    } finally {
      close();
    }
  });

  it("re-runs without resurrecting a withdrawn vote or overwriting a new one", () => {
    const { sqlite, close } = legacyDatabase();
    try {
      sqlite.exec(addColumns);
      sqlite.exec(migrateVotes);
      sqlite.exec("DELETE FROM votes WHERE package_id = 1 AND user_id = 8");
      sqlite.exec("UPDATE votes SET value = -1 WHERE package_id = 1 AND user_id = 9");
      sqlite.exec(migrateVotes);
      expect(sqlite.prepare("SELECT user_id, value FROM votes WHERE package_id = 1").all()).toEqual([{ user_id: 9, value: -1 }]);
      expect(sqlite.prepare("SELECT up_count, down_count FROM packages WHERE id = 1").get()).toEqual({ up_count: 0, down_count: 1 });
    } finally {
      close();
    }
  });

  it("leaves a migrated database shaped like a fresh one", () => {
    const migrated = legacyDatabase();
    const fresh = sqliteD1(currentSchema);
    try {
      migrated.sqlite.exec(addColumns);
      migrated.sqlite.exec(migrateVotes);
      for (const table of ["packages", "votes", "package_install_seen"]) {
        expect(columnsOf(migrated.sqlite, table)).toEqual(columnsOf(fresh.sqlite, table));
      }
      const indexes = (s: any) =>
        (s.prepare("SELECT name FROM sqlite_master WHERE type = 'index' AND name NOT LIKE 'sqlite_%' ORDER BY name").all() as { name: string }[]).map((r) => r.name);
      expect(indexes(migrated.sqlite)).toEqual(indexes(fresh.sqlite));
    } finally {
      migrated.close();
      fresh.close();
    }
  });
});
