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

  it("paginates topic posts with a stable created-at and id cursor", async () => {
    const queries: string[] = [];
    const topic = { id: 7, title: "A topic", status: "open", pinned: 0, acceptedPostId: null, replyCount: 2, viewCount: 0, createdAt: "2026-08-05T00:00:00.000Z", category: "help" };
    const posts = [
      { id: 2, author: "alice", handle: "alice", body: "first", status: "visible", likeCount: 0, createdAt: "2026-08-05T00:01:00.000Z", editedAt: null, trust: 0, role: "member", liked: 0 },
      { id: 3, author: "bob", handle: "bob", body: "second", status: "visible", likeCount: 0, createdAt: "2026-08-05T00:02:00.000Z", editedAt: null, trust: 0, role: "member", liked: 0 },
    ];
    const db = {
      prepare(query: string) {
        queries.push(query);
        const statement = {
          bind() { return statement; },
          async first<T>() { return query.includes("FROM topics") ? topic as T : null; },
          async run() { return { meta: { changes: 1 } }; },
          async all<T>() { return { results: (query.includes("FROM posts") ? posts : []) as T[] }; },
        };
        return statement;
      },
    } as unknown as D1Database;

    const response = await app.request(
      "https://forum.reasonix.io/topics/7?limit=1&after=2026-08-05T00%3A00%3A00.000Z&afterId=1",
      {},
      bindings(db),
    );
    expect(response.status).toBe(200);
    const payload = await response.json() as { posts: typeof posts; pageInfo: Record<string, unknown> };
    expect(payload.posts).toHaveLength(1);
    expect(payload.posts[0].id).toBe(2);
    expect(payload.pageInfo).toMatchObject({ limit: 1, hasMore: true, nextAfter: posts[0].createdAt, nextAfterId: 2 });
    expect(queries.find((sql) => sql.includes("FROM posts"))).toContain("LIMIT ?5");
    expect(queries.find((sql) => sql.includes("FROM posts"))).toContain("p.created_at > ?3");
  });
});
