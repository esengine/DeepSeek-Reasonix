import { describe, expect, it } from "vitest";
import { RemoteDeviceRepo } from "./remoteDevices";
import remoteDevicesMigration from "../../migrations/0003_remote_devices.sql?raw";

interface RecordedStatement {
  sql: string;
  values: unknown[];
}

function fakeDatabase(firstRows: unknown[] = []) {
  const statements: RecordedStatement[] = [];
  const rows = [...firstRows];
  const db = {
    prepare(sql: string) {
      const record = { sql, values: [] as unknown[] };
      statements.push(record);
      return {
        bind(...values: unknown[]) { record.values = values; return this; },
        async first() { return rows.shift() ?? null; },
        async all() { return { results: [] }; },
        async run() { return { meta: { changes: 1 } }; },
      };
    },
  } as unknown as D1Database;
  return { db, statements };
}

const deviceRow = {
  id: "a".repeat(64),
  user_id: 7,
  name: "Home Mac",
  platform: "macos",
  public_key: "A".repeat(43),
  capabilities: '["terminal","logs"]',
  created_at: "2026-09-27T00:00:00.000Z",
  updated_at: "2026-09-27T00:00:00.000Z",
  last_seen_at: null,
  revoked_at: null,
};

describe("RemoteDeviceRepo", () => {
  it("stores only a peppered device credential and makes registration retry-safe", async () => {
    const { db, statements } = fakeDatabase([deviceRow]);
    const result = await new RemoteDeviceRepo(db, "pepper").register({
      userId: 7,
      name: "Home Mac",
      platform: "macos",
      publicKey: "A".repeat(43),
      capabilities: ["terminal", "logs"],
    });

    expect(result.device.id).toBe(deviceRow.id);
    expect(result.deviceCredential).toMatch(/^[0-9a-f]{64}$/);
    expect(statements[0]?.sql).toContain("ON CONFLICT (user_id, public_key) DO UPDATE");
    expect(statements[0]?.values).not.toContain(result.deviceCredential);
    expect(statements[0]?.values[2]).toMatch(/^[0-9a-f]{64}$/);
  });

  it("issues a short-lived grant without storing its raw ticket", async () => {
    const { db, statements } = fakeDatabase();
    const before = Date.now();
    const grant = await new RemoteDeviceRepo(db, "pepper").issueGrant({
      userId: 7,
      targetDeviceId: "a".repeat(64),
      scopes: ["terminal"],
      ttlMs: 60_000,
    });

    expect(grant.ticket).toMatch(/^[0-9a-f]{64}$/);
    expect(statements[0]?.values).not.toContain(grant.ticket);
    expect(new Date(grant.expiresAt).getTime()).toBeGreaterThanOrEqual(before + 59_000);
  });

  it("atomically consumes a valid one-time grant", async () => {
    const { db, statements } = fakeDatabase([{
      user_id: 7,
      target_device_id: "a".repeat(64),
      scopes: '["terminal","logs"]',
    }]);
    const grant = await new RemoteDeviceRepo(db, "pepper").consumeGrant("b".repeat(64));

    expect(grant).toEqual({ userId: 7, targetDeviceId: "a".repeat(64), scopes: ["terminal", "logs"] });
    expect(statements[0]?.sql).toContain("DELETE FROM remote_connection_grants");
    expect(statements[0]?.sql).toContain("d.revoked_at IS NULL");
  });

  it("keeps the migration additive and indexes bounded state", () => {
    expect(remoteDevicesMigration).not.toMatch(/\b(?:DROP|ALTER|DELETE)\b/);
    expect(remoteDevicesMigration).toContain("CREATE TABLE IF NOT EXISTS remote_devices");
    expect(remoteDevicesMigration).toContain("CREATE TABLE IF NOT EXISTS remote_connection_grants");
    expect(remoteDevicesMigration).toContain("remote_connection_grants_expires");
  });
});
