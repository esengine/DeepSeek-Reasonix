import { describe, expect, it } from "vitest";
// @ts-expect-error Node types are intentionally not part of the Worker build.
import { DatabaseSync } from "node:sqlite";
import type { PackageRow, RegistryUser } from "../types";
import { PublishSchema } from "../lib/validation";
import { PackageRepo } from "./packages";
import registrySchema from "../../../registry-schema.sql?raw";

const now = "2026-07-22T00:00:00.000Z";
const user: RegistryUser = {
  id: 7,
  handle: "publisher",
  role: "member",
  emailVerified: true,
};

const existing: PackageRow = {
  id: 42,
  kind: "mcp",
  scope_handle: "publisher",
  name: "devkit",
  slug: "publisher/devkit",
  summary: "old",
  description: "",
  source: "https://github.com/o/r",
  install_kind: "auto",
  homepage: "",
  repo_url: "https://github.com/o/r",
  tags: "tool",
  latest_version: "2.7.0",
  status: "pending",
  verified: 0,
  publisher_id: 7,
  install_count: 0,
  star_count: 0,
  created_at: now,
  updated_at: now,
};

function fakePackageDB(reads: PackageRow[]) {
  const updates: { sql: string; values: unknown[] }[] = [];
  let packageReads = 0;
  const db = {
    prepare(sql: string) {
      let values: unknown[] = [];
      const statement = {
        bind(...bound: unknown[]) {
          values = bound;
          return statement;
        },
        async first<T>() {
          if (sql.startsWith("SELECT * FROM packages")) {
            const row = reads[Math.min(packageReads, reads.length - 1)];
            packageReads += 1;
            return row as T;
          }
          return null;
        },
        async run() {
          if (sql.startsWith("UPDATE packages SET")) updates.push({ sql, values });
          return { meta: { changes: 1 } };
        },
      };
      return statement;
    },
  };
  return { db: db as unknown as D1Database, updates };
}

function pluginInput() {
  return PublishSchema.parse({
    kind: "plugin",
    installKind: "plugin",
    name: "devkit",
    source: "https://github.com/o/r",
    repoUrl: "https://github.com/o/r",
    version: "2.7.1",
  });
}

describe("PackageRepo.publish", () => {
  it("persists a kind change when an owned pending package is republished as a plugin", async () => {
    const updated: PackageRow = { ...existing, kind: "plugin", install_kind: "plugin", latest_version: "2.7.1" };
    const { db, updates } = fakePackageDB([existing, updated]);
    const result = await new PackageRepo(db).publish(user, pluginInput(), now);

    expect(result.created).toBe(false);
    expect(result.row.kind).toBe("plugin");
    expect(updates).toHaveLength(1);
    expect(updates[0].sql).toContain("SET kind = ?1");
    expect(updates[0].values[0]).toBe("plugin");
    expect(updates[0].values[4]).toBe("plugin");
    expect(updates[0].values[10]).toBe("pending");
    expect(updates[0].values[11]).toBe(0);
    expect(updates[0].values[12]).toBe(existing.id);
  });

  it("returns an active verified package to review when its kind changes", async () => {
    const active: PackageRow = { ...existing, status: "active", verified: 1 };
    const requeued: PackageRow = {
      ...active,
      kind: "plugin",
      install_kind: "plugin",
      latest_version: "2.7.1",
      status: "pending",
      verified: 0,
    };
    const { db, updates } = fakePackageDB([active, requeued]);

    const result = await new PackageRepo(db).publish(user, pluginInput(), now);

    expect(result.row.status).toBe("pending");
    expect(result.row.verified).toBe(0);
    expect(updates[0].values[10]).toBe("pending");
    expect(updates[0].values[11]).toBe(0);
  });

  it("returns a same-kind active package update to review", async () => {
    const active: PackageRow = { ...existing, status: "active", verified: 1 };
    const updated: PackageRow = {
      ...active,
      summary: "new summary",
      source: "https://github.com/o/r2",
      repo_url: "https://github.com/o/r2",
      install_kind: "mcp",
      latest_version: "2.7.1",
      status: "pending",
      verified: 0,
    };
    const { db, updates } = fakePackageDB([active, updated]);
    const input = PublishSchema.parse({
      kind: "mcp",
      name: "devkit",
      summary: "new summary",
      source: "https://github.com/o/r2",
      repoUrl: "https://github.com/o/r2",
      version: "2.7.1",
    });

    const result = await new PackageRepo(db).publish(user, input, now);

    expect(result.row.status).toBe("pending");
    expect(result.row.verified).toBe(0);
    expect(updates[0].values[4]).toBe("mcp");
    expect(updates[0].values[10]).toBe("pending");
    expect(updates[0].values[11]).toBe(0);
  });

  it("returns a hidden package update to review and clears verification", async () => {
    const hidden: PackageRow = { ...existing, status: "hidden", verified: 1 };
    const updated: PackageRow = {
      ...hidden,
      kind: "plugin",
      install_kind: "plugin",
      latest_version: "2.7.1",
      status: "pending",
      verified: 0,
    };
    const { db, updates } = fakePackageDB([hidden, updated]);

    const result = await new PackageRepo(db).publish(user, pluginInput(), now);

    expect(result.row.status).toBe("pending");
    expect(result.row.verified).toBe(0);
    expect(updates[0].values[10]).toBe("pending");
    expect(updates[0].values[11]).toBe(0);
  });

  it("returns a rejected package update to review", async () => {
    const rejected: PackageRow = { ...existing, status: "rejected", verified: 0 };
    const requeued: PackageRow = { ...rejected, latest_version: "2.7.1", status: "pending" };
    const { db, updates } = fakePackageDB([rejected, requeued]);
    const input = PublishSchema.parse({
      kind: "mcp",
      name: "devkit",
      source: "https://github.com/o/r",
      repoUrl: "https://github.com/o/r",
      version: "2.7.1",
    });

    const result = await new PackageRepo(db).publish(user, input, now);

    expect(result.row.status).toBe("pending");
    expect(updates[0].values[10]).toBe("pending");
    expect(updates[0].values[11]).toBe(0);
  });

  it("preserves status and verification for trusted admin updates", async () => {
    const admin: RegistryUser = { ...user, role: "admin" };
    const active: PackageRow = { ...existing, status: "active", verified: 1 };
    const updated: PackageRow = { ...active, install_kind: "mcp", latest_version: "2.7.1" };
    const { db, updates } = fakePackageDB([active, updated]);
    const input = PublishSchema.parse({
      kind: "mcp",
      name: "devkit",
      source: "https://github.com/o/r",
      repoUrl: "https://github.com/o/r",
      version: "2.7.1",
    });

    const result = await new PackageRepo(db).publish(admin, input, now);

    expect(result.row.status).toBe("active");
    expect(result.row.verified).toBe(1);
    expect(updates[0].values[10]).toBe("active");
    expect(updates[0].values[11]).toBe(1);
  });
});

describe("PackageRepo.setStatusIfCurrent", () => {
  it("approves only the exact package revision the admin reviewed", async () => {
    const approvedAt = "2026-07-22T01:00:00.000Z";
    const approved: PackageRow = { ...existing, status: "active", updated_at: approvedAt };
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
            return approved as T;
          },
        };
        return statement;
      },
    } as unknown as D1Database;

    const row = await new PackageRepo(db).setStatusIfCurrent(
      existing.slug,
      "active",
      existing.latest_version,
      existing.updated_at,
      existing.status,
      approvedAt,
    );

    expect(row).toEqual(approved);
    expect(statements[0].sql).toContain("latest_version = ?4 AND updated_at = ?5 AND status = ?6");
    expect(statements[0].sql).toContain("RETURNING *");
    expect(statements[0].values).toEqual([
      "active",
      approvedAt,
      existing.slug,
      existing.latest_version,
      existing.updated_at,
      existing.status,
    ]);
  });

  it("returns null when a newer package revision no longer matches", async () => {
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
            return null as T | null;
          },
        };
        return statement;
      },
    } as unknown as D1Database;

    const row = await new PackageRepo(db).setStatusIfCurrent(
      existing.slug,
      "active",
      existing.latest_version,
      existing.updated_at,
      existing.status,
      "2026-07-22T01:00:00.000Z",
    );

    expect(row).toBeNull();
    expect(statements).toHaveLength(1);
  });
});

describe("PackageRepo.versions", () => {
  it("returns a bounded page and a stable cursor for older versions", async () => {
    let sql = "";
    const rows = [
      { id: 3, version: "0.3.0", source: "s3", content_hash: "h3", risk_level: "", created_at: "2026-07-24T00:00:00.000Z" },
      { id: 2, version: "0.2.0", source: "s2", content_hash: "h2", risk_level: "", created_at: "2026-07-23T00:00:00.000Z" },
      { id: 1, version: "0.1.0", source: "s1", content_hash: "h1", risk_level: "", created_at: "2026-07-22T00:00:00.000Z" },
    ];
    const db = {
      prepare(query: string) {
        sql = query;
        const statement = {
          bind() { return statement; },
          async all<T>() { return { results: rows as T[] }; },
        };
        return statement;
      },
    } as unknown as D1Database;
    const result = await new PackageRepo(db).versions(7, { limit: 2, before: "2026-07-25T00:00:00.000Z", beforeId: 4 });
    expect(result.versions).toHaveLength(2);
    expect(result.pageInfo).toEqual({ limit: 2, hasMore: true, nextBefore: rows[1].created_at, nextBeforeId: rows[1].id });
    expect(sql).toContain("created_at < ?2");
    expect(sql).toContain("ORDER BY created_at DESC, id DESC LIMIT ?4");
  });
});

describe("PackageRepo.list", () => {
  it("uses the daily install rollup for trending", async () => {
    let sql = "";
    const db = {
      prepare(query: string) {
        sql = query;
        const statement = {
          bind() { return statement; },
          async all() { return { results: [] }; },
        };
        return statement;
      },
    } as unknown as D1Database;
    await new PackageRepo(db).list({ kind: "all", q: "", sort: "trending", limit: 24, offset: 0, now });
    expect(sql).toContain("FROM package_install_daily");
    expect(sql).not.toContain("FROM events");
    expect(sql).toContain("SUM(count)");
  });
});

describe("PackageRepo.recordInstall", () => {
  it("increments the package and a daily rollup without writing a raw install event", async () => {
    const sqlite = new DatabaseSync(":memory:");
    sqlite.exec(registrySchema);
    sqlite.prepare(
      `INSERT INTO packages (kind, scope_handle, name, slug, source, latest_version, status, publisher_id, created_at, updated_at)
       VALUES ('skill', 'publisher', 'devkit', 'publisher/devkit', 'https://github.com/o/r', '0.1.0', 'active', 7, ?1, ?1)`,
    ).run(now);
    const db = {
      prepare(sql: string) {
        const statement = sqlite.prepare(sql);
        const wrapper: any = {
          bind(...values: unknown[]) { wrapper.values = values; return wrapper; },
          values: [] as unknown[],
          async first() { return statement.get(...wrapper.values); },
          async all() { return { results: statement.all(...wrapper.values) }; },
          async run() { return { meta: { changes: Number(statement.run(...wrapper.values).changes) } }; },
        };
        return wrapper;
      },
      async batch(statements: Array<{ values?: unknown[]; first?: () => Promise<unknown>; run?: () => Promise<unknown> }>) {
        const results = [] as Array<{ results: unknown[] }>;
        for (const statement of statements) {
          if (statement.first) results.push({ results: [await statement.first()] });
          else {
            await statement.run?.();
            results.push({ results: [] });
          }
        }
        return results;
      },
    } as unknown as D1Database;
    try {
      const result = await new PackageRepo(db).recordInstall("publisher/devkit", now);
      expect(result).toMatchObject({ count: 1, scopeHandle: "publisher" });
      expect(sqlite.prepare("SELECT count FROM package_install_daily").get()).toEqual({ count: 1 });
      expect(sqlite.prepare("SELECT COUNT(*) AS count FROM events WHERE type = 'install'").get()).toEqual({ count: 0 });
    } finally {
      sqlite.close();
    }
  });
});
