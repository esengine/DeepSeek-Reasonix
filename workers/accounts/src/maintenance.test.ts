import { describe, expect, it } from "vitest";
import { purgeExpiredAuthState } from "./maintenance";

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
        ];
      },
    } as unknown as D1Database;
    const now = new Date("2026-09-27T00:00:00.000Z");

    await expect(purgeExpiredAuthState({ DB: db }, now)).resolves.toBe(26);
    expect(statements).toEqual([
      { sql: "DELETE FROM device_grants WHERE expires_at <= ?1", cutoff: now.toISOString() },
      { sql: "DELETE FROM email_tokens WHERE expires_at <= ?1", cutoff: now.toISOString() },
      { sql: "DELETE FROM sessions WHERE expires_at <= ?1", cutoff: now.toISOString() },
      { sql: "DELETE FROM remote_connection_grants WHERE expires_at <= ?1", cutoff: now.toISOString() },
    ]);
  });
});
