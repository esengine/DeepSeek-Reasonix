import { afterEach, describe, expect, it, vi } from "vitest";
import registryApp from "../app";
import type { Bindings } from "../env";
import { sqliteD1 } from "../sqlite_d1.testkit";
import { rescore } from "../db/ranking";
import { recommendScore } from "../lib/ranking";
import registrySchema from "../../../registry-schema.sql?raw";

const USERS: Record<string, { id: number; handle: string; role: string; emailVerified: boolean }> = {
  alice: { id: 7, handle: "alice", role: "member", emailVerified: true },
  bob: { id: 8, handle: "bob", role: "member", emailVerified: true },
  carol: { id: 9, handle: "carol", role: "member", emailVerified: true },
  dave: { id: 10, handle: "dave", role: "member", emailVerified: false },
  root: { id: 1, handle: "root", role: "admin", emailVerified: true },
};

function stubAccounts() {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const auth = (init?.headers as Record<string, string> | undefined)?.authorization ?? "";
      const user = USERS[auth.replace(/^Bearer /, "")];
      return user ? Response.json({ user }) : new Response("no", { status: 401 });
    }),
  );
}

function setup() {
  const kit = sqliteD1(registrySchema);
  const insert = kit.sqlite.prepare(
    `INSERT INTO packages (kind, scope_handle, name, slug, source, install_kind, latest_version, status, publisher_id, created_at, updated_at)
     VALUES ('skill', ?1, ?2, ?1 || '/' || ?2, 'https://github.com/o/r', 'skill', '0.1.0', ?3, ?4, ?5, ?5)`,
  );
  insert.run("alice", "review", "active", 7, "2026-09-20T00:00:00.000Z");
  insert.run("alice", "draft", "pending", 7, "2026-09-21T00:00:00.000Z");
  insert.run("bob", "lint", "active", 8, "2026-09-22T00:00:00.000Z");
  const env: Bindings = {
    DB: kit.db,
    ACCOUNTS_ORIGIN: "https://id.reasonix.test",
    APP_ORIGIN: "https://reasonix.test",
    ALLOWED_ORIGINS: "https://reasonix.test",
  };
  const call = (path: string, init: { method?: string; as?: string; body?: unknown } = {}) =>
    registryApp.fetch(
      new Request(`https://crash.reasonix.test${path}`, {
        method: init.method ?? "GET",
        headers: {
          ...(init.as ? { authorization: `Bearer ${init.as}` } : {}),
          ...(init.body !== undefined ? { "content-type": "application/json" } : {}),
        },
        body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
      }),
      env,
    );
  const vote = (slug: string, as: string, value: number) => call(`/v1/packages/${slug}/vote`, { method: "POST", as, body: { value } });
  const counts = (slug: string) =>
    kit.sqlite.prepare("SELECT up_count AS up, down_count AS down FROM packages WHERE slug = ?1").get(slug) as { up: number; down: number };
  const recount = (slug: string) =>
    kit.sqlite
      .prepare(
        `SELECT SUM(v.value = 1) AS up, SUM(v.value = -1) AS down FROM votes v
         JOIN packages p ON p.id = v.package_id WHERE p.slug = ?1`,
      )
      .get(slug) as { up: number | null; down: number | null };
  return { ...kit, env, call, vote, counts, recount };
}

afterEach(() => vi.unstubAllGlobals());

describe("votes", () => {
  it("records one changeable, withdrawable vote per account", async () => {
    stubAccounts();
    const t = setup();
    try {
      let res = await t.vote("alice/review", "bob", 1);
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({ value: 1, upCount: 1, downCount: 0, approvalRate: 1 });
      await t.vote("alice/review", "bob", 1);
      expect(t.counts("alice/review")).toEqual({ up: 1, down: 0 });
      res = await t.vote("alice/review", "bob", -1);
      expect(await res.json()).toMatchObject({ value: -1, upCount: 0, downCount: 1, approvalRate: 0 });
      await t.vote("alice/review", "carol", 1);
      expect(t.counts("alice/review")).toEqual({ up: 1, down: 1 });
      res = await t.vote("alice/review", "bob", 0);
      expect(await res.json()).toMatchObject({ value: 0, upCount: 1, downCount: 0 });
      const mine = await (await t.call("/v1/packages/alice/review/vote", { as: "carol" })).json();
      expect(mine).toMatchObject({ value: 1, canVote: true, own: false });
      const list = (await (await t.call("/v1/packages?sort=recommended")).json()) as { packages: { slug: string; upCount: number; starCount: number; approvalRate: number | null }[] };
      expect(list.packages.find((p) => p.slug === "alice/review")).toMatchObject({ upCount: 1, starCount: 1, approvalRate: 1 });
    } finally {
      t.close();
    }
  });

  it("refuses the publisher, an unverified account, a signed-out caller and an unlisted package", async () => {
    stubAccounts();
    const t = setup();
    try {
      const own = await t.vote("alice/review", "alice", 1);
      expect(own.status).toBe(403);
      expect(((await own.json()) as { error: { code: string } }).error.code).toBe("own_package");
      const legacyOwn = await t.call("/v1/packages/alice/review/star", { method: "POST", as: "alice" });
      expect(legacyOwn.status).toBe(403);
      const unverified = await t.vote("alice/review", "dave", 1);
      expect(((await unverified.json()) as { error: { code: string } }).error.code).toBe("email_unverified");
      expect((await t.call("/v1/packages/alice/review/vote", { method: "POST", body: { value: 1 } })).status).toBe(401);
      expect((await t.vote("alice/draft", "bob", 1)).status).toBe(404);
      expect((await t.vote("alice/review", "bob", 2)).status).toBe(422);
      const mine = await (await t.call("/v1/packages/alice/review/vote", { as: "alice" })).json();
      expect(mine).toMatchObject({ value: 0, canVote: false, own: true });
      expect(t.counts("alice/review")).toEqual({ up: 0, down: 0 });
    } finally {
      t.close();
    }
  });

  it("keeps the legacy star toggle as a +1 vote", async () => {
    stubAccounts();
    const t = setup();
    try {
      let res = await t.call("/v1/packages/alice/review/star", { method: "POST", as: "bob" });
      expect(await res.json()).toEqual({ starred: true, count: 1 });
      res = await t.call("/v1/packages/alice/review/star", { method: "POST", as: "bob" });
      expect(await res.json()).toEqual({ starred: false, count: 0 });
      await t.vote("alice/review", "bob", -1);
      res = await t.call("/v1/packages/alice/review/star", { method: "POST", as: "bob" });
      expect(await res.json()).toEqual({ starred: true, count: 1 });
      expect(t.counts("alice/review")).toEqual({ up: 1, down: 0 });
    } finally {
      t.close();
    }
  });

  it("keeps the cached counts equal to the votes table under concurrent writes", async () => {
    stubAccounts();
    const t = setup();
    try {
      const writes = [];
      for (let i = 0; i < 12; i++) {
        writes.push(t.vote("alice/review", i % 2 ? "bob" : "carol", [1, -1, 0][i % 3]));
        writes.push(t.vote("alice/review", "root", [1, -1][i % 2]));
      }
      const results = await Promise.all(writes);
      expect(results.every((r) => r.status === 200)).toBe(true);
      const cached = t.counts("alice/review");
      const truth = t.recount("alice/review");
      expect(cached).toEqual({ up: truth.up ?? 0, down: truth.down ?? 0 });
    } finally {
      t.close();
    }
  });
});

describe("install pings", () => {
  const ping = (t: ReturnType<typeof setup>, slug: string, installId?: string) =>
    t.call(`/v1/packages/${slug}/installed`, { method: "POST", body: installId === undefined ? {} : { installId } });
  const key = (n: number) => n.toString(16).padStart(32, "0");

  it("counts one install per anonymous id, package and day", async () => {
    const t = setup();
    try {
      expect(await (await ping(t, "alice/review", key(1))).json()).toEqual({ ok: true, counted: true, installCount: 1 });
      expect(await (await ping(t, "alice/review", key(1))).json()).toEqual({ ok: true, counted: false, installCount: 1 });
      expect(await (await ping(t, "alice/review", key(2))).json()).toMatchObject({ counted: true, installCount: 2 });
      expect(await (await ping(t, "bob/lint", key(1))).json()).toMatchObject({ counted: true, installCount: 1 });
      expect(await (await ping(t, "alice/review")).json()).toMatchObject({ counted: false, installCount: 2 });
      expect(await (await ping(t, "alice/review", "not-hex")).json()).toMatchObject({ counted: false });
      expect((await ping(t, "alice/draft", key(3))).status).toBe(404);
      const daily = t.sqlite.prepare("SELECT SUM(count) AS n FROM package_install_daily").get() as { n: number };
      expect(daily.n).toBe(3);
      const stored = t.sqlite.prepare("SELECT install_key FROM package_install_seen ORDER BY install_key").all();
      expect(stored.map((r: { install_key: string }) => r.install_key)).toEqual([key(1), key(1), key(2)]);
    } finally {
      t.close();
    }
  });

  it("counts a burst of identical concurrent pings once", async () => {
    const t = setup();
    try {
      const results = await Promise.all(Array.from({ length: 10 }, () => ping(t, "alice/review", key(9))));
      const bodies = (await Promise.all(results.map((r) => r.json()))) as { counted: boolean }[];
      expect(bodies.filter((b) => b.counted)).toHaveLength(1);
      const row = t.sqlite.prepare("SELECT install_count FROM packages WHERE slug = 'alice/review'").get() as { install_count: number };
      expect(row.install_count).toBe(1);
      expect((t.sqlite.prepare("SELECT count FROM package_install_daily").get() as { count: number }).count).toBe(1);
    } finally {
      t.close();
    }
  });

  it("counts the same id again on a later day", async () => {
    const t = setup();
    const { InstallRepo } = await import("../db/installs");
    try {
      const repo = new InstallRepo(t.db);
      expect((await repo.record("alice/review", key(5), "2026-09-26T23:59:00.000Z"))?.counted).toBe(true);
      expect((await repo.record("alice/review", key(5), "2026-09-27T00:01:00.000Z"))?.counted).toBe(true);
      await repo.purgeBefore("2026-09-27");
      expect((t.sqlite.prepare("SELECT COUNT(*) AS n FROM package_install_seen").get() as { n: number }).n).toBe(1);
    } finally {
      t.close();
    }
  });
});

describe("recommended order", () => {
  it("materializes the published formula and sorts by it", async () => {
    stubAccounts();
    const t = setup();
    try {
      const today = new Date().toISOString().slice(0, 10);
      await t.vote("bob/lint", "carol", 1);
      await t.vote("bob/lint", "alice", 1);
      await t.call("/v1/packages/alice/review/installed", { method: "POST", body: { installId: "a".repeat(32) } });
      const lint = t.sqlite.prepare("SELECT rec_score FROM packages WHERE slug = 'bob/lint'").get() as { rec_score: number };
      expect(lint.rec_score).toBe(recommendScore(2, 0, [], today));
      const review = t.sqlite.prepare("SELECT rec_score FROM packages WHERE slug = 'alice/review'").get() as { rec_score: number };
      expect(review.rec_score).toBe(recommendScore(0, 0, [{ date: today, count: 1 }], today));
      const list = (await (await t.call("/v1/packages")).json()) as { packages: { slug: string; score: number }[] };
      expect(list.packages.map((p) => p.slug)).toEqual(["bob/lint", "alice/review"]);
      expect(list.packages[0].score).toBe(lint.rec_score);
      // Decay moves a score with no write at all; the cron recompute catches it.
      const later = new Date(Date.now() + 20 * 86_400_000).toISOString().slice(0, 10);
      expect(await rescore(t.db, later)).toBeGreaterThan(0);
      const decayed = t.sqlite.prepare("SELECT rec_score FROM packages WHERE slug = 'alice/review'").get() as { rec_score: number };
      expect(decayed.rec_score).toBe(recommendScore(0, 0, [{ date: today, count: 1 }], later));
      expect(await rescore(t.db, later)).toBe(0);
    } finally {
      t.close();
    }
  });

  it("flags heavily down-voted packages for review without hiding them", async () => {
    stubAccounts();
    const t = setup();
    try {
      const id = (t.sqlite.prepare("SELECT id FROM packages WHERE slug = 'bob/lint'").get() as { id: number }).id;
      const cast = t.sqlite.prepare("INSERT INTO votes (package_id, user_id, value, created_at, updated_at) VALUES (?1, ?2, ?3, 'x', 'x')");
      for (let u = 100; u < 110; u++) cast.run(id, u, -1);
      for (let u = 200; u < 204; u++) cast.run(id, u, 1);
      await t.vote("bob/lint", "carol", -1);
      const flagged = (await (await t.call("/v1/admin/packages?status=flagged", { as: "root" })).json()) as { packages: { slug: string; status: string }[] };
      expect(flagged.packages.map((p) => [p.slug, p.status])).toEqual([["bob/lint", "active"]]);
      expect((await (await t.call("/v1/packages")).json()) as { packages: unknown[] }).toMatchObject({ packages: expect.arrayContaining([expect.objectContaining({ slug: "bob/lint" })]) });
      expect((await t.call("/v1/admin/packages?status=flagged", { as: "bob" })).status).toBe(403);
    } finally {
      t.close();
    }
  });
});

describe("activity feed", () => {
  it("announces an account's first up-vote once, however it toggles", async () => {
    stubAccounts();
    const t = setup();
    try {
      await t.vote("alice/review", "bob", 1);
      await t.vote("alice/review", "bob", 0);
      await t.vote("alice/review", "bob", 1);
      await t.vote("alice/review", "carol", -1);
      await t.vote("alice/review", "carol", 1);
      await t.vote("alice/review", "carol", -1);
      await t.vote("alice/review", "carol", 1);
      const events = t.sqlite.prepare("SELECT actor_handle FROM events WHERE type = 'star'").all() as { actor_handle: string }[];
      expect(events.map((e) => e.actor_handle)).toEqual(["bob", "carol"]);
    } finally {
      t.close();
    }
  });
});
