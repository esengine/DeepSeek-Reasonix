import { describe, expect, it } from "vitest";
import type { PackageRow, RegistryUser } from "../types";
import { PublishSchema } from "../lib/validation";
import { PackageRepo } from "./packages";

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
