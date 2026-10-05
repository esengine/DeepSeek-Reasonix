import { describe, expect, it } from "vitest";
import { purgeExpiredAuthState } from "./maintenance";
import { sqliteD1 } from "./testing/sqliteD1";

describe("purgeExpiredAuthState", () => {
  it("removes only expired rows from bounded auth-state tables", async () => {
    const statements: Array<{ sql: string; cutoff: unknown }> = [];
    const db = {
      prepare(sql: string) {
        return {
          bind(cutoff: unknown) {
            statements.push({ sql, cutoff });
            return this;
          },
        };
      },
      async batch() {
        return [
          { meta: { changes: 3 } },
          { meta: { changes: 5 } },
          { meta: { changes: 7 } },
          { meta: { changes: 11 } },
          { meta: { changes: 13 } },
          { meta: { changes: 17 } },
          { meta: { changes: 19 } },
          { meta: { changes: 23 } },
        ];
      },
    } as unknown as D1Database;
    const now = new Date("2026-09-27T00:00:00.000Z");

    await expect(purgeExpiredAuthState({ DB: db }, now)).resolves.toBe(98);
    expect(statements).toEqual([
      { sql: "DELETE FROM device_grants WHERE expires_at <= ?1", cutoff: now.toISOString() },
      { sql: "DELETE FROM email_tokens WHERE expires_at <= ?1", cutoff: now.toISOString() },
      { sql: "DELETE FROM sessions WHERE expires_at <= ?1", cutoff: now.toISOString() },
      { sql: "DELETE FROM remote_connection_grants WHERE expires_at <= ?1", cutoff: now.toISOString() },
      { sql: "DELETE FROM remote_attachment_grants WHERE expires_at <= ?1", cutoff: now.toISOString() },
      { sql: "DELETE FROM remote_controller_challenges WHERE expires_at <= ?1", cutoff: now.toISOString() },
      { sql: "DELETE FROM remote_rate_counters WHERE expires_at <= ?1", cutoff: now.toISOString() },
      {
        sql: "UPDATE remote_controllers SET state = 'revoked', revoked_at = ?1 WHERE state = 'pending' AND expires_at <= ?1",
        cutoff: now.toISOString(),
      },
    ]);
  });
});

describe("purgeExpiredAuthState on the real schema", () => {
  it("drops expired challenges and counters, keeps live ones, and revokes expired pending controllers without deleting them", async () => {
    const { db, raw } = sqliteD1();
    const now = new Date("2026-10-05T12:00:00.000Z");
    const past = "2026-10-05T11:00:00.000Z";
    const future = "2026-10-05T13:00:00.000Z";
    for (const [hash, expires] of [["old", past], ["live", future]]) {
      raw.prepare("INSERT INTO remote_controller_challenges VALUES (?1, 1, 'h', 's', 'enroll', 't', ?2)").run(hash, expires);
      raw.prepare("INSERT INTO remote_rate_counters VALUES (?1, 0, 1, ?2)").run(hash, expires);
    }
    const insert = raw.prepare(`INSERT INTO remote_controllers (id, user_id, host_device_id, key_thumbprint, public_key, name, state, requester_session_hash, created_at, last_seen_at, expires_at)
      VALUES (?1, 1, 'h', ?1, '{}', 'n', ?2, 's', 't', 't', ?3)`);
    insert.run("stale", "pending", past);
    insert.run("waiting", "pending", future);
    await purgeExpiredAuthState({ DB: db }, now);
    expect(raw.prepare("SELECT nonce_hash FROM remote_controller_challenges").all()).toEqual([{ nonce_hash: "live" }]);
    expect(raw.prepare("SELECT counter_key FROM remote_rate_counters").all()).toEqual([{ counter_key: "live" }]);
    expect(raw.prepare("SELECT id, state FROM remote_controllers ORDER BY id").all()).toEqual([
      { id: "stale", state: "revoked" }, { id: "waiting", state: "pending" },
    ]);
  });
});
