import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
// @ts-expect-error Node types are intentionally not part of the Worker build.
import { DatabaseSync } from "node:sqlite";
import registryApp from "../app";
import type { Bindings } from "../env";
import registrySchema from "../../../registry-schema.sql?raw";
import registryIndexes from "../../../registry-migrate-performance-indexes.sql?raw";

const accounts: Record<string, { id: number; handle: string; role: string; emailVerified: boolean }> = {
  "Bearer alice": { id: 7, handle: "alice", role: "member", emailVerified: true },
  "Bearer bob": { id: 8, handle: "bob", role: "member", emailVerified: true },
  "Bearer root": { id: 1, handle: "root", role: "admin", emailVerified: true },
};

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

async function call(path: string, init: { method?: string; as?: string; body?: unknown } = {}) {
  const headers: Record<string, string> = {};
  if (init.as) headers.authorization = `Bearer ${init.as}`;
  if (init.body !== undefined) headers["content-type"] = "application/json";
  const res = await registryApp.fetch(
    new Request(`https://crash.reasonix.test${path}`, {
      method: init.method ?? "GET",
      headers,
      body: init.body === undefined ? undefined : JSON.stringify(init.body),
    }),
    env,
  );
  return { status: res.status, body: (await res.json()) as any };
}

function publish(as: string, name: string, extra: Record<string, unknown> = {}) {
  return call("/v1/packages", {
    method: "POST",
    as,
    body: { kind: "plugin", name, source: "https://github.com/o/r", summary: `secret ${name}`, tags: ["hidden-tag"], ...extra },
  });
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

describe("private packages", () => {
  it("land outside the review queue and never reach a public surface", async () => {
    const created = await publish("alice", "vault", { visibility: "private" });
    expect(created.status).toBe(201);
    expect(created.body.package.status).toBe("private");
    await publish("root", "admin-vault", { visibility: "private" });

    const list = await call("/v1/packages?kind=all");
    expect(list.body.packages).toEqual([]);
    const search = await call("/v1/packages?q=secret");
    expect(search.body.packages).toEqual([]);
    const tagged = await call("/v1/packages?q=hidden-tag&sort=trending");
    expect(tagged.body.packages).toEqual([]);
    const missing = await call("/v1/packages/alice/nope");
    const detail = await call("/v1/packages/alice/vault");
    expect(detail).toEqual(missing);
    expect((await call("/v1/packages/root/admin-vault")).status).toBe(404);
    expect((await call("/v1/packages/alice/vault/installed", { method: "POST" })).status).toBe(404);
    expect((await call("/v1/packages/alice/vault/star", { method: "POST", as: "bob" })).status).toBe(404);
    expect((await call("/v1/activity")).body.events).toEqual([]);

    const queue = await call("/v1/admin/packages", { as: "root" });
    expect(queue.body.packages).toEqual([]);
    const privateQueue = await call("/v1/admin/packages?status=private", { as: "root" });
    expect(privateQueue.body.packages.map((p: { slug: string }) => p.slug).sort()).toEqual([
      "alice/vault",
      "root/admin-vault",
    ]);
  });

  it("let the owner, and only the owner, read one in full", async () => {
    await publish("alice", "vault", { visibility: "private", version: "1.2.0" });

    const own = await call("/v1/me/packages/alice/vault", { as: "alice" });
    expect(own.status).toBe(200);
    expect(own.body.package).toMatchObject({
      slug: "alice/vault",
      status: "private",
      kind: "plugin",
      installKind: "plugin",
      source: "https://github.com/o/r",
    });
    expect(own.body.versions.map((v: { version: string }) => v.version)).toEqual(["1.2.0"]);

    const other = await call("/v1/me/packages/alice/vault", { as: "bob" });
    const absent = await call("/v1/me/packages/alice/absent", { as: "bob" });
    expect(other).toEqual(absent);
    expect(other.status).toBe(404);
    expect((await call("/v1/me/packages/alice/vault", { as: "root" })).status).toBe(404);
    expect((await call("/v1/me/packages/alice/vault")).status).toBe(401);

    const mine = await call("/v1/me/packages", { as: "alice" });
    expect(mine.body.packages.map((p: { status: string }) => p.status)).toEqual(["private"]);
  });

  it("reads the owner's pending and rejected packages too", async () => {
    await publish("alice", "draft");
    const pending = await call("/v1/me/packages/alice/draft", { as: "alice" });
    expect(pending.body.package.status).toBe("pending");
    await call("/v1/admin/packages/alice/draft/reject", { method: "POST", as: "root" });
    const rejected = await call("/v1/me/packages/alice/draft", { as: "alice" });
    expect(rejected.body.package.status).toBe("rejected");
  });

  it("enter review only when the owner submits them", async () => {
    await publish("alice", "vault", { visibility: "private" });

    const stranger = await call("/v1/me/packages/alice/vault/submit", { method: "POST", as: "bob" });
    const nothing = await call("/v1/me/packages/alice/absent/submit", { method: "POST", as: "bob" });
    expect(stranger).toEqual(nothing);
    expect(stranger.status).toBe(404);

    const submitted = await call("/v1/me/packages/alice/vault/submit", { method: "POST", as: "alice" });
    expect(submitted.status).toBe(200);
    expect(submitted.body.package.status).toBe("pending");
    const again = await call("/v1/me/packages/alice/vault/submit", { method: "POST", as: "alice" });
    expect(again.status).toBe(409);
    expect(again.body.error.code).toBe("not_private");

    const queue = await call("/v1/admin/packages", { as: "root" });
    expect(queue.body.packages.map((p: { slug: string }) => p.slug)).toEqual(["alice/vault"]);
    expect((await call("/v1/packages/alice/vault")).status).toBe(404);
  });

  it("cannot be approved straight out of private", async () => {
    const created = await publish("alice", "vault", { visibility: "private" });
    const approve = await call("/v1/admin/packages/alice/vault/approve", {
      method: "POST",
      as: "root",
      body: {
        expectedVersion: created.body.version,
        expectedUpdatedAt: created.body.package.updatedAt,
        expectedStatus: "private",
      },
    });
    expect(approve.status).toBe(400);
    expect((await call("/v1/packages/alice/vault")).status).toBe(404);
  });

  it("cannot be moved by a moderator into an approvable state", async () => {
    const created = await publish("alice", "vault", { visibility: "private" });
    for (const act of ["hide", "reject", "verify"]) {
      expect((await call(`/v1/admin/packages/alice/vault/${act}`, { method: "POST", as: "root" })).status).toBe(404);
    }
    for (const expectedStatus of ["hidden", "rejected", "pending"]) {
      const approve = await call("/v1/admin/packages/alice/vault/approve", {
        method: "POST",
        as: "root",
        body: { expectedVersion: created.body.version, expectedUpdatedAt: created.body.package.updatedAt, expectedStatus },
      });
      expect(approve.status).toBe(409);
    }
    const own = await call("/v1/me/packages/alice/vault", { as: "alice" });
    expect(own.body.package).toMatchObject({ status: "private", verified: false });
    expect((await call("/v1/packages/alice/vault")).status).toBe(404);
  });

  it("follow the visibility of each publish", async () => {
    await publish("alice", "vault", { visibility: "private" });
    const publicUpdate = await publish("alice", "vault", { version: "0.2.0" });
    expect(publicUpdate.body.package.status).toBe("pending");
    const privateAgain = await publish("alice", "vault", { visibility: "private", version: "0.3.0" });
    expect(privateAgain.body.package.status).toBe("private");

    await publish("root", "tool");
    expect((await call("/v1/packages/root/tool")).status).toBe(200);
    const adminPrivate = await publish("root", "tool", { visibility: "private", version: "0.2.0" });
    expect(adminPrivate.body.package.status).toBe("private");
    const adminPublic = await publish("root", "tool", { version: "0.3.0" });
    expect(adminPublic.body.package.status).toBe("pending");
  });

  it("drop out of the feed once a live package turns private", async () => {
    await publish("root", "tool");
    expect((await call("/v1/activity")).body.events).toHaveLength(1);
    await publish("root", "tool", { visibility: "private", version: "0.2.0" });
    expect((await call("/v1/activity")).body.events).toEqual([]);
  });

  it("rejects an unknown visibility", async () => {
    const res = await publish("alice", "vault", { visibility: "friends" });
    expect(res.status).toBe(422);
  });
});

// The feed is public and cacheable, so it names only packages the public can
// open: not one taken back down, back in review after an update, or rejected.
describe("the activity feed", () => {
  const slugs = async () => (await call("/v1/activity")).body.events.map((e: { slug: string }) => e.slug);
  const approve = (slug: string, published: { body: any }) =>
    call(`/v1/admin/packages/${slug}/approve`, {
      method: "POST",
      as: "root",
      body: { expectedVersion: published.body.version, expectedUpdatedAt: published.body.package.updatedAt, expectedStatus: "pending" },
    });
  const goLive = async (as: string, name: string) => {
    expect((await approve(`${as}/${name}`, await publish(as, name))).status).toBe(200);
  };

  it("names only packages the public can open", async () => {
    await publish("root", "tool");
    await publish("root", "gone");
    expect((await call("/v1/admin/packages/root/gone/hide", { method: "POST", as: "root" })).status).toBe(200);
    await goLive("alice", "queued");
    expect((await publish("alice", "queued", { version: "0.2.0" })).body.package.status).toBe("pending");
    await goLive("bob", "refused");
    await publish("bob", "refused", { version: "0.2.0" });
    expect((await call("/v1/admin/packages/bob/refused/reject", { method: "POST", as: "root" })).status).toBe(200);

    expect(await slugs()).toEqual(["root/tool"]);
  });
});
