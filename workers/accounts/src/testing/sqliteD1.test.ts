import { describe, expect, it } from "vitest";
import { sqliteD1 } from "./sqliteD1";

describe("sqliteD1 harness", () => {
  it("applies every migration and runs RETURNING statements", async () => {
    const { db } = sqliteD1();
    const row = await db.prepare("INSERT INTO email_tokens (token_hash, user_id, purpose, created_at, expires_at) VALUES (?1, ?2, ?3, ?4, ?4) RETURNING user_id")
      .bind("h", 7, "reset", "2026-01-01").first<{ user_id: number }>();
    expect(row?.user_id).toBe(7);
  });
});
