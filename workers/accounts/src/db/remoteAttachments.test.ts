import { describe, expect, it } from "vitest";
import migration from "../../migrations/0004_remote_attachments.sql?raw";
import { RemoteAttachmentRepo } from "./remoteAttachments";

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
        async run() { return { meta: { changes: 1 } }; },
      };
    },
  } as unknown as D1Database;
  return { db, statements };
}

const authorizedRow = {
  object_id: "a".repeat(64),
  user_id: 7,
  target_device_id: "b".repeat(64),
  max_bytes: 4096,
  ciphertext_bytes: 2048,
  ciphertext_sha256: "c".repeat(64),
  expires_at: "2026-09-27T00:15:00.000Z",
};

describe("RemoteAttachmentRepo", () => {
  it("issues separate upload and download tickets without storing either raw value", async () => {
    const { db, statements } = fakeDatabase();
    const grant = await new RemoteAttachmentRepo(db, "pepper").issue({
      userId: 7,
      targetDeviceId: "b".repeat(64),
      maxBytes: 4096,
      ttlMs: 15 * 60 * 1000,
    });

    expect(grant.objectId).toMatch(/^[0-9a-f]{64}$/);
    expect(grant.uploadTicket).toMatch(/^[0-9a-f]{64}$/);
    expect(grant.downloadTicket).toMatch(/^[0-9a-f]{64}$/);
    expect(grant.uploadTicket).not.toBe(grant.downloadTicket);
    expect(statements[0]?.values).not.toContain(grant.uploadTicket);
    expect(statements[0]?.values).not.toContain(grant.downloadTicket);
  });

  it("consumes an upload once and binds its ciphertext size and hash", async () => {
    const { db, statements } = fakeDatabase([authorizedRow]);
    const result = await new RemoteAttachmentRepo(db, "pepper").consumeUpload({
      objectId: authorizedRow.object_id,
      ticket: "d".repeat(64),
      ciphertextBytes: 2048,
      ciphertextSha256: authorizedRow.ciphertext_sha256,
    });

    expect(result?.ciphertextBytes).toBe(2048);
    expect(statements[0]?.sql).toContain("uploaded_at IS NULL");
    expect(statements[0]?.sql).toContain("d.revoked_at IS NULL");
    expect(statements[0]?.values).not.toContain("d".repeat(64));
  });

  it("authorizes downloads only after upload while the target device remains active", async () => {
    const { db, statements } = fakeDatabase([authorizedRow]);
    const result = await new RemoteAttachmentRepo(db, "pepper")
      .authorizeDownload(authorizedRow.object_id, "e".repeat(64));

    expect(result?.ciphertextSha256).toBe(authorizedRow.ciphertext_sha256);
    expect(statements[0]?.sql).toContain("uploaded_at IS NOT NULL");
    expect(statements[0]?.sql).toContain("d.revoked_at IS NULL");
  });

  it("keeps the migration additive and its expiring state indexed", () => {
    expect(migration).not.toMatch(/\b(?:DROP|ALTER|DELETE)\b/);
    expect(migration).toContain("CREATE TABLE IF NOT EXISTS remote_attachment_grants");
    expect(migration).toContain("remote_attachment_grants_expires");
  });
});
