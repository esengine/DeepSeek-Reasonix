import { afterEach, describe, expect, it, vi } from "vitest";
import registryApp from "../app";
import type { Bindings } from "../env";
import type { PackageRow } from "../types";

const oldRevision: PackageRow = {
  id: 42,
  kind: "plugin",
  scope_handle: "publisher",
  name: "devkit",
  slug: "publisher/devkit",
  summary: "Developer tools",
  description: "",
  source: "https://github.com/o/r",
  install_kind: "plugin",
  homepage: "",
  repo_url: "https://github.com/o/r",
  tags: "tool",
  latest_version: "2.7.1",
  install_count: 0,
  star_count: 0,
  up_count: 0,
  down_count: 0,
  rec_score: 0,
  verified: 0,
  status: "pending",
  publisher_id: 7,
  created_at: "2026-07-22T00:00:00.000Z",
  updated_at: "2026-07-22T00:30:00.000Z",
};

function approvalDB(current: PackageRow, approved: PackageRow | null) {
  const statements: { sql: string; values: unknown[] }[] = [];
  const db = {
    prepare(sql: string) {
      let values: unknown[] = [];
      const statement = {
        bind(...bound: unknown[]) {
          values = bound;
          return statement;
        },
        async first<T>() {
          statements.push({ sql, values });
          if (sql.startsWith("UPDATE packages SET status")) return approved as T | null;
          if (sql.startsWith("SELECT * FROM packages")) return current as T;
          return null;
        },
        async run() {
          statements.push({ sql, values });
          return { meta: { changes: 1 } };
        },
      };
      return statement;
    },
  };
  return { db: db as unknown as D1Database, statements };
}

function bindings(db: D1Database): Bindings {
  return {
    DB: db,
    ACCOUNTS_ORIGIN: "https://id.reasonix.test",
    APP_ORIGIN: "https://reasonix.test",
    ALLOWED_ORIGINS: "https://reasonix.test",
  };
}

function approvalRequest(body: object): Request {
  return new Request("https://registry.reasonix.test/v1/admin/packages/publisher/devkit/approve", {
    method: "POST",
    headers: { cookie: "rxid=test", "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}

afterEach(() => vi.unstubAllGlobals());

describe("admin package approval", () => {
  it("fails closed when an older review page omits the revision", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({ user: { id: 1, handle: "admin", role: "admin", emailVerified: true } }),
      ),
    );
    const { db, statements } = approvalDB(oldRevision, null);

    const response = await registryApp.fetch(approvalRequest({}), bindings(db));

    expect(response.status).toBe(400);
    await expect(response.json()).resolves.toEqual({
      error: {
        code: "invalid_review_revision",
        message: "Approval requires the reviewed package revision.",
      },
    });
    expect(statements).toHaveLength(0);
  });

  it("rejects a stale review after the publisher submits a newer version", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({ user: { id: 1, handle: "admin", role: "admin", emailVerified: true } }),
      ),
    );
    const current = { ...oldRevision, latest_version: "2.7.2", updated_at: "2026-07-22T00:45:00.000Z" };
    const { db, statements } = approvalDB(current, null);

    const response = await registryApp.fetch(
      approvalRequest({
        expectedVersion: oldRevision.latest_version,
        expectedUpdatedAt: oldRevision.updated_at,
        expectedStatus: oldRevision.status,
      }),
      bindings(db),
    );

    expect(response.status).toBe(409);
    await expect(response.json()).resolves.toEqual({
      error: {
        code: "stale_review",
        message: "Package changed since it was reviewed. Refresh and review the latest version.",
      },
    });
    expect(statements.some(({ sql }) => sql.startsWith("INSERT INTO events"))).toBe(false);
  });

  it("publishes when the submitted review revision still matches", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({ user: { id: 1, handle: "admin", role: "admin", emailVerified: true } }),
      ),
    );
    const approved = { ...oldRevision, status: "active", updated_at: "2026-07-22T01:00:00.000Z" };
    const { db, statements } = approvalDB(oldRevision, approved);

    const response = await registryApp.fetch(
      approvalRequest({
        expectedVersion: oldRevision.latest_version,
        expectedUpdatedAt: oldRevision.updated_at,
        expectedStatus: oldRevision.status,
      }),
      bindings(db),
    );

    expect(response.status).toBe(200);
    const body = (await response.json()) as { package: { status: string; latestVersion: string } };
    expect(body.package).toMatchObject({ status: "active", latestVersion: "2.7.1" });
    expect(statements.some(({ sql }) => sql.startsWith("INSERT INTO events"))).toBe(true);
  });

  it("refuses a reviewed digest that is not the installer's spelling", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({ user: { id: 1, handle: "admin", role: "admin", emailVerified: true } }),
      ),
    );
    const { db, statements } = approvalDB(oldRevision, null);
    const response = await registryApp.fetch(
      approvalRequest({
        expectedVersion: oldRevision.latest_version,
        expectedUpdatedAt: oldRevision.updated_at,
        expectedStatus: oldRevision.status,
        contentHash: "trust me",
      }),
      bindings(db),
    );
    expect(response.status).toBe(400);
    expect(statements).toHaveLength(0);
  });

  it("binds the reviewer's digest to the approved version", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({ user: { id: 1, handle: "admin", role: "admin", emailVerified: true } }),
      ),
    );
    const approved = { ...oldRevision, status: "active", updated_at: "2026-07-22T01:00:00.000Z" };
    const { db, statements } = approvalDB(oldRevision, approved);
    const digest = "sha256:" + "0f".repeat(32);
    const response = await registryApp.fetch(
      approvalRequest({
        expectedVersion: oldRevision.latest_version,
        expectedUpdatedAt: oldRevision.updated_at,
        expectedStatus: oldRevision.status,
        contentHash: digest,
      }),
      bindings(db),
    );
    expect(response.status).toBe(200);
    const pin = statements.find(({ sql }) => sql.includes("UPDATE package_versions SET content_hash"));
    expect(pin?.values[0]).toBe(digest);
  });
});

describe("admin digest pin", () => {
  const live: PackageRow = { ...oldRevision, status: "active" };
  const digest = "sha256:" + "0f".repeat(32);

  function pinDB(currentHash: string | null, changes = 1) {
    const statements: { sql: string; values: unknown[] }[] = [];
    const db = {
      prepare(sql: string) {
        let values: unknown[] = [];
        const statement = {
          bind(...bound: unknown[]) {
            values = bound;
            return statement;
          },
          async first<T>() {
            statements.push({ sql, values });
            if (sql.includes("SELECT v.content_hash")) return (currentHash === null ? null : { content_hash: currentHash }) as T;
            if (sql.startsWith("SELECT * FROM packages")) return live as T;
            return null;
          },
          async run() {
            statements.push({ sql, values });
            return { meta: { changes } };
          },
        };
        return statement;
      },
    };
    return { db: db as unknown as D1Database, statements };
  }

  function pinRequest(body: object): Request {
    return new Request("https://registry.reasonix.test/v1/admin/packages/publisher/devkit/pin", {
      method: "POST",
      headers: { cookie: "rxid=test", "content-type": "application/json" },
      body: JSON.stringify(body),
    });
  }

  const liveRevision = {
    expectedVersion: live.latest_version,
    expectedUpdatedAt: live.updated_at,
    expectedStatus: "active",
  };

  function asAdmin() {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ user: { id: 1, handle: "admin", role: "admin", emailVerified: true } })),
    );
  }

  it("is for admins only", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ user: { id: 2, handle: "member", role: "member", emailVerified: true } })),
    );
    const { db, statements } = pinDB("");
    const response = await registryApp.fetch(pinRequest({ ...liveRevision, contentHash: digest }), bindings(db));
    expect(response.status).toBe(403);
    expect(statements).toHaveLength(0);
  });

  it("accepts only the installer's digest spelling on a live revision", async () => {
    asAdmin();
    for (const body of [
      { ...liveRevision, contentHash: "" },
      { ...liveRevision, contentHash: "sha256:" + "0F".repeat(32) },
      { ...liveRevision, contentHash: "trust me" },
      { ...liveRevision, expectedStatus: "pending", contentHash: digest },
      { expectedStatus: "active", contentHash: digest },
    ]) {
      const { db, statements } = pinDB("");
      const response = await registryApp.fetch(pinRequest(body), bindings(db));
      expect(response.status, JSON.stringify(body)).toBe(400);
      await expect(response.json()).resolves.toMatchObject({ error: { code: "invalid_pin" } });
      expect(statements).toHaveLength(0);
    }
  });

  it("binds the digest to the live version and logs the change", async () => {
    asAdmin();
    const { db, statements } = pinDB("");
    const response = await registryApp.fetch(pinRequest({ ...liveRevision, contentHash: digest }), bindings(db));

    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toMatchObject({
      package: { status: "active", latestVersion: "2.7.1" },
      previousContentHash: "",
      contentHash: digest,
    });
    const pin = statements.find(({ sql }) => sql.includes("UPDATE package_versions SET content_hash"));
    expect(pin?.sql).toContain("status = 'active'");
    expect(pin?.values).toEqual([digest, live.latest_version, "", live.slug, live.updated_at]);
    expect(statements.some(({ sql }) => sql.startsWith("UPDATE packages"))).toBe(false);
    const event = statements.find(({ sql }) => sql.startsWith("INSERT INTO events"));
    expect(event?.values.slice(0, 4)).toEqual(["pin", live.id, "admin", `2.7.1 unpinned -> ${digest}`]);
  });

  it("refuses a stale revision without logging", async () => {
    asAdmin();
    const { db, statements } = pinDB(null);
    const response = await registryApp.fetch(pinRequest({ ...liveRevision, contentHash: digest }), bindings(db));

    expect(response.status).toBe(409);
    await expect(response.json()).resolves.toMatchObject({ error: { code: "stale_review" } });
    expect(statements.some(({ sql }) => sql.includes("UPDATE package_versions"))).toBe(false);
    expect(statements.some(({ sql }) => sql.startsWith("INSERT INTO events"))).toBe(false);
  });

  it("refuses when the digest moved between read and write", async () => {
    asAdmin();
    const { db, statements } = pinDB("", 0);
    const response = await registryApp.fetch(pinRequest({ ...liveRevision, contentHash: digest }), bindings(db));

    expect(response.status).toBe(409);
    expect(statements.some(({ sql }) => sql.startsWith("INSERT INTO events"))).toBe(false);
  });
});
