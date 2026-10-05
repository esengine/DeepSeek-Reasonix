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
});
