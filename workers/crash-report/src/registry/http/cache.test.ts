import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
// @ts-expect-error Node types are intentionally not part of the Worker build.
import { DatabaseSync } from "node:sqlite";
import registryApp from "../app";
import type { Bindings } from "../env";
import registrySchema from "../../../registry-schema.sql?raw";
import registryIndexes from "../../../registry-migrate-performance-indexes.sql?raw";

const accounts: Record<string, { id: number; handle: string; role: string; emailVerified: boolean }> = {
  "Bearer alice": { id: 7, handle: "alice", role: "member", emailVerified: true },
  "Bearer root": { id: 1, handle: "root", role: "admin", emailVerified: true },
};
const PUBLIC = "public, max-age=60, stale-while-revalidate=300";
const DETAIL = "public, max-age=60, stale-while-revalidate=30";

let sqlite: any;
let env: Bindings;

function d1(): D1Database {
  const wrap = (sql: string) => {
    const statement = sqlite.prepare(sql);
    const wrapper: any = {
      values: [] as unknown[],
      bind(...values: unknown[]) {
        wrapper.values = values;
        return wrapper;
      },
      async first() {
        return statement.get(...wrapper.values) ?? null;
      },
      async all() {
        return { results: statement.all(...wrapper.values) };
      },
      async run() {
        return { meta: { changes: Number(statement.run(...wrapper.values).changes) } };
      },
    };
    return wrapper;
  };
  return {
    prepare: wrap,
    async batch(statements: Array<{ all: () => Promise<{ results: unknown[] }> }>) {
      const out = [];
      for (const s of statements) out.push(await s.all());
      return out;
    },
  } as unknown as D1Database;
}

function fetchAs(path: string, init: { method?: string; headers?: Record<string, string>; body?: unknown } = {}) {
  return registryApp.fetch(
    new Request(`https://crash.reasonix.test${path}`, {
      method: init.method ?? "GET",
      headers: { ...(init.body === undefined ? {} : { "content-type": "application/json" }), ...init.headers },
      body: init.body === undefined ? undefined : JSON.stringify(init.body),
    }),
    env,
  );
}

async function publish(as: string, name: string, extra: Record<string, unknown> = {}) {
  const res = await fetchAs("/v1/packages", {
    method: "POST",
    headers: { authorization: `Bearer ${as}` },
    body: { kind: "plugin", name, source: "https://github.com/o/r", summary: `summary ${name}`, ...extra },
  });
  return (await res.json()) as any;
}

async function approve(slug: string, pkg: any) {
  return fetchAs(`/v1/admin/packages/${slug}/approve`, {
    method: "POST",
    headers: { authorization: "Bearer root" },
    body: { expectedVersion: pkg.latestVersion, expectedUpdatedAt: pkg.updatedAt, expectedStatus: "pending" },
  });
}

class FakeEdge {
  store = new Map<string, Response>();
  matches = 0;
  async match(req: Request) {
    const hit = this.store.get(req.url);
    if (hit) this.matches++;
    return hit?.clone();
  }
  async put(req: Request, res: Response) {
    this.store.set(req.url, res);
  }
}

beforeEach(() => {
  sqlite = new DatabaseSync(":memory:");
  sqlite.exec(registrySchema);
  sqlite.exec(registryIndexes);
  env = {
    DB: d1(),
    ACCOUNTS_ORIGIN: "https://id.reasonix.test",
    APP_ORIGIN: "https://reasonix.test",
    ALLOWED_ORIGINS: "https://reasonix.test",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const user = accounts[(init?.headers as Record<string, string>)?.authorization ?? ""];
      return user ? Response.json({ user }) : new Response("no", { status: 401 });
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  sqlite.close();
});

describe("public read caching", () => {
  it("marks list, search, detail and activity public with a 60 second window and an ETag", async () => {
    const { package: pkg } = await publish("root", "devkit");
    expect(pkg.status).toBe("active");
    for (const path of ["/v1/packages?kind=all", "/v1/packages?q=devkit", "/v1/packages/root/devkit", "/v1/activity"]) {
      const res = await fetchAs(path);
      expect(res.status, path).toBe(200);
      expect(res.headers.get("cache-control"), path).toBe(path.includes("root/devkit") ? DETAIL : PUBLIC);
      expect(res.headers.get("etag"), path).toMatch(/^W\/"[0-9a-f]{32}"$/);
    }
  });

  it("answers a matching If-None-Match with an empty 304 that keeps the validators", async () => {
    await publish("root", "devkit");
    const first = await fetchAs("/v1/packages");
    const etag = first.headers.get("etag")!;
    const again = await fetchAs("/v1/packages", { headers: { "if-none-match": etag } });
    expect(again.status).toBe(304);
    expect(await again.text()).toBe("");
    expect(again.headers.get("etag")).toBe(etag);
    expect(again.headers.get("cache-control")).toBe(PUBLIC);
    const list = await fetchAs("/v1/packages", { headers: { "if-none-match": `"other", ${etag}` } });
    expect(list.status).toBe(304);
    const star = await fetchAs("/v1/packages", { headers: { "if-none-match": "*" } });
    expect(star.status).toBe(304);
    const stale = await fetchAs("/v1/packages", { headers: { "if-none-match": 'W/"stale"' } });
    expect(stale.status).toBe(200);
  });

  it("keeps the response body byte-identical to the uncached payload", async () => {
    await publish("root", "devkit");
    const res = await fetchAs("/v1/packages");
    const body = JSON.parse(await res.text());
    expect(Object.keys(body)).toEqual(["packages", "limit", "offset"]);
    expect(res.headers.get("content-type")).toContain("application/json");
  });

  it("answers HEAD with the cache headers and no body", async () => {
    await publish("root", "devkit");
    const res = await fetchAs("/v1/packages", { method: "HEAD" });
    expect(res.status).toBe(200);
    expect(res.headers.get("cache-control")).toBe(PUBLIC);
    expect(res.headers.get("etag")).toBeTruthy();
    expect(await res.text()).toBe("");
  });

  it("never shares a response requested with credentials", async () => {
    await publish("root", "devkit");
    for (const headers of <Record<string, string>[]>[{ authorization: "Bearer root" }, { authorization: "Bearer alice" }, { cookie: "rxid=abc" }]) {
      for (const path of ["/v1/packages", "/v1/packages/root/devkit", "/v1/activity"]) {
        const res = await fetchAs(path, { headers });
        expect(res.headers.get("cache-control"), `${path} ${JSON.stringify(headers)}`).toBe("private, no-store");
      }
    }
  });

  it("keeps a pending package out of the list and caches nothing for its detail", async () => {
    const { package: pending } = await publish("alice", "wip");
    expect(pending.status).toBe("pending");
    const list = await fetchAs("/v1/packages?kind=all");
    expect((await list.json() as any).packages).toEqual([]);
    const detail = await fetchAs("/v1/packages/alice/wip");
    expect(detail.status).toBe(404);
    expect(detail.headers.get("cache-control")).toBe("no-store");
    expect(detail.headers.get("etag")).toBeNull();
  });

  it("changes the list ETag once a package is approved", async () => {
    const { package: pending } = await publish("alice", "wip");
    const before = (await fetchAs("/v1/packages?kind=all")).headers.get("etag");
    expect((await approve("alice/wip", pending)).status).toBe(200);
    const after = await fetchAs("/v1/packages?kind=all", { headers: { "if-none-match": before! } });
    expect(after.status).toBe(200);
    expect(after.headers.get("etag")).not.toBe(before);
    expect(((await after.json()) as any).packages.map((p: any) => p.slug)).toEqual(["alice/wip"]);
  });

  it("marks error responses no-store", async () => {
    const invalid = await fetchAs("/v1/packages?limit=-5");
    expect(invalid.status).toBe(422);
    expect(invalid.headers.get("cache-control")).toBe("no-store");
    expect(invalid.headers.get("etag")).toBeNull();
    const missing = await fetchAs("/v1/packages/nobody/none");
    expect(missing.headers.get("cache-control")).toBe("no-store");
    const unknown = await fetchAs("/v1/packages/a/b/c/d");
    expect(unknown.headers.get("cache-control")).toBe("no-store");
    const broken = { ...env, DB: { prepare: () => { throw new Error("d1 down"); } } } as unknown as Bindings;
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const res = await registryApp.fetch(new Request("https://crash.reasonix.test/v1/packages"), broken);
    expect(res.status).toBe(500);
    expect(res.headers.get("cache-control")).toBe("no-store");
  });

  it("marks writes, admin, me and health no-store", async () => {
    const created = await fetchAs("/v1/packages", {
      method: "POST",
      headers: { authorization: "Bearer alice" },
      body: { kind: "plugin", name: "w", source: "https://github.com/o/r", summary: "s" },
    });
    expect(created.headers.get("cache-control")).toBe("no-store");
    const queue = await fetchAs("/v1/admin/packages", { headers: { authorization: "Bearer root" } });
    expect(queue.status).toBe(200);
    expect(queue.headers.get("cache-control")).toBe("no-store");
    const mine = await fetchAs("/v1/me/packages", { headers: { authorization: "Bearer alice" } });
    expect(mine.headers.get("cache-control")).toBe("no-store");
    const anonymousAdmin = await fetchAs("/v1/admin/packages");
    expect(anonymousAdmin.status).toBe(401);
    expect(anonymousAdmin.headers.get("cache-control")).toBe("no-store");
    expect((await fetchAs("/health")).headers.get("cache-control")).toBe("no-store");
  });

  it("keeps CORS per origin on a response served from the edge cache", async () => {
    const edge = new FakeEdge();
    vi.stubGlobal("caches", { default: edge });
    await publish("root", "devkit");
    const cold = await fetchAs("/v1/packages", { headers: { origin: "https://reasonix.test" } });
    expect(cold.headers.get("access-control-allow-origin")).toBe("https://reasonix.test");
    expect(edge.store.size).toBe(1);
    const warm = await fetchAs("/v1/packages", { headers: { origin: "https://evil.test" } });
    expect(edge.matches).toBe(1);
    expect(warm.headers.get("access-control-allow-origin")).toBeNull();
    expect(warm.headers.get("cache-control")).toBe(PUBLIC);
    const stored = [...edge.store.values()][0];
    expect(stored.headers.get("access-control-allow-origin")).toBeNull();
  });

  it("serves a warm edge entry without touching the database and still honours If-None-Match", async () => {
    const edge = new FakeEdge();
    vi.stubGlobal("caches", { default: edge });
    await publish("root", "devkit");
    const cold = await fetchAs("/v1/packages");
    const etag = cold.headers.get("etag")!;
    const prepare = vi.fn(() => { throw new Error("db must not be hit"); });
    env = { ...env, DB: { prepare } as unknown as D1Database };
    const warm = await fetchAs("/v1/packages", { headers: { "if-none-match": etag } });
    expect(warm.status).toBe(304);
    expect(prepare).not.toHaveBeenCalled();
    expect((await fetchAs("/v1/packages")).status).toBe(200);
  });

  it("keys edge entries by validated parameters only", async () => {
    const edge = new FakeEdge();
    vi.stubGlobal("caches", { default: edge });
    await publish("root", "devkit");
    await fetchAs("/v1/packages?kind=all&q=DevKit&limit=10");
    expect(edge.store.size).toBe(1);
    for (const noise of ["x=1", "x=2&utm=abc", "limit=10&q=devkit&kind=all", "q=DEVKIT&kind=all&limit=10&junk=z"]) {
      await fetchAs(`/v1/packages?${noise.includes("kind") ? "" : "kind=all&q=devkit&limit=10&"}${noise}`);
    }
    expect(edge.store.size).toBe(1);
    expect(edge.matches).toBe(4);
    await fetchAs("/v1/packages?kind=all&q=devkit&limit=11");
    expect(edge.store.size).toBe(2);
  });

  it("treats defaults and spelled-out defaults as one entry and never keys an invalid query", async () => {
    const edge = new FakeEdge();
    vi.stubGlobal("caches", { default: edge });
    await publish("root", "devkit");
    await fetchAs("/v1/packages");
    await fetchAs("/v1/packages?limit=24&offset=0&sort=recommended&kind=all");
    expect(edge.store.size).toBe(1);
    await fetchAs("/v1/packages?limit=-5&x=1");
    await fetchAs("/v1/packages?kind=bogus");
    expect(edge.store.size).toBe(1);
    await fetchAs("/v1/packages/root/devkit?x=1");
    await fetchAs("/v1/packages/root/devkit?x=2");
    await fetchAs("/v1/activity?r=1");
    await fetchAs("/v1/activity?r=2");
    expect(edge.store.size).toBe(3);
  });

  it("keys detail by decoded route parameters and never stores a missing spelling", async () => {
    const edge = new FakeEdge();
    vi.stubGlobal("caches", { default: edge });
    await publish("root", "devkit");
    await fetchAs("/v1/packages/root/devkit");
    for (const path of ["/v1/packages/%72oot/devkit", "/v1/packages/root/%64evkit", "/v1/packages/%72oot/%64%65vkit"]) {
      const res = await fetchAs(path);
      expect(res.status, path).toBe(200);
    }
    expect(edge.store.size).toBe(1);
    expect(edge.matches).toBe(3);
    for (const path of ["/v1/packages/ROOT/devkit", "/v1/packages/root/DevKit", "/v1/packages/root/devkit/"]) {
      const res = await fetchAs(path);
      expect(res.status, path).toBe(404);
      expect(res.headers.get("cache-control")).toBe("no-store");
    }
    expect(edge.store.size).toBe(1);
  });

  it("does not put credentialed or failed responses into the edge cache", async () => {
    const edge = new FakeEdge();
    vi.stubGlobal("caches", { default: edge });
    await publish("root", "devkit");
    await fetchAs("/v1/packages", { headers: { authorization: "Bearer root" } });
    await fetchAs("/v1/packages?limit=-5");
    await fetchAs("/v1/packages/nobody/none");
    expect(edge.store.size).toBe(0);
  });
});
