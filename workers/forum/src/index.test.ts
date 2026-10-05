import { describe, expect, it } from "vitest";
import app from "./index";
import type { Bindings } from "./env";

function bindings(db: D1Database): Bindings {
  return {
    DB: db,
    APP_ORIGIN: "https://reasonix.io",
    ALLOWED_ORIGINS: "https://reasonix.io",
    ID_ORIGIN: "https://id.reasonix.io",
  };
}

describe("forum public API", () => {
  it("maps invalid query input to a client error", async () => {
    const db = { prepare: () => { throw new Error("database should not be reached"); } } as unknown as D1Database;
    const response = await app.request("https://forum.reasonix.io/topics?sort=invalid", {}, bindings(db));
    expect(response.status).toBe(422);
    await expect(response.json()).resolves.toMatchObject({ error: { code: "invalid_input" } });
  });

  it("selects public handles instead of stored author emails", async () => {
    const queries: string[] = [];
    const rows = [{
      id: 1,
      title: "Safe public topic",
      slug: "safe-public-topic",
      status: "open",
      pinned: 0,
      replyCount: 0,
      viewCount: 0,
      author: "alice",
      createdAt: "2026-08-05T00:00:00.000Z",
      lastPostAt: "2026-08-05T00:00:00.000Z",
      category: "help",
      categoryName: "Help & Support",
    }];
    const statement = {
      bind() { return this; },
      async all() { return { results: rows }; },
    };
    const db = {
      prepare(query: string) {
        queries.push(query);
        return statement;
      },
    } as unknown as D1Database;

    const response = await app.request("https://forum.reasonix.io/topics", {}, bindings(db));
    expect(response.status).toBe(200);
    const payload = await response.json();
    expect(payload).toEqual({ topics: rows });
    expect(queries[0]).toContain("m.handle, 'deleted') AS author");
    expect(queries[0]).not.toMatch(/\bt\.author\s*,/);
    expect(JSON.stringify(payload)).not.toContain("example.test");
  });
});
